package main

import (
	"flag"
	"fmt"
	"fyne.io/fyne"
	"fyne.io/fyne/app"
	"fyne.io/fyne/canvas"
	"fyne.io/fyne/dialog"
	"fyne.io/fyne/layout"
	"fyne.io/fyne/theme"
	"fyne.io/fyne/widget"
	"github.com/fioprotocol/fio-go"
	monitor "github.com/frameloss/fiowatch"
	"github.com/frameloss/fiowatch/assets"
	"github.com/frameloss/prettyfyne"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"image/color"
	"log"
	"math"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	MonitorLightTheme bool
	P2pNode, Uri      string
)

func main() {
	monitorTitle := "FIO Block Activity Summary"

	ErrChan := make(chan string)
	go func() {
		for e := range ErrChan {
			log.Println(e)
		}
	}()

	var (
		api        *fio.API
		opts       *fio.TxOptions
		err        error
		impolite   bool
		fullscreen bool
	)

	flag.BoolVar(&impolite, "impolite", false, "use get_block instead of P2P, results in lots of API traffic")
	flag.BoolVar(&fullscreen, "full", false, "start in full-screen mode")
	flag.StringVar(&P2pNode, "p2p", "127.0.0.1:3856", "nodeos P2P endpoint")
	flag.StringVar(&Uri, "u", "", "nodeos API endpoint")
	flag.Parse()
	if impolite {
		P2pNode = ""
	}

	rect := monitor.Scale()
	me := app.NewWithID("org.frameloss.fiowatch")
	monitorWindow := me.NewWindow(monitorTitle)

	go func() {
		for {
			if Uri == "" {
				time.Sleep(time.Second)
				continue
			}
			api, opts, err = fio.NewConnection(nil, Uri)
			if err != nil {
				panic(err)
			}
			api.Header.Set("User-Agent", "fio-watch")
			switch opts.ChainID.String() {
			case fio.ChainIdTestnet:
				monitorTitle = monitorTitle + " (Testnet)"
			case fio.ChainIdMainnet:
				monitorTitle = monitorTitle + " (Mainnet)"
			}
			monitorWindow.SetTitle(monitorTitle)
			break
		}
	}()

	// channels used for getting data from the chain:
	stopFetching := make(chan bool, 1)
	summaryChan := make(chan *monitor.BlockSummary)
	detailsChan := make(chan *monitor.ActionRow)
	headChan := make(chan int)
	libChan := make(chan int)
	fetchDiedChan := make(chan bool, 1)

	// my state
	allDone := sync.WaitGroup{}
	stopDetails := make(chan bool, 1)

	pie := &canvas.Image{}

	// allow workers to detect if the window was closed, sometimes the exit signal isn't making it before the process dies.
	heartBeat := make(chan time.Time)
	var closed bool
	go func() {
		for {
			if closed {
				return
			}
			time.Sleep(time.Second)
			heartBeat <- time.Now()
		}
	}()
	runningSlow := time.Now().Add(-time.Second)
	runningSlowChan := make(chan bool)
	go func() {
		for {
			select {
			case <-stopDetails:
				return
			case s := <-runningSlowChan:
				if s {
					runningSlow = time.Now().Add(15 * time.Second)
				}
			}
		}
	}()

	wbCounter := 1
	go func() {
		for {
			if api != nil {
				break
			}
			time.Sleep(time.Second)
		}
		monitor.WatchBlocks(summaryChan, detailsChan, stopFetching, headChan, libChan, fetchDiedChan, heartBeat, runningSlowChan, api.BaseURL, P2pNode)
	}()

	wRunning := make(chan bool, 1)
	var notified bool
	notifyQuit := func() {
		closed = true
		if notified {
			return
		}
		notified = true
		close(stopDetails)
		allDone.Wait()
		if wbCounter > 0 {
			close(stopFetching)
		}
		monitorWindow.Close()
	}
	monitorWindow.SetOnClosed(func() {
		dialog.NewInformation("Exiting", "Stopping collection threads, standby.", monitorWindow)
		go notifyQuit()
	})

	monSize := func() fyne.Size {
		for _, w := range fyne.CurrentApp().Driver().AllWindows() {
			if w.Title() == monitorTitle {
				return w.Canvas().Size()
			}
		}
		return fyne.CurrentApp().Driver().AllWindows()[0].Canvas().Size()
	}

	var txsSeen, txInBlockMin, txInBlockMax, currentHead, currentLib, seenBlocks int
	p := message.NewPrinter(language.AmericanEnglish)
	info := widget.NewLabelWithStyle("Patience: getting ABI information", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	blockInfo := widget.NewLabel("")
	infoBox := widget.NewHBox(layout.NewSpacer(), info, blockInfo, layout.NewSpacer())
	myWidth := monSize().Width
	var pieReady bool
	updateChartChan := make(chan bool)

	var startBlock int
	go func() {
		for {
			if api == nil {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			startBlock = int(api.GetCurrentBlock())
			break
		}
	}()
	ticks := 360

	chartVals := make([]*monitor.BlockSummary, 0)

	detailsGrid := fyne.NewContainerWithLayout(layout.NewGridLayout(5))
	detailsBusy := false
	detailsGridContent := monitor.NewDetailsContent()
	allDone.Add(1)
	go func() {
		defer allDone.Done()
		<-wRunning
		last := time.Now()
		tick := time.NewTicker(500 * time.Millisecond)
		for {
			select {
			case <-stopDetails:
				return
			case <-tick.C:
				if closed {
					return
				}
				if pieReady {
					if detailsBusy {
						continue
					}
					if detailsGridContent.Updated.After(last) {
						detailsBusy = true
						last = detailsGridContent.Updated
						detailsGrid.Objects = detailsGridContent.Get()
						detailsGrid.Refresh()
						detailsBusy = false
					}
				}
			}
		}
	}()

	lastHeadTime := time.Now()
	// since we use headblock updates for stall detection, run them in their own thread so processing doesn't block
	allDone.Add(1)
	defer allDone.Done()
	go func() {
		for {
			select {
			case currentHead = <-headChan:
				if closed {
					return
				}
				lastHeadTime = time.Now()
			case currentLib = <-libChan:
			}
		}
	}()

	go detailsGridContent.UpdateContent(detailsChan, stopDetails, monitorWindow)

	allDone.Add(1)
	go func() {
		defer allDone.Done()
		<-wRunning
		if Uri == "" {
			go func() {
				api, P2pNode = promptForUrl()
				Uri = api.BaseURL
			}()
		}
		for {
			if api != nil {
				break
			}
			time.Sleep(time.Second)
		}
		stalledTicker := time.NewTicker(15 * time.Second)
		stalled := false
		for {
			select {
			case newSummary := <-summaryChan:
				if closed {
					return
				}
				go func() {
					innerWg := sync.WaitGroup{}
					waitCh := make(chan struct{})
					innerWg.Add(1)
					go func() {
						go func() {
							defer innerWg.Done()
							// only happens first time we get a summary
							if len(chartVals) == 0 {
								chartVals = make([]*monitor.BlockSummary, ticks*2)
								st := time.Now().Add(time.Duration(-ticks-5) * time.Second)
								for i := 0; i < ticks*2; i++ {
									if chartVals[i] == nil {
										chartVals[i] = &monitor.BlockSummary{}
									}
									chartVals[i].T = st.Add(time.Duration(i*500) * time.Millisecond)
									chartVals[i].Block = newSummary.Block - ticks*2 + i
									chartVals[i].Y = 0.0
								}
							}
							chartVals[0] = nil
							for i := 1; i < len(chartVals); i++ {
								chartVals[i-1] = chartVals[i]
							}
							chartVals[len(chartVals)-1] = newSummary
							if !pieReady && len(newSummary.Actions) != 0 {
								pieReady = true
							}
						}()
						innerWg.Wait()
						close(waitCh)
					}()
					select {
					case <-waitCh:
						return
					case <-time.After(10 * time.Second):
						return
					}
				}()

			case <-fetchDiedChan:
				wbCounter = wbCounter - 1
				if closed {
					return
				}
				if !stalled {
					wbCounter = wbCounter + 1
					go monitor.WatchBlocks(summaryChan, detailsChan, stopFetching, headChan, libChan, fetchDiedChan, heartBeat, runningSlowChan, api.BaseURL, P2pNode)
				}

			case <-stalledTicker.C:
				innerWg := sync.WaitGroup{}
				waitCh := make(chan struct{})
				innerWg.Add(1)
				if stalled {
					continue
				}
				go func() {
					if closed {
						return
					}
					go func() {
						defer innerWg.Done()
						if lastHeadTime.Before(time.Now().Add(-45 * time.Second)) {
							if closed {
								return
							}
							stalled = true
							ErrChan <- "monitor: no head block update in 45s, forcibly restarting workers"
							if !closed {
								wbCounter = wbCounter - 1
								go func() { stopFetching <- true }()
							}
							time.Sleep(2 * time.Second)
							wbCounter = wbCounter + 1
							go monitor.WatchBlocks(summaryChan, detailsChan, stopFetching, headChan, libChan, fetchDiedChan, heartBeat, runningSlowChan, api.BaseURL, P2pNode)
						}
					}()
					innerWg.Wait()
					close(waitCh)
				}()
				select {
				case <-waitCh:
					if closed {
						return
					}
					stalled = false
					continue
				case <-time.After(time.Minute):
					if closed {
						return
					}
					ErrChan <- "monitor: data collection stalled and couldn't be restarted"
					notifyQuit()
					monitorWindow.Close()
					return
				}
			}
		}
	}()

	pieSize := func() int {
		return 256
		//switch true {
		//case me.Driver().AllWindows()[0].Canvas().Size().Width <= 400:
		//	return 128
		//case me.Driver().AllWindows()[0].Canvas().Size().Width <= 600:
		//	return 192
		//default:
		//	return 256
		//}
	}

	sortedCopy := make([]*monitor.BlockSummary, 0)
	getSummary := func() []*monitor.BlockSummary {
		if len(sortedCopy) <= ticks {
			return sortedCopy
		}
		return sortedCopy[len(sortedCopy)-ticks:]
	}

	allDone.Add(1)
	go func() {
		defer allDone.Done()

		<-wRunning
		updateChartChan <- true
		time.Sleep(2 * time.Second)
		sortTick := time.NewTicker(time.Second)
		var skip bool
		for {
			select {
			case <-sortTick.C:
				if closed {
					return
				}
				if skip {
					continue
				}
				skip = true
				uniq := make(map[int]*monitor.BlockSummary)
				for _, bs := range chartVals {
					uniq[bs.Block] = bs
				}
				sorted := make([]int, 0)
				for blockNum := range uniq {
					sorted = append(sorted, blockNum)
				}
				sort.Ints(sorted)
				sortedCopy = make([]*monitor.BlockSummary, len(sorted))
				if len(sorted) == 0 {
					return
				}
				for i, n := range sorted {
					sortedCopy[i] = uniq[n]
				}
				if monitor.BlockSummarysLen(sortedCopy) < 2 {
					pie.Image, _, _ = fioassets.NewFioLogo()
					pie.Refresh()
				}
				skip = false
			}
		}
	}()

	allDone.Add(1)
	go func() {
		defer allDone.Done()
		<-wRunning
		var pieHasLogo bool
		pieContainer := fyne.NewContainerWithLayout(layout.NewFixedGridLayout(fyne.NewSize(pieSize(), pieSize())))
		timeLine := canvas.NewImageFromResource(fyne.NewStaticResource("timeline", monitor.LineChart(getSummary(), MonitorLightTheme, myWidth/5*4+((myWidth/5*4)/2), pieSize()+(pieSize()/2))))
		timeContainer := fyne.NewContainerWithLayout(layout.NewFixedGridLayout(fyne.NewSize(myWidth/5*4, pieSize())), timeLine)
		chartRows := widget.NewHBox(
			pieContainer,
			layout.NewSpacer(),
			timeContainer,
		)
		chartRows.Refresh()
		cheight := func() int {
			return monSize().Height - chartRows.Size().Height - infoBox.Size().Height - 50
			//return rect.Dy() - pieSize() - 50
		}

		extraInfoBox := widget.NewHBox()
		content := fyne.NewContainerWithLayout(layout.NewMaxLayout(), widget.NewVBox(
			infoBox,
			chartRows,
			extraInfoBox,
			fyne.NewContainerWithLayout(layout.NewFixedGridLayout(fyne.NewSize(myWidth-20, cheight())),
				widget.NewGroupWithScroller("TX Details",
					detailsGrid,
				)),
		))
		pie.Image, _, _ = fioassets.NewFioLogo()
		monitorWindow.SetContent(content)
		// give the API a few seconds to pull ABIs before we start.
		for {
			time.Sleep(200 * time.Millisecond)
			if monitor.Abis != nil && monitor.Abis.Ready {
				break
			}
		}

		pieContainer.AddObject(pie)
		extraInfo := []fyne.CanvasObject{widget.NewLabel(" ")}
		var eiRefresh bool

		// receive update notices:
		var alreadyBusy bool
		for {
			if closed {
				return
			}
			select {
			case <-updateChartChan:
				if closed {
					return
				}
				if alreadyBusy {
					continue
				}
				innerWg := sync.WaitGroup{}
				waitCh := make(chan struct{})
				innerWg.Add(1)
				go func() {
					go func() {
						defer innerWg.Done()
						alreadyBusy = true
						if time.Now().Before(runningSlow) {
							extraInfo = []fyne.CanvasObject{
								layout.NewSpacer(),
								fyne.NewContainerWithLayout(layout.NewFixedGridLayout(fyne.NewSize(28, 28)),
									canvas.NewImageFromResource(theme.WarningIcon()),
								),
								widget.NewLabelWithStyle("  Cannot keep up with transaction volume, displayed data will not be accurate.", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
								layout.NewSpacer(),
							}
							extraInfoBox.Children = extraInfo
							extraInfoBox.Refresh()
							eiRefresh = true
						} else if eiRefresh {
							eiRefresh = false
							extraInfo = []fyne.CanvasObject{widget.NewLabel(" ")}
							extraInfoBox.Children = extraInfo
							extraInfoBox.Refresh()
						}
						timeLine.Resource = fyne.NewStaticResource("timeline", monitor.LineChart(getSummary(), MonitorLightTheme, myWidth/5*4+((myWidth/5*4)/2), pieSize()+(pieSize()/2)))
						timeLine.Refresh()
						if monitor.BlockSummarysLen(chartVals) < 2 {
							if !pieHasLogo {
								pie.Image, _, _ = fioassets.NewFioLogo()
								pie.Refresh()
								pieHasLogo = true
							}
						} else {
							pieHasLogo = false
							if pie.Hidden {
								pie.Show()
							}
							pieContainer.Resize(fyne.NewSize(pieSize(), pieSize()))
							//pie.Image = monitor.Pie(getSummary(), pieSize()+(pieSize()/2), pieSize()+(pieSize()/2), MonitorLightTheme)
							pie.Image = monitor.Pie(getSummary(), pieSize()*3, pieSize()*3)
							//pie.Image = monitor.Pie(getSummary(), pieSize(), pieSize(), MonitorLightTheme)
							pieContainer.Refresh()
						}

						info.SetText(p.Sprintf("%d Transactions In Last %d Blocks.", txsSeen, seenBlocks))
						blockInfo.SetText(p.Sprintf(" (Most in one block: %d, Least: %d) Current Head: %d, Last Irreversible: %d", txInBlockMin, txInBlockMax, currentHead, currentLib))

						// detect size change, and handle new layout
						if myWidth != monSize().Width {
							myWidth = monSize().Width

							// update grids for charts:
							pieContainer.Objects = []fyne.CanvasObject{fyne.NewContainerWithLayout(layout.NewFixedGridLayout(fyne.NewSize(pieSize(), pieSize())), pie)}
							timeContainer.Objects = []fyne.CanvasObject{fyne.NewContainerWithLayout(layout.NewFixedGridLayout(fyne.NewSize(myWidth/5*4, pieSize())), timeLine)}
							chartRows.Children = []fyne.CanvasObject{widget.NewHBox(
								pieContainer,
								layout.NewSpacer(),
								timeContainer,
							)}
							chartRows.Refresh()

							detailsBusy = true
							detailsGrid = fyne.NewContainerWithLayout(layout.NewGridLayout(5))
							detailsGrid.Objects = detailsGridContent.Get()
							detailsGrid.Refresh()
							detailsBusy = false

							content.Objects = []fyne.CanvasObject{fyne.NewContainerWithLayout(layout.NewMaxLayout(), widget.NewVBox(
								infoBox,
								chartRows,
								extraInfoBox,
								fyne.NewContainerWithLayout(layout.NewFixedGridLayout(fyne.NewSize(myWidth-20, cheight())),
									widget.NewGroupWithScroller("TX Details",
										detailsGrid,
									)),
							))}
							content.Refresh()
						}

					}()
					innerWg.Wait()
					close(waitCh)
					alreadyBusy = false
				}()

				select {
				case <-waitCh:
					if closed {
						return
					}
					continue
				case <-time.After(30 * time.Second):
					if closed {
						return
					}
					ErrChan <- "deadlock detected while trying to update display"
					notifyQuit()
					monitorWindow.Close()
					return
				}
			}
		}
	}()

	txCount := func() (total int, most int, least int) {
		var c, m, l int
		if len(getSummary()) < 2 {
			return 0, 0, 0
		}
		m, l = int(getSummary()[0].Y), int(getSummary()[0].Y)
		for _, b := range getSummary() {
			cur := int(math.Round(b.Y))
			c = c + cur
			if m < cur {
				m = cur
			}
			if l > cur {
				l = cur
			}
		}
		return c, m, l
	}

	go func() {
		<-wRunning
		updateChartChan <- true
		//defer log.Println("routine with tickers triggering refreshes exited.")
		blockTick := time.NewTicker(500 * time.Millisecond)
		t := time.NewTicker(500 * time.Millisecond)
		for {
			select {
			case <-t.C:
				if closed {
					return
				}
				txsSeen, txInBlockMin, txInBlockMax = txCount()
				updateChartChan <- true
			case <-blockTick.C:
				if closed {
					return
				}
				startBlock = startBlock + 1
				if seenBlocks < ticks {
					seenBlocks = seenBlocks + 1
				}
			}
		}
	}()

	go func() {
		time.Sleep(250 * time.Millisecond)
		close(wRunning)
	}()

	go func() {
		for {
			time.Sleep(50 * time.Millisecond)
			if fyne.CurrentApp().Driver().AllWindows()[0].Canvas().Content().Visible() {
				break
			}
		}
		th := prettyfyne.ExampleDracula
		th.TextSize = 13
		th.PlaceHolderColor = color.RGBA{
			R: 128,
			G: 128,
			B: 128,
			A: 255,
		}
		th.IconColor = th.PlaceHolderColor
		th.FocusColor = color.RGBA{
			R: 72,
			G: 72,
			B: 96,
			A: 255,
		}
		th.HoverColor = color.RGBA{
			R: 24,
			G: 24,
			B: 36,
			A: 255,
		}
		fyne.CurrentApp().Settings().SetTheme(th.ToFyneTheme())
		// without something in the window we can't calculate the canvas size,
		monitorWindow.SetContent(widget.NewHBox(layout.NewSpacer()))

		if fullscreen || rect.Dy() == 0 {
			monitorWindow.SetFullScreen(true)
			return
		}

		scale := monitorWindow.Canvas().Scale()
		x := int(float32(rect.Dx()) * scale)
		y := int(float32(rect.Dy()) * scale)

		monitorWindow.Resize(fyne.NewSize(x, y-(y/4)))
	}()

	//monitorWindow.CenterOnScreen()
	monitorWindow.SetMaster()
	monitorWindow.ShowAndRun()
}

func promptForUrl() (api *fio.API, p2pAddr string) {
	errLabel := widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	a, p := monitor.GetHost()
	submit := &widget.Button{}
	p2pInput := widget.NewEntry()
	p2pInput.SetPlaceHolder("nodeos:9876")
	p2pInput.OnChanged = func(string) {
		errLabel.SetText("")
	}

	uriInput := widget.NewEntry()
	uriInput.SetPlaceHolder("http://nodeos:8888")

	uriInput.SetText(a)
	p2pInput.SetText(p)
	uriInput.OnChanged = func(s string) {
		errLabel.SetText("")
		if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
			if u := strings.Split(s, "//"); len(u) > 1 {
				h := strings.Split(u[1], ":")
				p2pInput.SetText(h[0] + ":9876")
			}
		}
	}

	good := make(chan interface{}, 1)
	submit = widget.NewButtonWithIcon("Connect", theme.ConfirmIcon(), func() {
		errLabel.SetText("Please wait ...")
		submit.Disable()
		defer submit.Enable()
		_, err := url.Parse(uriInput.Text)
		if err != nil {
			errLabel.SetText(fmt.Sprintf("Invalid url: %s", err.Error()))
			return
		}
		api, _, err = fio.NewConnection(nil, uriInput.Text)
		if err != nil {
			errLabel.SetText(fmt.Sprintf("Couldn't connect to nodeos API: %s", err.Error()))
			return
		}
		p := strings.Split(p2pInput.Text, ":")
		if len(p) != 2 {
			errLabel.SetText("p2p address should be in the format host:port")
			return
		}
		ips, err := net.LookupHost(p[0])
		if err != nil {
			errLabel.SetText(fmt.Sprintf("Couldn't resolve p2p host: %s", err.Error()))
			return
		}
		if len(ips) == 0 {
			errLabel.SetText(fmt.Sprintf("could not resolve %q to an IPv4 addres", p[0]))
			return
		}
		var port int64
		port, err = strconv.ParseInt(p[1], 10, 32)
		if err != nil {
			errLabel.SetText(fmt.Sprintf("p2p port invalid: %s", err.Error()))
			return
		}
		dest := net.ParseIP(ips[0])
		t := &net.TCPConn{}
		t, err = net.DialTCP("tcp4", nil, &net.TCPAddr{IP: dest, Port: int(port)})
		if err != nil {
			errLabel.SetText(fmt.Sprintf("could not connect to p2p service: %s", err.Error()))
			return
		}
		t.Close()
		errLabel.SetText("connected!")
		close(good)
	})

	randMain := widget.NewButton("Mainnet", func() {
		errLabel.SetText("")
		a, p, err := monitor.GetRandomHost(monitor.Mainnet)
		if err != nil {
			errLabel.SetText(err.Error())
			return
		}
		uriInput.SetText(a)
		p2pInput.SetText(p)
	})
	randTest := widget.NewButton("Testnet", func() {
		errLabel.SetText("")
		a, p, err := monitor.GetRandomHost(monitor.Testnet)
		if err != nil {
			errLabel.SetText(err.Error())
			return
		}
		uriInput.SetText(a)
		p2pInput.SetText(p)
	})

	i := &canvas.Image{}
	i.Image, _, _ = fioassets.NewFioLogo()
	box := widget.NewHBox(
		fyne.NewContainerWithLayout(layout.NewFixedGridLayout(fyne.NewSize(256, 256)), i),
		widget.NewVBox(
			layout.NewSpacer(),
			widget.NewForm(
				widget.NewFormItem("API", uriInput),
				widget.NewFormItem("P2P Address", p2pInput),
			),
			widget.NewHBox(layout.NewSpacer(), submit, layout.NewSpacer()),
			errLabel,
			layout.NewSpacer(),
			widget.NewHBox(
				widget.NewLabel("Find random hosts for network"), randMain, randTest, layout.NewSpacer(), widget.NewLabel("   "),
			),
			layout.NewSpacer(),
		))
	pop := widget.NewModalPopUp(box, fyne.CurrentApp().Driver().AllWindows()[0].Canvas())
	pop.Show()
	<-good
	pop.Hide()
	monitor.SaveHost(uriInput.Text, p2pInput.Text)
	return api, p2pInput.Text
}

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
	"log"
	"math"
	"sort"
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

	var impolite bool

	flag.BoolVar(&impolite, "impolite", false, "use get_block instead of P2P, results in lots of API traffic")
	flag.StringVar(&P2pNode, "p2p", "127.0.0.1:3856", "nodeos P2P endpoint")
	flag.StringVar(&Uri, "u", "http://127.0.0.1:8888", "nodeos API endpoint")
	flag.Parse()
	if impolite {
		P2pNode = ""
	}
	api, opts, err := fio.NewConnection(nil, Uri)
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

	me := app.NewWithID("org.frameloss.fiowatch")
	monitorWindow := me.NewWindow(monitorTitle)

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
	go monitor.WatchBlocks(summaryChan, detailsChan, stopFetching, headChan, libChan, fetchDiedChan, heartBeat, runningSlowChan, api.BaseURL, P2pNode)

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
	info := widget.NewLabelWithStyle("Showing last 0 Blocks", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	blockInfo := widget.NewLabel("")
	infoBox := widget.NewHBox(layout.NewSpacer(), info, blockInfo, layout.NewSpacer())
	myWidth := monSize().Width
	var pieReady bool
	updateChartChan := make(chan bool)

	startBlock := int(api.GetCurrentBlock())
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
			return monSize().Height - chartRows.Size().Height - infoBox.Size().Height - 110
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
		fmt.Println("size:", th.TextSize, fyne.CurrentApp().Driver().AllWindows()[0].Canvas().Size().Width)
		fyne.CurrentApp().Settings().SetTheme(th.ToFyneTheme())
	}()

	monitorWindow.Resize(fyne.NewSize(1440, 900))
	monitorWindow.CenterOnScreen()

	monitorWindow.SetMaster()
	monitorWindow.ShowAndRun()
}

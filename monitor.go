package fiowatch

import (
	"fmt"
	"fyne.io/fyne"
	"fyne.io/fyne/dialog"
	"fyne.io/fyne/layout"
	"fyne.io/fyne/widget"
	"github.com/wcharczuk/go-chart"
	"github.com/wcharczuk/go-chart/drawing"
	"sort"
	"strings"
	"sync"
	"time"
)

const ActionRows = 32

type BlockSummary struct {
	Mux     sync.RWMutex
	Actions map[string]int
	Y       float64
	T       time.Time
	Block   int
}

type BlockSummarys []*BlockSummary

func (b BlockSummarys) ToXY() (xv []time.Time, yv []float64) {
	xv = make([]time.Time, len(b))
	yv = make([]float64, len(b))
	for i, t := range b {
		xv[i] = t.T
		yv[i] = t.Y
	}
	return xv, yv
}

func (b BlockSummarys) ToValues() (v []chart.Value) {
	summed := make(map[string]int)
	for _, s := range b {
		if s.Actions == nil {
			continue
		}
		s.Mux.RLock()
		for n := range s.Actions {
			summed[n] = summed[n] + s.Actions[n]
		}
		s.Mux.RUnlock()
	}
	sorted := make([]string, len(summed))
	z := 0
	for n := range summed {
		sorted[z] = n
		z += 1
	}
	sort.Strings(sorted)
	style := chart.StyleTextDefaults()
	style.Padding = chart.Box{4, 4, 4, 4, true}
	style.FontSize = 10
	style.FontColor = drawing.Color{
		R: 255,
		G: 255,
		B: 255,
		A: 255,
	}
	v = make([]chart.Value, len(sorted))
	for i := range sorted {
		v[i] = chart.Value{
			Label: sorted[i],
			Value: float64(summed[sorted[i]]),
			Style: style,
		}
	}
	return
}

func BlockSummarysLen(b BlockSummarys) int {
	return len(b.ToValues())
}

type DetailsContent struct {
	L [ActionRows * 5]fyne.CanvasObject
	sync.RWMutex
	Updated time.Time
}

func (labels *DetailsContent) Push(f [5]fyne.CanvasObject) {
	labels.Lock()
	labels.Updated = time.Now()

	labels.L[len(labels.L)-1] = nil
	labels.L[len(labels.L)-2] = nil
	labels.L[len(labels.L)-3] = nil
	labels.L[len(labels.L)-4] = nil
	labels.L[len(labels.L)-5] = nil

	for i := len(labels.L) - 1; i >= 5; i-- {
		labels.L[i] = labels.L[i-5]
	}
	labels.L[0] = f[0]
	labels.L[1] = f[1]
	labels.L[2] = f[2]
	labels.L[3] = f[3]
	labels.L[4] = f[4]
	labels.Unlock()
}

func NewDetailsContent() *DetailsContent {
	l := DetailsContent{}
	for i := range l.L {
		l.L[i] = layout.NewSpacer()
	}
	return &l
}

func (labels *DetailsContent) Get() []fyne.CanvasObject {
	labels.RLock()
	defer labels.RUnlock()
	if labels.L[0] == nil {
		return []fyne.CanvasObject{layout.NewSpacer(), layout.NewSpacer(), layout.NewSpacer(), layout.NewSpacer(), layout.NewSpacer()}
	}
	content := labels.L
	return content[:]
}

type ArBuffer struct {
	ar [ActionRows]*ActionRow
	sync.RWMutex
}

func (a *ArBuffer) Push(row *ActionRow) {
	l := a.Len()
	if l < len(a.ar) {
		a.Lock()
		a.ar[l] = row
		a.Unlock()
		return
	}
	a.Lock()
	a.ar[0] = nil
	for i := 1; i < len(a.ar); i++ {
		a.ar[i-1] = a.ar[i]
	}
	a.ar[len(a.ar)-1] = row
	a.Unlock()
}

func (a *ArBuffer) Pop(count int) []*ActionRow {
	popped := make([]*ActionRow, 0)
	l := a.Len()
	if l >= count {
		a.Lock()
		popped = append(popped, a.ar[:count]...)
		for i := range a.ar {
			a.ar[i] = nil
		}
		a.Unlock()
	} else {
		a.Lock()
		popped = append(popped, a.ar[:l]...)
		for i := range a.ar {
			a.ar[i] = nil
		}
		a.Unlock()
	}
	return popped
}

func (a *ArBuffer) Len() int {
	var has int
	a.RLock()
	for _, ar := range a.ar {
		if ar != nil {
			has += 1
		}
		if has > ActionRows {
			ar = nil
		}
	}
	a.RUnlock()
	if has > ActionRows {
		return ActionRows
	}
	return has
}

func (labels *DetailsContent) UpdateContent(rows chan *ActionRow, done chan bool, win fyne.Window) {
	//queue := make(chan *ActionRow, 1)
	queue := make(chan *ActionRow)
	//queuePending := make([]*ActionRow, 0)
	queuePending := ArBuffer{}
	pending := make(chan int)
	sendPending := make(chan int)

	closeQueue := make(chan bool, 1)
	go func() {
		for {
			select {
			case <-closeQueue:
				return
			case send := <-sendPending:
				queueCopy := queuePending.Pop(send)
				for _, c := range queueCopy {
					queue <- c
				}
			}
		}
	}()

	working := sync.Mutex{}
	tick := time.NewTicker(500 * time.Millisecond)
	notBefore := time.Now().Add(-4 * time.Second)
	var busy bool
	for {
		select {
		case <-done:
			closeQueue <- true
			return
		case incomingRow := <-rows:
			if incomingRow.Time.Before(notBefore) {
				continue
			}
			queuePending.Push(incomingRow)
		case <-tick.C:
			notBefore = time.Now().Add(-3 * time.Minute)
			l := queuePending.Len()
			if !busy && l > 0 {
				go func() {
					sendPending <- l
					pending <- l
				}()
			}
		case records := <-pending:
			if busy {
				continue
			}
			working.Lock()
			busy = true
			if records > 0 {
				for i := 0; i < records; i++ {
					ar := <-queue
					labels.Push(func(ar *ActionRow) [5]fyne.CanvasObject {
						newTopRow := [5]fyne.CanvasObject{}
						newTopRow[0] = widget.NewLabel(fmt.Sprintf("%d @ %s", ar.BlockNum, ar.Time.Format(time.UnixDate)))
						newTopRow[1] = fyne.NewContainerWithLayout(layout.NewGridLayout(2),
							widget.NewLabelWithStyle(ar.Contract, fyne.TextAlignTrailing, fyne.TextStyle{}),
							widget.NewLabelWithStyle(ar.Action, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
						)
						if len(ar.Info) > 16 {
							ar.Info = ar.Info[:13] + "..."
						}
						newTopRow[2] = fyne.NewContainerWithLayout(layout.NewGridLayout(2),
							widget.NewLabelWithStyle(ar.Actor, fyne.TextAlignTrailing, fyne.TextStyle{}),
							widget.NewLabelWithStyle(ar.Info, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
						)
						usedStyle := fyne.TextStyle{}
						if strings.HasPrefix(ar.Used, " ") {
							usedStyle.Bold = true
						}
						newTopRow[3] = widget.NewLabelWithStyle(ar.Used, fyne.TextAlignCenter, usedStyle)
						a := ar.Actor
						t := string(ar.TxDetail)
						e := widget.NewMultiLineEntry()
						e.SetText(t)
						curSize := fyne.CurrentApp().Driver().AllWindows()[0].Canvas().Size()
						button := widget.NewButton("View", func() {
							dialog.ShowCustom(a+" - TX Information", "Done",
								fyne.NewContainerWithLayout(layout.NewFixedGridLayout(fyne.NewSize(curSize.Width/2, (curSize.Height*80)/100)),
									widget.NewScrollContainer(
										e,
									),
								),
								win,
							)
						})

						txid := ar.TxId
						if len(txid) > 8 {
							txid = txid[:8] + "..."
						}
						if t == "" {
							txid = ""
							button.Hide()
						}
						newTopRow[4] = widget.NewHBox(
							widget.NewHBox(
								layout.NewSpacer(),
								widget.NewLabelWithStyle(txid, fyne.TextAlignTrailing, fyne.TextStyle{Monospace: true}),
								button,
							),
						)
						return newTopRow
					}(ar))
				}
			}
			busy = false
			working.Unlock()
		}
	}
}

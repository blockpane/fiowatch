package fiowatch

import (
	"bytes"
	"fmt"
	"fyne.io/fyne/theme"
	"github.com/golang/freetype/truetype"
	"github.com/wcharczuk/go-chart/v2"
	"github.com/wcharczuk/go-chart/v2/drawing"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"math"
	"time"
)

var font = func() *truetype.Font {
	font, err := truetype.Parse(theme.TextMonospaceFont().Content())
	if err != nil {
		fmt.Println(err)
	}
	return font
}()

func LineChart(summary BlockSummarys, lightTheme bool, w int, h int) []byte {
	xv, yv := summary.ToXY()
	p := message.NewPrinter(language.AmericanEnglish)

	style := chart.StyleTextDefaults()
	style.StrokeWidth = 2.5
	switch lightTheme {
	case true:
		style.StrokeColor = drawing.Color{R: 65, G: 65, B: 156, A: 64}
		style.FillColor = drawing.Color{R: 0x0c, G: 0x078, B: 0x9b, A: 255}
	case false:
		style.StrokeColor = drawing.Color{R: 65, G: 65, B: 156, A: 192}
		style.FillColor = drawing.Color{R: 17, G: 100, B: 138, A: 128}
	}

	fc := drawing.ColorWhite
	if lightTheme {
		fc = drawing.ColorBlack
	}

	graph := chart.Chart{
		XAxis: chart.XAxis{
			Name: "Block",
			Style: chart.Style{
				FontColor: fc,
			},
			ValueFormatter: func(v interface{}) string {
				u, ok := v.(float64)
				if !ok {
					return ""
				}
				t := time.Unix(0, int64(math.Round(u)))
				return t.Format("15:04:05")
			},
		},
		YAxis: chart.YAxis{
			Name: "# tx",
			Style: chart.Style{
				FontColor: fc,
			},
			ValueFormatter: func(v interface{}) string {
				if math.Round(v.(float64))-v.(float64) != 0.0 {
					return ""
				}
				return p.Sprintf("%d", int(math.Round(v.(float64))))
			},
		},
		ColorPalette: barPalette{},
		Background: chart.Style{
			Padding: chart.Box{
				Top:    10,
				Bottom: 10,
				Left:   10,
				Right:  10,
			},
		},
		Height: h,
		Width:  w,
		Font:   font,
		Series: []chart.Series{
			chart.TimeSeries{
				Style:   style,
				XValues: xv,
				YValues: yv,
			},
		},
	}

	buffer := bytes.NewBuffer([]byte{})
	if e := graph.Render(chart.PNG, buffer); e != nil {
		if e.Error() != "zero y-range delta" {
			fmt.Println("render: ", e)
		}
	}
	return buffer.Bytes()
}

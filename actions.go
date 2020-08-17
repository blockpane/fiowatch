package fiowatch

import (
	"bytes"
	"github.com/frameloss/fiowatch/assets"
	"github.com/wcharczuk/go-chart"
	"image"
)

func Pie(summary BlockSummarys, w int, h int) image.Image {
	if len(summary.ToValues()) < 2 {
		i, _, _ := fioassets.NewFioLogo()
		return i
	}

	style := chart.StyleTextDefaults()
	style.Padding = chart.Box{
		Top:    6,
		Left:   6,
		Right:  6,
		Bottom: 6,
	}
	style.FontSize = 30

	var cp interface{}
	cp = pieLightPalette{}

	pie := chart.PieChart{
		Width:        w,
		Height:       h,
		Font:         font,
		DPI:          288,
		Canvas:       style,
		ColorPalette: cp.(chart.ColorPalette),
		Values:       summary.ToValues(),
	}

	buf := bytes.NewBuffer(nil)
	err := pie.Render(chart.PNG, buf)
	if err != nil {
		return image.NewRGBA(image.Rect(0, 0, w, h))
	}
	img, _, err := image.Decode(buf)
	if err != nil {
		return image.NewRGBA(image.Rect(0, 0, w, h))
	}
	return img
}

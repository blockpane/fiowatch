package fiowatch

import (
	"github.com/wcharczuk/go-chart/v2"
	"github.com/wcharczuk/go-chart/v2/drawing"
)

type barPalette struct{}

func (bPal barPalette) BackgroundColor() drawing.Color {
	return drawing.Color{0, 0, 0, 0}
}

func (bPal barPalette) BackgroundStrokeColor() drawing.Color {
	return drawing.Color{0, 0, 0, 0}
}

func (bPal barPalette) CanvasColor() drawing.Color {
	return drawing.Color{0, 0, 0, 0}
}

func (bPal barPalette) CanvasStrokeColor() drawing.Color {
	return drawing.Color{0, 0, 0, 0}
}

func (bPal barPalette) AxisStrokeColor() drawing.Color {
	return chart.DefaultAxisColor
}

func (bPal barPalette) TextColor() drawing.Color {
	return chart.DefaultTextColor
}

func (bPal barPalette) GetSeriesColor(index int) drawing.Color {
	return chart.GetAlternateColor(index)
}

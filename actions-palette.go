package fiowatch

import (
	"github.com/wcharczuk/go-chart"
	"github.com/wcharczuk/go-chart/drawing"
)

type pieLightPalette struct{}

func (ap pieLightPalette) BackgroundColor() drawing.Color {
	return drawing.Color{R: 0, G: 0, B: 0, A: 0}
}

func (ap pieLightPalette) BackgroundStrokeColor() drawing.Color {
	return drawing.Color{R: 0, G: 0, B: 0, A: 0}
}

func (ap pieLightPalette) CanvasColor() drawing.Color {
	return drawing.Color{R: 0, G: 0, B: 0, A: 0}
}

func (ap pieLightPalette) CanvasStrokeColor() drawing.Color {
	return drawing.Color{R: 0, G: 0, B: 0, A: 128}
}

func (ap pieLightPalette) AxisStrokeColor() drawing.Color {
	return drawing.Color{R: 0, G: 0, B: 0, A: 0}
}

func (ap pieLightPalette) TextColor() drawing.Color {
	return drawing.Color{R: 255, G: 255, B: 255, A: 255}
}

func (ap pieLightPalette) GetSeriesColor(index int) drawing.Color {
	c := chart.GetAlternateColor(index + 1)
	c.R = 32
	if c.G > 192 {
		c.G = c.G - 128
	}
	if c.B > 192 {
		c.B = c.B - 128
	}
	c.A = 255
	return c
}

// +build !android, darwin,!arm

package fiowatch

import (
	"github.com/kbinani/screenshot"
	"image"
	"os"
)

func Scale() image.Rectangle {
	rect := screenshot.GetDisplayBounds(0)
	switch true {
	case rect.Dy() <= 640:
		os.Setenv("FYNE_SCALE","0.75")
	case rect.Dy() < 900:
		os.Setenv("FYNE_SCALE","0.85")
	}
	return rect
}

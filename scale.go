// +build !arm

package fiowatch

import (
	"github.com/kbinani/screenshot"
	"image"
)

func Scale() image.Rectangle {
	return screenshot.GetDisplayBounds(0)
	// disabling for now, don't think this is actually helping anything.
	//rect := screenshot.GetDisplayBounds(0)
	//switch true {
	//case rect.Dy() <= 640:
	//	os.Setenv("FYNE_SCALE","0.75")
	//case rect.Dy() < 900:
	//	os.Setenv("FYNE_SCALE","0.85")
	//}
	//return rect
}

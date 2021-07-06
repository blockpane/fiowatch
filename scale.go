// +build !arm

package fiowatch

import (
	"github.com/kbinani/screenshot"
	"image"
)

func Scale() image.Rectangle {
	r := screenshot.GetDisplayBounds(0)
	if r.Max.X > 1920 {
		r.Max.X = 1920
	}
	if r.Max.Y > 1080 {
		r.Max.Y = 1080
	}
	return r
}

// +build android, darwin,arm

package fiowatch

import "image"

// Scale is a noop on mobile platforms, always full screen
func Scale() image.Rectangle {
	return image.Rectangle{}
}

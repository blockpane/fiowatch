package fiowatch

import "image"

// Scale is a noop on mobile platforms, always full screen
func Scale() image.Rectangle {
	// needs a starting point, not sure what will happen on ios or android just yet.
	return image.Rectangle{
		Max: image.Point{
			X: 256,
			Y: 1024,
		},
	}
}

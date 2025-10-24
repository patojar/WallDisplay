//go:build !linux

package matrixdisplay

import (
	"errors"
	"image"
)

// Controller is unavailable on non-Linux platforms.
type Controller struct{}

// NewController always returns an error on unsupported platforms.
func NewController() (*Controller, error) {
	return nil, errors.New("matrixdisplay: RGB LED matrix output is only supported on linux")
}

// Show is a no-op that reports the unsupported platform.
func (c *Controller) Show(image.Image) error {
	return errors.New("matrixdisplay: show not supported on this platform")
}

// Clear is a no-op that reports the unsupported platform.
func (c *Controller) Clear() error {
	return errors.New("matrixdisplay: clear not supported on this platform")
}

// Close is a no-op that reports the unsupported platform.
func (c *Controller) Close() error {
	return errors.New("matrixdisplay: close not supported on this platform")
}

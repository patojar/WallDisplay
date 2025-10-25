//go:build linux

package matrixdisplay

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"

	rgbmatrix "github.com/mcuadros/go-rpi-rgb-led-matrix"
)

const defaultBrightness = 60

// Controller manages a HUB75 RGB LED matrix and provides helpers to display
// 64x64 images on the panel.
type Controller struct {
	matrix rgbmatrix.Matrix
	canvas *rgbmatrix.Canvas
}

// NewController initializes the LED matrix and clears the display. Call Close
// when finished to release resources.
func NewController() (*Controller, error) {
	config := rgbmatrix.DefaultConfig
	config.Rows = PanelHeight
	config.Cols = PanelWidth
	config.ChainLength = 1
	config.Parallel = 1
	config.Brightness = defaultBrightness
	// Force the GPIO mapping expected by the Adafruit RGB Matrix Bonnet.
	config.HardwareMapping = "adafruit-hat-pwm"

	matrix, err := rgbmatrix.NewRGBLedMatrix(&config)
	if err != nil {
		return nil, fmt.Errorf("matrixdisplay: create matrix: %w", err)
	}

	canvas := rgbmatrix.NewCanvas(matrix)

	ctrl := &Controller{
		matrix: matrix,
		canvas: canvas,
	}

	if err := ctrl.Clear(); err != nil {
		_ = ctrl.Close()
		return nil, err
	}

	return ctrl, nil
}

// Show renders the supplied 64x64 image on the matrix.
func (c *Controller) Show(img image.Image) error {
	if img == nil {
		return fmt.Errorf("matrixdisplay: nil image")
	}
	bounds := img.Bounds()
	if bounds.Dx() != PanelWidth || bounds.Dy() != PanelHeight {
		return fmt.Errorf("matrixdisplay: image dimensions must be %dx%d, got %dx%d", PanelWidth, PanelHeight, bounds.Dx(), bounds.Dy())
	}

	draw.Draw(c.canvas, c.canvas.Bounds(), img, bounds.Min, draw.Src)
	if err := c.canvas.Render(); err != nil {
		return fmt.Errorf("matrixdisplay: render image: %w", err)
	}
	return nil
}

// Clear turns off all pixels on the matrix.
func (c *Controller) Clear() error {
	draw.Draw(c.canvas, c.canvas.Bounds(), &image.Uniform{color.Black}, image.Point{}, draw.Src)
	if err := c.canvas.Render(); err != nil {
		return fmt.Errorf("matrixdisplay: clear display: %w", err)
	}
	return nil
}

// Close clears the display and releases the underlying resources.
func (c *Controller) Close() error {
	return c.canvas.Close()
}

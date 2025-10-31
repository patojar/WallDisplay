package overlay

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Margin describes the pixel padding to keep between the rendered text and the edges of the image.
type Margin struct {
	Top   int
	Right int
}

var (
	fontOnce    sync.Once
	regularFont *opentype.Font
	fontErr     error
)

// loadFont parses the embedded Go regular font once using the opentype API.
func loadFont() (*opentype.Font, error) {
	fontOnce.Do(func() {
		parsed, err := opentype.Parse(goregular.TTF)
		if err != nil {
			fontErr = fmt.Errorf("parse embedded font: %w", err)
			return
		}
		regularFont = parsed
	})
	if fontErr != nil {
		return nil, fontErr
	}
	return regularFont, nil
}

// OverlayTopRightText places text in the top-right corner of a 64x64 image using the provided margin and text height.
// The original image is left unchanged; a copy with the overlay applied is returned instead.
func OverlayTopRightText(src image.Image, text string, margin Margin, textHeight float64) (*image.RGBA, error) {
	if src == nil {
		return nil, fmt.Errorf("nil source image")
	}
	if textHeight <= 0 {
		return nil, fmt.Errorf("text height must be positive")
	}
	if margin.Top < 0 || margin.Right < 0 {
		return nil, fmt.Errorf("margin values must be non-negative")
	}

	bounds := src.Bounds()
	if bounds.Dx() != 64 || bounds.Dy() != 64 {
		return nil, fmt.Errorf("expected 64x64 image, got %dx%d", bounds.Dx(), bounds.Dy())
	}

	dst := image.NewRGBA(bounds)
	draw.Draw(dst, bounds, src, bounds.Min, draw.Src)

	if text == "" {
		return dst, nil
	}

	fontParsed, err := loadFont()
	if err != nil {
		return nil, err
	}

	face, err := opentype.NewFace(fontParsed, &opentype.FaceOptions{
		Size:    textHeight,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("create font face: %w", err)
	}
	if closer, ok := face.(interface{ Close() error }); ok {
		defer closer.Close()
	}

	measureDrawer := font.Drawer{Face: face}
	textWidth := measureDrawer.MeasureString(text).Ceil()
	if textWidth <= 0 {
		return dst, nil
	}

	x := bounds.Max.X - margin.Right - textWidth
	if x < bounds.Min.X {
		x = bounds.Min.X
	}

	metrics := face.Metrics()
	baseline := bounds.Min.Y + margin.Top + metrics.Ascent.Round()
	if baseline > bounds.Max.Y {
		baseline = bounds.Max.Y
	}

	mask := image.NewAlpha(bounds)
	drawer := &font.Drawer{
		Dst:  mask,
		Src:  image.NewUniform(color.Opaque),
		Face: face,
		Dot: fixed.Point26_6{
			X: fixed.I(x),
			Y: fixed.I(baseline),
		},
	}
	drawer.DrawString(text)
	thresholdAlpha(mask, 0x80)

	draw.DrawMask(dst, bounds, image.NewUniform(color.White), image.Point{}, mask, bounds.Min, draw.Over)

	return dst, nil
}

func thresholdAlpha(img *image.Alpha, threshold uint8) {
	if img == nil {
		return
	}
	for i := range img.Pix {
		if img.Pix[i] < threshold {
			img.Pix[i] = 0
		} else {
			img.Pix[i] = 0xff
		}
	}
}

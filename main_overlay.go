package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"musicDisplay/overlay"
)

const (
	defaultOverlayTextHeight = 18.0
	defaultOverlayMargin     = 4
)

func generateOverlayImage(text, imagePath string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("overlay: text must not be empty")
	}
	imagePath = strings.TrimSpace(imagePath)
	if imagePath == "" {
		return "", fmt.Errorf("overlay: image path must not be empty")
	}
	if !strings.EqualFold(filepath.Ext(imagePath), ".png") {
		return "", fmt.Errorf("overlay: image path must point to a .png file")
	}

	src, err := loadAndScaleImage(imagePath)
	if err != nil {
		return "", fmt.Errorf("overlay: load base image: %w", err)
	}

	margin := overlay.Margin{
		Top:   defaultOverlayMargin,
		Right: defaultOverlayMargin,
	}
	result, err := overlay.OverlayTopRightText(src, text, margin, defaultOverlayTextHeight)
	if err != nil {
		return "", fmt.Errorf("overlay: apply text overlay: %w", err)
	}

	outputPath := overlayOutputPath(imagePath)

	file, err := os.Create(outputPath)
	if err != nil {
		return "", fmt.Errorf("overlay: create output %q: %w", outputPath, err)
	}
	defer file.Close()

	if err := png.Encode(file, result); err != nil {
		return "", fmt.Errorf("overlay: encode png: %w", err)
	}
	return outputPath, nil
}

func overlayOutputPath(srcPath string) string {
	ext := filepath.Ext(srcPath)
	base := strings.TrimSuffix(srcPath, ext)
	return fmt.Sprintf("%s-overlayed%s", base, ext)
}

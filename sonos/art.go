package sonos

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	imagedraw "image/draw"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "image/gif"
	_ "image/jpeg"

	xdraw "golang.org/x/image/draw"
)

// SaveAlbumArt downloads the current track art and stores a 64x64 PNG under ./art/.
// It returns true if the image exists after the call (either already present or newly written).
func SaveAlbumArt(ctx context.Context, device Device, room string, track TrackInfo, signature string) (bool, error) {
	artURI := strings.TrimSpace(track.AlbumArtURI)
	if artURI == "" {
		return false, nil
	}

	targetURL, err := resolveAlbumArtURL(device, artURI)
	if err != nil {
		return false, fmt.Errorf("resolve album art url: %w", err)
	}

	artCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(artCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		return false, fmt.Errorf("create album art request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	var resp *http.Response
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		resp, err = client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if resp.StatusCode == http.StatusOK {
			break
		}
		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			time.Sleep(200 * time.Millisecond)
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		resp.Body.Close()
		return false, fmt.Errorf("album art http status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	if resp == nil {
		return false, fmt.Errorf("fetch album art failed: %w", lastErr)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return false, fmt.Errorf("album art http status 404 after retries")
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return false, fmt.Errorf("album art http status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	const storedContentType = "image/png"
	path, err := albumArtPath(room, signature, storedContentType)
	if err != nil {
		return false, err
	}

	if _, err := os.Stat(path); err == nil {
		return true, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("create album art directory: %w", err)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("read album art body: %w", err)
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return false, fmt.Errorf("decode album art: %w", err)
	}

	img = cropToSquare(img)

	dst := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), xdraw.Over, nil)

	file, err := os.Create(path)
	if err != nil {
		return false, fmt.Errorf("create album art file: %w", err)
	}
	defer file.Close()

	if err := png.Encode(file, dst); err != nil {
		return false, fmt.Errorf("encode album art: %w", err)
	}

	return true, nil
}

func cropToSquare(img image.Image) image.Image {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width == height {
		return img
	}

	size := width
	if height < width {
		size = height
	}

	x0 := bounds.Min.X + (width-size)/2
	y0 := bounds.Min.Y + (height-size)/2
	cropRect := image.Rect(x0, y0, x0+size, y0+size)

	type subImager interface {
		SubImage(image.Rectangle) image.Image
	}
	if s, ok := img.(subImager); ok {
		return s.SubImage(cropRect)
	}

	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	imagedraw.Draw(dst, dst.Bounds(), img, cropRect.Min, imagedraw.Src)
	return dst
}

func albumArtPath(room, signature, contentType string) (string, error) {
	roomSlug := sanitizeForFilename(room)
	if roomSlug == "" {
		roomSlug = "room"
	}
	if signature == "" {
		return "", errors.New("album art signature empty")
	}
	hash := sha1.Sum([]byte(signature))
	hashHex := hex.EncodeToString(hash[:6])
	ext := extensionFromContentType(contentType)
	filename := fmt.Sprintf("%s-%s.%s", roomSlug, hashHex, ext)
	return filepath.Join("art", filename), nil
}

func sanitizeForFilename(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteRune(r)
		case r == ' ':
			builder.WriteRune('_')
		}
	}
	return strings.ToLower(builder.String())
}

func extensionFromContentType(contentType string) string {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if idx := strings.Index(contentType, ";"); idx >= 0 {
		contentType = strings.TrimSpace(contentType[:idx])
	}
	switch contentType {
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	case "":
		return "png"
	default:
		return "bin"
	}
}

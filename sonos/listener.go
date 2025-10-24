package sonos

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ListenForEvents subscribes to AVTransport events for the supplied device and
// prints updates for the provided room until the context is canceled.
func ListenForEvents(ctx context.Context, device Device, room, callbackPath string) error {
	bindAddr, err := determineLocalCallbackAddr(device)
	if err != nil {
		return err
	}
	bindAddr.Port = 0

	notifyCh := make(chan AVTransportEvent, 16)
	serverErrors := make(chan error, 1)
	lastState := ""
	lastTrackSignature := ""

	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "NOTIFY" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			log.Printf("warning: read event body: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		event, err := ParseAVTransportEvent(body)
		if err != nil {
			log.Printf("warning: parse event: %v", err)
			log.Printf("warning: event payload: %s", string(body))
		} else {
			select {
			case notifyCh <- event:
			default:
				log.Printf("warning: dropping event for %s (channel full)", room)
			}
		}
		w.WriteHeader(http.StatusOK)
	})

	server := &http.Server{Handler: mux}
	listener, err := net.ListenTCP("tcp", bindAddr)
	if err != nil {
		return fmt.Errorf("listen callback address: %w", err)
	}
	defer listener.Close()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok || addr == nil {
		return fmt.Errorf("listen callback address: unexpected address type %T", listener.Addr())
	}
	host := net.JoinHostPort(addr.IP.String(), strconv.Itoa(addr.Port))
	callbackURL := &url.URL{
		Scheme: "http",
		Host:   host,
		Path:   callbackPath,
	}
	log.Printf("info: callback listening on %s", callbackURL.String())

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	subCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	subscription, err := SubscribeAVTransport(subCtx, device, callbackURL.String(), 30*time.Minute)
	cancel()
	if err != nil {
		_ = server.Shutdown(context.Background())
		return err
	}
	log.Printf("info: subscribed to AVTransport events with SID %s", subscription.ID)

	var renewTicker *time.Ticker
	var renew <-chan time.Time
	if subscription.Timeout > 0 {
		interval := subscription.Timeout / 2
		if interval < time.Minute {
			interval = time.Minute
		}
		renewTicker = time.NewTicker(interval)
		renew = renewTicker.C
		defer renewTicker.Stop()
	}

	for {
		select {
		case <-ctx.Done():
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = server.Shutdown(shutdownCtx)
			shutdownCancel()
			unsubscribeCtx, unsubscribeCancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := UnsubscribeAVTransport(unsubscribeCtx, subscription)
			unsubscribeCancel()
			if err != nil {
				log.Printf("warning: unsubscribe failed: %v", err)
			}
			return nil
		case ev := <-notifyCh:
			state := formatStateDisplay(ev.TransportState)
			if state == "" {
				state = "Unknown"
			}
			display := formatTrackDisplay(ev.Track)
			if display == "" {
				display = "(idle)"
			}
			if shouldSkipDisplay(display) {
				continue
			}
			signature := trackSignature(ev.Track, display)
			if state == lastState && signature == lastTrackSignature {
				continue
			}
			lastState = state
			lastTrackSignature = signature
			fmt.Printf("[%s] %s – %s | %s\n", time.Now().Format("15:04:05"), room, state, display)
			if err := saveAlbumArt(ctx, device, room, ev.Track, signature); err != nil {
				log.Printf("warning: album art: %v", err)
			}
		case <-renew:
			renewCtx, renewCancel := context.WithTimeout(context.Background(), 5*time.Second)
			newTimeout, err := RenewAVTransport(renewCtx, subscription, subscription.Timeout)
			renewCancel()
			if err != nil {
				log.Printf("warning: renew subscription failed: %v", err)
				continue
			}
			if newTimeout > 0 {
				subscription.Timeout = newTimeout
				interval := newTimeout / 2
				if interval < time.Minute {
					interval = time.Minute
				}
				renewTicker.Reset(interval)
			}
		case err := <-serverErrors:
			_ = server.Shutdown(context.Background())
			return fmt.Errorf("callback server error: %w", err)
		}
	}
}

func determineLocalCallbackAddr(device Device) (*net.TCPAddr, error) {
	remoteIP := strings.TrimSpace(device.IP)
	remotePort := "1400"

	if location := strings.TrimSpace(device.Location); location != "" {
		if u, err := url.Parse(location); err == nil {
			if host := u.Hostname(); host != "" && remoteIP == "" {
				remoteIP = host
			}
			if port := u.Port(); port != "" {
				remotePort = port
			}
		}
	}

	if remoteIP == "" {
		return nil, errors.New("determine local callback address: device IP unknown")
	}

	dialAddr := net.JoinHostPort(remoteIP, remotePort)
	conn, err := net.DialTimeout("udp", dialAddr, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("determine local callback address: dial %s: %w", dialAddr, err)
	}
	defer conn.Close()

	udpAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || udpAddr == nil {
		return nil, fmt.Errorf("determine local callback address: unexpected local addr %T", conn.LocalAddr())
	}

	ip := udpAddr.IP
	if ip == nil || ip.IsUnspecified() {
		return nil, errors.New("determine local callback address: resolved unspecified IP")
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}

	return &net.TCPAddr{IP: ip, Zone: udpAddr.Zone}, nil
}

func trackSignature(info TrackInfo, display string) string {
	fields := []string{
		info.Title,
		info.Artist,
		info.Album,
		info.StreamInfo,
		info.URI,
		display,
	}
	for i := range fields {
		fields[i] = strings.ToLower(strings.TrimSpace(fields[i]))
	}
	return strings.Join(fields, "|")
}

func shouldSkipDisplay(value string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "x-sonos")
}

func saveAlbumArt(ctx context.Context, device Device, room string, track TrackInfo, signature string) error {
	artURI := strings.TrimSpace(track.AlbumArtURI)
	if artURI == "" {
		return nil
	}

	targetURL, err := resolveAlbumArtURL(device, artURI)
	if err != nil {
		return fmt.Errorf("resolve album art url: %w", err)
	}

	artCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(artCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		return fmt.Errorf("create album art request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch album art: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("album art http status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	contentType := parseContentType(resp.Header.Get("Content-Type"))
	path, err := albumArtPath(room, signature, contentType)
	if err != nil {
		return err
	}

	if _, err := os.Stat(path); err == nil {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create album art directory: %w", err)
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create album art file: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("write album art body: %w", err)
	}

	return nil
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
		return "jpg"
	default:
		return "bin"
	}
}

func parseContentType(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if idx := strings.Index(raw, ";"); idx >= 0 {
		return strings.TrimSpace(strings.ToLower(raw[:idx]))
	}
	return strings.ToLower(raw)
}

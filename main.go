package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"musicDisplay/sonos"
)

const (
	discoveryTimeout       = 8 * time.Second
	enrichmentPerDevice    = 10 * time.Second
	enrichmentMinimumTotal = 30 * time.Second
	defaultConfigPath      = "config.json"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := loadConfig(defaultConfigPath)
	if err != nil {
		log.Printf("warning: %v", err)
	}
	targetRoom := strings.TrimSpace(cfg.Room)
	if targetRoom != "" {
		log.Printf("info: filtering to room %q", targetRoom)
	}

	discoveryCtx, discoveryCancel := context.WithTimeout(ctx, discoveryTimeout)
	devices, err := sonos.Discover(discoveryCtx, discoveryTimeout)
	discoveryCancel()
	if err != nil {
		log.Fatalf("failed to discover Sonos devices: %v", err)
	}

	if len(devices) == 0 {
		fmt.Println("No Sonos-compatible responders found via SSDP.")
		return
	}

	enrichmentWindow := time.Duration(len(devices)) * enrichmentPerDevice
	if enrichmentWindow < enrichmentMinimumTotal {
		enrichmentWindow = enrichmentMinimumTotal
	}
	enrichmentCtx, enrichmentCancel := context.WithTimeout(ctx, enrichmentWindow)
	defer enrichmentCancel()

	enriched, err := sonos.EnrichDevices(enrichmentCtx, devices)
	if err != nil {
		log.Printf("warning: failed to enrich all devices: %v", err)
	}
	devices = enriched

	type roomStatus struct {
		roomName string
		track    string
		state    string
	}

	var statuses []roomStatus

	var targetDevice *sonos.Device

	for i := range devices {
		device := devices[i]
		if !device.IsSonos {
			log.Printf("ignoring non-Sonos responder at %s (%s)", device.IP, device.Server)
			continue
		}
		room := strings.TrimSpace(device.Metadata.RoomName)
		if room == "" {
			room = deriveFallbackRoomName(device, device.Metadata)
		}
		if room == "" {
			room = deriveFallbackName(device)
		}
		if targetRoom != "" && !roomMatches(room, targetRoom) {
			continue
		}

		if targetRoom != "" && roomMatches(room, targetRoom) && targetDevice == nil {
			targetDevice = &devices[i]
		}

		playbackCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		info, err := sonos.NowPlaying(playbackCtx, device)
		cancel()
		var track string
		state := "Unknown"
		if err != nil {
			log.Printf("warning: now playing for %s: %v", room, err)
			track = "Unavailable"
			state = "Unavailable"
		} else {
			track = formatTrackDisplay(info)
			if track == "" {
				track = "(idle)"
			}
			state = formatStateDisplay(info.State)
			if state == "" {
				state = "Unknown"
			}
		}
		statuses = append(statuses, roomStatus{roomName: room, track: track, state: state})
	}

	if len(statuses) == 0 {
		fmt.Println("No Sonos devices found after filtering.")
		return
	}

	roomColumnWidth := len("Room")
	stateColumnWidth := len("State")
	for _, status := range statuses {
		if len(status.roomName) > roomColumnWidth {
			roomColumnWidth = len(status.roomName)
		}
		if len(status.state) > stateColumnWidth {
			stateColumnWidth = len(status.state)
		}
	}

	fmt.Printf("%-*s  %-*s  %s\n", roomColumnWidth, "Room", stateColumnWidth, "State", "Now Playing")
	fmt.Printf("%s  %s  %s\n", strings.Repeat("-", roomColumnWidth), strings.Repeat("-", stateColumnWidth), strings.Repeat("-", len("Now Playing")))
	for _, status := range statuses {
		fmt.Printf("%-*s  %-*s  %s\n", roomColumnWidth, status.roomName, stateColumnWidth, status.state, status.track)
	}

	if targetRoom == "" {
		return
	}

	if cfg.CallbackURL == "" {
		log.Printf("info: callback_url not configured; skipping event subscription")
		return
	}

	if targetDevice == nil {
		log.Printf("warning: no device matched room %q for subscription", targetRoom)
		return
	}

	fmt.Println("Listening for updates. Press Ctrl+C to exit.")
	if err := listenForEvents(ctx, *targetDevice, targetRoom, cfg); err != nil {
		log.Printf("warning: %v", err)
	}
}

func deriveFallbackName(device sonos.Device) string {
	if friendly, ok := device.Headers["FRIENDLYNAME"]; ok && strings.TrimSpace(friendly) != "" {
		return friendly
	}
	return device.USN
}

func deriveFallbackRoomName(device sonos.Device, meta sonos.DeviceMetadata) string {
	if room, ok := device.Headers["ROOMNAME"]; ok && strings.TrimSpace(room) != "" {
		return strings.TrimSpace(room)
	}
	if strings.TrimSpace(meta.FriendlyName) != "" {
		parts := strings.Fields(meta.FriendlyName)
		if len(parts) > 1 {
			return strings.Join(parts[:len(parts)-1], " ")
		}
	}
	return ""
}

func formatTrackDisplay(info sonos.TrackInfo) string {
	title := strings.TrimSpace(info.Title)
	artist := strings.TrimSpace(info.Artist)
	switch {
	case title != "" && artist != "":
		return fmt.Sprintf("%s - %s", artist, title)
	case title != "":
		return title
	case artist != "":
		return artist
	}
	if strings.TrimSpace(info.StreamInfo) != "" {
		return strings.TrimSpace(info.StreamInfo)
	}
	if strings.TrimSpace(info.URI) != "" {
		return strings.TrimSpace(info.URI)
	}
	return ""
}

func formatStateDisplay(raw string) string {
	state := strings.ToUpper(strings.TrimSpace(raw))
	switch state {
	case "PLAYING":
		return "Playing"
	case "PAUSED_PLAYBACK":
		return "Paused"
	case "STOPPED":
		return "Stopped"
	case "TRANSITIONING":
		return "Transitioning"
	case "NO_MEDIA_PRESENT":
		return "No Media"
	case "":
		return ""
	default:
		return raw
	}
}

func roomMatches(roomName, target string) bool {
	return strings.EqualFold(strings.TrimSpace(roomName), strings.TrimSpace(target))
}

func listenForEvents(ctx context.Context, device sonos.Device, room string, cfg Config) error {
	callbackURL, err := url.Parse(strings.TrimSpace(cfg.CallbackURL))
	if err != nil {
		return fmt.Errorf("invalid callback_url: %w", err)
	}
	if !strings.EqualFold(callbackURL.Scheme, "http") {
		return fmt.Errorf("callback_url must use http scheme")
	}
	if callbackURL.Host == "" {
		return fmt.Errorf("callback_url missing host:port")
	}
	path := callbackURL.Path
	if path == "" {
		path = "/"
	}

	notifyCh := make(chan sonos.AVTransportEvent, 16)
	serverErrors := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
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
		event, err := sonos.ParseAVTransportEvent(body)
		if err != nil {
			log.Printf("warning: parse event: %v", err)
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
	listener, err := net.Listen("tcp", callbackURL.Host)
	if err != nil {
		return fmt.Errorf("listen callback address: %w", err)
	}

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	subCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	subscription, err := sonos.SubscribeAVTransport(subCtx, device, callbackURL.String(), 30*time.Minute)
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
			err := sonos.UnsubscribeAVTransport(unsubscribeCtx, subscription)
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
			track := formatTrackDisplay(ev.Track)
			if track == "" {
				track = "(idle)"
			}
			fmt.Printf("[%s] %s – %s | %s\n", time.Now().Format("15:04:05"), room, state, track)
		case <-renew:
			renewCtx, renewCancel := context.WithTimeout(context.Background(), 5*time.Second)
			newTimeout, err := sonos.RenewAVTransport(renewCtx, subscription, subscription.Timeout)
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
			return fmt.Errorf("callback server error: %w", err)
		}
	}
}

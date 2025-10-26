package sonos

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Display abstracts the image rendering backend (e.g. an RGB LED matrix).
type Display interface {
	Show(image.Image) error
	Clear() error
}

// ListenerOptions customises runtime behaviour for ListenForEvents.
type ListenerOptions struct {
	Debug       bool
	Display     Display
	IdleTimeout time.Duration
}

// ListenForEvents subscribes to AVTransport events for the supplied device and
// prints updates for the provided room until the context is canceled.
func ListenForEvents(ctx context.Context, device Device, room, callbackPath string, opts ListenerOptions) error {
	// default idle timeout
	if opts.IdleTimeout <= 0 {
		opts.IdleTimeout = 5 * time.Minute
	}

	bindAddr, err := determineLocalCallbackAddr(device)
	if err != nil {
		return err
	}
	bindAddr.Port = 0

	notifyCh := make(chan AVTransportEvent, 16)
	serverErrors := make(chan error, 1)
	lastState := ""
	lastTrackSignature := ""
	savedArtSignature := ""
	displayActive := false
	cacheToDisk := opts.Display == nil
	var idleTimer *time.Timer
	var idleTimerCh <-chan time.Time

	stopIdleTimer := func() {
		if idleTimer != nil {
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer = nil
			idleTimerCh = nil
		}
	}

	startIdleTimer := func() {
		if opts.Display == nil || opts.IdleTimeout <= 0 {
			return
		}
		if idleTimer == nil {
			idleTimer = time.NewTimer(opts.IdleTimeout)
			idleTimerCh = idleTimer.C
			return
		}
		if !idleTimer.Stop() {
			select {
			case <-idleTimer.C:
			default:
			}
		}
		idleTimer.Reset(opts.IdleTimeout)
	}

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
	logInfo("info: callback listening on %s", callbackURL.String())

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
	logInfo("info: subscribed to AVTransport events with SID %s", subscription.ID)

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
			stateChanged := state != lastState || signature != lastTrackSignature
			shouldPrint := opts.Debug && stateChanged
			needArt := signature != "" && signature != savedArtSignature
			idleState := display == "(idle)" || strings.EqualFold(state, "No Media") || strings.EqualFold(state, "Stopped")
			isPlaying := strings.EqualFold(state, "Playing")

			if isPlaying {
				stopIdleTimer()
			} else {
				startIdleTimer()
			}

			if opts.Debug {
				logDebug("debug: event room=%s state=%s display=%s sig=%s stateChanged=%t shouldPrint=%t needArt=%t idle=%t timerActive=%t", room, state, display, signature, stateChanged, shouldPrint, needArt, idleState, idleTimer != nil)
			}

			if !stateChanged && !needArt {
				continue
			}
			if stateChanged {
				lastState = state
				lastTrackSignature = signature
			}
			if shouldPrint {
				fmt.Printf("[%s] %s – %s | %s\n", time.Now().Format("15:04:05"), room, state, display)
			}
			if needArt {
				img, err := SaveAlbumArt(ctx, device, room, ev.Track, signature, cacheToDisk)
				if err != nil {
					log.Printf("warning: album art: %v", err)
				} else if img != nil {
					savedArtSignature = signature
					if opts.Display != nil {
						if err := opts.Display.Show(img); err != nil {
							log.Printf("warning: update display: %v", err)
						} else {
							displayActive = true
						}
					}
				}
			}
		case <-idleTimerCh:
			stopIdleTimer()
			if opts.Display != nil && displayActive {
				if err := opts.Display.Clear(); err != nil {
					log.Printf("warning: clear display after idle timeout: %v", err)
				}
				displayActive = false
			}
			savedArtSignature = ""
			if opts.Debug {
				logDebug("debug: idle timeout reached; display cleared for room %s", room)
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

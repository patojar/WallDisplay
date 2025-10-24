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
	"strconv"
	"strings"
	"time"

	"musicDisplay/sonos"
)

func listenForEvents(ctx context.Context, device sonos.Device, room string) error {
	bindAddr, err := determineLocalCallbackAddr(device)
	if err != nil {
		return err
	}
	bindAddr.Port = 0

	notifyCh := make(chan sonos.AVTransportEvent, 16)
	serverErrors := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc(defaultCallbackPath, func(w http.ResponseWriter, r *http.Request) {
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
		Path:   defaultCallbackPath,
	}
	log.Printf("info: callback listening on %s", callbackURL.String())

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
			_ = server.Shutdown(context.Background())
			return fmt.Errorf("callback server error: %w", err)
		}
	}
}

func determineLocalCallbackAddr(device sonos.Device) (*net.TCPAddr, error) {
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

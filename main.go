package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"musicDisplay/sonos"
)

const (
	discoveryTimeout       = 30 * time.Second
	enrichmentPerDevice    = 10 * time.Second
	enrichmentMinimumTotal = 30 * time.Second
)

func main() {
	discoveryCtx, discoveryCancel := context.WithTimeout(context.Background(), discoveryTimeout)
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
	enrichmentCtx, enrichmentCancel := context.WithTimeout(context.Background(), enrichmentWindow)
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

	for _, device := range devices {
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

		playbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

package sonos

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// RoomStatus represents the playback state of a Sonos room.
type RoomStatus struct {
	Room  string
	State string
	Track string
}

// GatherRoomStatuses collects the playback status for each discovered device. If
// targetRoom is supplied, it returns a pointer to the first matching device to
// support subsequent event subscriptions.
func GatherRoomStatuses(ctx context.Context, devices []Device, targetRoom string) ([]RoomStatus, *Device) {
	statuses := make([]RoomStatus, 0, len(devices))

	var targetDevice *Device

	for i := range devices {
		device := devices[i]
		if !device.IsSonos {
			log.Printf("ignoring non-Sonos responder at %s (%s)", device.IP, device.Server)
			continue
		}

		room := deriveRoomName(device)
		if targetRoom != "" && !roomMatches(room, targetRoom) {
			continue
		}

		if targetRoom != "" && targetDevice == nil && roomMatches(room, targetRoom) {
			targetDevice = &devices[i]
		}

		statuses = append(statuses, buildRoomStatus(ctx, device, room))
	}

	return statuses, targetDevice
}

// PrintRoomStatuses renders the collected statuses in a table format.
func PrintRoomStatuses(statuses []RoomStatus) {
	roomColumnWidth := len("Room")
	stateColumnWidth := len("State")
	for _, status := range statuses {
		if len(status.Room) > roomColumnWidth {
			roomColumnWidth = len(status.Room)
		}
		if len(status.State) > stateColumnWidth {
			stateColumnWidth = len(status.State)
		}
	}

	fmt.Printf("%-*s  %-*s  %s\n", roomColumnWidth, "Room", stateColumnWidth, "State", "Now Playing")
	fmt.Printf("%s  %s  %s\n", strings.Repeat("-", roomColumnWidth), strings.Repeat("-", stateColumnWidth), strings.Repeat("-", len("Now Playing")))
	for _, status := range statuses {
		fmt.Printf("%-*s  %-*s  %s\n", roomColumnWidth, status.Room, stateColumnWidth, status.State, status.Track)
	}
}

func buildRoomStatus(ctx context.Context, device Device, room string) RoomStatus {
	playbackCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	info, err := NowPlaying(playbackCtx, device)
	if err != nil {
		log.Printf("warning: now playing for %s: %v", room, err)
		return RoomStatus{
			Room:  room,
			State: "Unavailable",
			Track: "Unavailable",
		}
	}

	track := formatTrackDisplay(info)
	if track == "" {
		track = "(idle)"
	}

	state := formatStateDisplay(info.State)
	if state == "" {
		state = "Unknown"
	}

	return RoomStatus{
		Room:  room,
		State: state,
		Track: track,
	}
}

func deriveRoomName(device Device) string {
	if room := strings.TrimSpace(device.Metadata.RoomName); room != "" {
		return room
	}
	if room := deriveFallbackRoomName(device, device.Metadata); room != "" {
		return room
	}
	return deriveFallbackName(device)
}

func deriveFallbackName(device Device) string {
	if friendly, ok := device.Headers["FRIENDLYNAME"]; ok && strings.TrimSpace(friendly) != "" {
		return friendly
	}
	return device.USN
}

func deriveFallbackRoomName(device Device, meta DeviceMetadata) string {
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

func formatTrackDisplay(info TrackInfo) string {
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

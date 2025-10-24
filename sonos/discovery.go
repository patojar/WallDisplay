package sonos

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"sort"
	"strings"
	"time"
)

const (
	ssdpAddress     = "239.255.255.250:1900"
	ssdpSearch      = "urn:schemas-upnp-org:device:ZonePlayer:1"
	ssdpTimeout     = 250 * time.Millisecond
	ssdpQuietPeriod = 1 * time.Second
)

var ssdpUDPAddr = &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}

// Device contains basic metadata about a discovered Sonos device.
type Device struct {
	IP       string
	Location string
	Server   string
	ST       string
	USN      string
	Headers  map[string]string

	Metadata DeviceMetadata
	IsSonos  bool
}

// Discover queries the local network for Sonos devices using SSDP.
// The context governs the lifetime of the discovery. A zero timeout
// falls back to a sensible default. If targetRoom is non-empty, discovery
// stops as soon as a matching device is observed.
func Discover(ctx context.Context, timeout time.Duration, targetRoom string) ([]Device, error) {
	if ctx == nil {
		return nil, errors.New("sonos: nil context")
	}

	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	targetRoomCanonical := canonicalRoomName(targetRoom)

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, fmt.Errorf("sonos: listen UDP: %w", err)
	}
	defer conn.Close()

	if err := sendSearchRequests(conn, ssdpUDPAddr); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(timeout)
	buf := make([]byte, 2048)
	indexByKey := make(map[string]int)
	devices := make([]Device, 0, 4)

	lastResponse := time.Time{}
	for {
		if ctx.Err() != nil {
			break
		}
		if time.Now().After(deadline) {
			break
		}

		remaining := time.Until(deadline)
		readDeadline := time.Now().Add(ssdpTimeout)
		if remaining < ssdpTimeout {
			readDeadline = time.Now().Add(remaining)
		}

		if err := conn.SetReadDeadline(readDeadline); err != nil {
			return nil, fmt.Errorf("sonos: set read deadline: %w", err)
		}

		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				if !lastResponse.IsZero() && time.Since(lastResponse) >= ssdpQuietPeriod {
					break
				}
				continue
			}
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				break
			}
			return nil, fmt.Errorf("sonos: read response: %w", err)
		}

		device, err := parseResponse(buf[:n])
		if err != nil {
			// Ignore malformed responses.
			continue
		}
		device.IP = addr.IP.String()

		lastResponse = time.Now()

		if targetRoomCanonical != "" && device.IsSonos && roomMatchesHeader(device, targetRoomCanonical) {
			return []Device{device}, nil
		}

		key := device.USN
		if key == "" {
			key = device.IP
		}
		if idx, ok := indexByKey[key]; ok {
			devices[idx] = device
		} else {
			indexByKey[key] = len(devices)
			devices = append(devices, device)
		}
	}

	if len(devices) == 0 {
		return nil, nil
	}

	sort.Slice(devices, func(i, j int) bool {
		if devices[i].IP == devices[j].IP {
			return devices[i].Location < devices[j].Location
		}
		return devices[i].IP < devices[j].IP
	})

	return devices, nil
}

func canonicalRoomName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func roomMatchesHeader(device Device, targetCanonical string) bool {
	if targetCanonical == "" {
		return false
	}
	for _, candidate := range headerRoomCandidates(device) {
		if candidate == targetCanonical {
			return true
		}
	}
	return false
}

func headerRoomCandidates(device Device) []string {
	candidates := make([]string, 0, 3)
	seen := make(map[string]struct{}, 3)

	if room := canonicalRoomName(device.Headers["ROOMNAME"]); room != "" {
		candidates = append(candidates, room)
		seen[room] = struct{}{}
	}

	if friendlyRaw := strings.TrimSpace(device.Headers["FRIENDLYNAME"]); friendlyRaw != "" {
		if friendly := canonicalRoomName(friendlyRaw); friendly != "" {
			if _, ok := seen[friendly]; !ok {
				candidates = append(candidates, friendly)
				seen[friendly] = struct{}{}
			}
		}

		parts := strings.Fields(friendlyRaw)
		if len(parts) > 1 {
			base := canonicalRoomName(strings.Join(parts[:len(parts)-1], " "))
			if base != "" {
				if _, ok := seen[base]; !ok {
					candidates = append(candidates, base)
					seen[base] = struct{}{}
				}
			}
		}
	}

	return candidates
}

func sendSearchRequests(conn *net.UDPConn, target *net.UDPAddr) error {
	message := strings.Join([]string{
		"M-SEARCH * HTTP/1.1",
		"HOST: " + ssdpAddress,
		"MAN: \"ssdp:discover\"",
		"MX: 1",
		"ST: " + ssdpSearch,
		"",
		"",
	}, "\r\n")

	payload := []byte(message)

	for i := 0; i < 3; i++ {
		if err := conn.SetWriteDeadline(time.Now().Add(ssdpTimeout)); err != nil {
			return fmt.Errorf("sonos: set write deadline: %w", err)
		}
		if _, err := conn.WriteToUDP(payload, target); err != nil {
			return fmt.Errorf("sonos: write SSDP search: %w", err)
		}
	}

	return nil
}

func parseResponse(data []byte) (Device, error) {
	reader := bufio.NewReader(bytes.NewReader(data))
	tp := textproto.NewReader(reader)

	statusLine, err := tp.ReadLine()
	if err != nil {
		return Device{}, fmt.Errorf("sonos: read status line: %w", err)
	}

	if !strings.HasPrefix(strings.ToUpper(statusLine), "HTTP/1.1 200") {
		return Device{}, fmt.Errorf("sonos: unexpected status line: %q", statusLine)
	}

	headers, err := tp.ReadMIMEHeader()
	if err != nil {
		return Device{}, fmt.Errorf("sonos: read headers: %w", err)
	}

	flat := make(map[string]string, len(headers))
	for key, values := range headers {
		if len(values) > 0 {
			flat[strings.ToUpper(key)] = values[0]
		}
	}

	device := Device{
		Location: flat["LOCATION"],
		Server:   flat["SERVER"],
		ST:       flat["ST"],
		USN:      flat["USN"],
		Headers:  flat,
	}
	device.IsSonos = looksLikeSonosFromHeaders(device)

	return device, nil
}

func looksLikeSonosFromHeaders(device Device) bool {
	server := strings.ToLower(device.Server)
	if strings.Contains(server, "sonos") {
		return true
	}

	st := strings.ToLower(device.ST)
	if strings.Contains(st, "sonos") || strings.Contains(st, "zoneplayer") {
		return true
	}

	usn := strings.ToLower(device.USN)
	if strings.Contains(usn, "rincon") {
		return true
	}

	return false
}

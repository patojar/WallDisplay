package sonos

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DeviceMetadata holds fields extracted from the UPnP device description XML.
type DeviceMetadata struct {
	DeviceType      string
	FriendlyName    string
	Manufacturer    string
	RoomName        string
	ModelName       string
	ModelNumber     string
	SerialNumber    string
	SoftwareVersion string
}

// enrichMetadata pulls the device description XML and updates metadata fields on the Device.
func enrichMetadata(ctx context.Context, device Device) (Device, error) {
	if device.Location == "" {
		return device, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, device.Location, nil)
	if err != nil {
		return device, fmt.Errorf("sonos: create metadata request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return device, fmt.Errorf("sonos: fetch metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return device, fmt.Errorf("sonos: metadata http status %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return device, fmt.Errorf("sonos: read metadata body: %w", err)
	}

	meta, err := parseDeviceDescription(body)
	if err != nil {
		return device, err
	}

	device.Metadata = meta
	device.IsSonos = device.IsSonos || isSonosDevice(meta)

	return device, nil
}

func parseDeviceDescription(body []byte) (DeviceMetadata, error) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	var stack []xml.StartElement
	var meta DeviceMetadata
	capturing := false
	deviceDepth := 0

	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return meta, fmt.Errorf("sonos: decode metadata xml: %w", err)
		}

		switch tok := token.(type) {
		case xml.StartElement:
			stack = append(stack, tok)
			if !capturing && len(stack) >= 2 {
				parent := stack[len(stack)-2]
				if parent.Name.Local == "root" && tok.Name.Local == "device" {
					capturing = true
					deviceDepth = len(stack)
				}
			}
		case xml.EndElement:
			if capturing && tok.Name.Local == "device" && len(stack) == deviceDepth {
				stack = stack[:len(stack)-1]
				capturing = false
				return meta, nil
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if !capturing {
				continue
			}
			if len(stack) != deviceDepth+1 {
				continue
			}
			value := strings.TrimSpace(string(tok))
			if value == "" {
				continue
			}
			field := stack[len(stack)-1].Name.Local
			switch field {
			case "deviceType":
				meta.DeviceType = value
			case "friendlyName":
				meta.FriendlyName = value
			case "manufacturer":
				meta.Manufacturer = value
			case "roomName":
				meta.RoomName = value
			case "modelName":
				meta.ModelName = value
			case "modelNumber":
				meta.ModelNumber = value
			case "serialNumber":
				meta.SerialNumber = value
			case "softwareVersion":
				meta.SoftwareVersion = value
			}
		}
	}

	if meta.DeviceType != "" || meta.FriendlyName != "" {
		return meta, nil
	}

	return meta, fmt.Errorf("sonos: metadata missing top-level device information")
}

func isSonosDevice(meta DeviceMetadata) bool {
	manufacturer := strings.ToLower(meta.Manufacturer)
	if strings.Contains(manufacturer, "sonos") {
		return true
	}

	deviceType := strings.ToLower(meta.DeviceType)
	if strings.Contains(deviceType, "zoneplayer") || strings.Contains(deviceType, "sonos") {
		return true
	}

	modelName := strings.ToLower(meta.ModelName)
	if strings.Contains(modelName, "sonos") {
		return true
	}

	return false
}

// EnrichDevices walks over each device and attempts to download and parse metadata.
// Devices collected before an error are returned alongside the error so callers can
// decide whether to continue.
func EnrichDevices(ctx context.Context, devices []Device) ([]Device, error) {
	enriched := make([]Device, len(devices))
	copy(enriched, devices)

	var firstErr error
	for i, device := range enriched {
		if ctx.Err() != nil {
			return enriched, ctx.Err()
		}

		localCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		updated, err := enrichMetadata(localCtx, device)
		cancel()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		enriched[i] = updated
	}

	return enriched, firstErr
}

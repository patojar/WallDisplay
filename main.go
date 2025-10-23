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

	count := 0
	for _, device := range devices {
		if !device.IsSonos {
			log.Printf("ignoring non-Sonos responder at %s (%s)", device.IP, device.Server)
			continue
		}
		count++
		meta := device.Metadata
		friendly := meta.FriendlyName
		if friendly == "" {
			friendly = deriveFallbackName(device)
		}
		room := strings.TrimSpace(meta.RoomName)
		if room == "" {
			room = deriveFallbackRoomName(device, meta)
		}
		model := strings.TrimSpace(fmt.Sprintf("%s %s", meta.Manufacturer, meta.ModelName))
		if strings.TrimSpace(model) == "" {
			model = device.Server
		}
		fmt.Printf("- %s (%s)\n", friendly, device.IP)
		fmt.Printf("  Model: %s | Serial: %s | SW: %s\n", model, meta.SerialNumber, meta.SoftwareVersion)
		if room != "" {
			fmt.Printf("  Room: %s\n", room)
		}
		fmt.Printf("  Location: %s\n", device.Location)
	}

	if count == 0 {
		fmt.Println("No Sonos devices found after filtering.")
	} else {
		fmt.Printf("Found %d Sonos device(s).\n", count)
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

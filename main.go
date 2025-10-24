package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	discoveryTimeout       = 8 * time.Second
	enrichmentPerDevice    = 10 * time.Second
	enrichmentMinimumTotal = 30 * time.Second
	defaultConfigPath      = "config.json"
	defaultCallbackPath    = "/sonos/events"
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

	devices, err := discoverDevices(ctx, targetRoom)
	if err != nil {
		log.Fatalf("failed to discover Sonos devices: %v", err)
	}
	if len(devices) == 0 {
		fmt.Println("No Sonos-compatible responders found via SSDP.")
		return
	}

	devices, enrichmentErr := enrichDeviceMetadata(ctx, devices)
	if enrichmentErr != nil {
		log.Printf("warning: failed to enrich all devices: %v", enrichmentErr)
	}

	statuses, targetDevice := gatherRoomStatuses(ctx, devices, targetRoom)
	if len(statuses) == 0 {
		fmt.Println("No Sonos devices found after filtering.")
		return
	}

	printRoomStatuses(statuses)

	if targetRoom == "" {
		return
	}

	if targetDevice == nil {
		log.Printf("warning: no device matched room %q for subscription", targetRoom)
		return
	}

	fmt.Println("Listening for updates. Press Ctrl+C to exit.")
	if err := listenForEvents(ctx, *targetDevice, targetRoom); err != nil {
		log.Printf("warning: %v", err)
	}
}

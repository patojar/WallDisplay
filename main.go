package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"musicDisplay/matrixdisplay"
	"musicDisplay/sonos"
)

const (
	discoveryTimeout       = 8 * time.Second
	enrichmentPerDevice    = 10 * time.Second
	enrichmentMinimumTotal = 30 * time.Second
	defaultConfigPath      = "config.json"
	defaultCallbackPath    = "/sonos/events"
)

var debugMode bool

func infof(format string, args ...interface{}) {
	if debugMode {
		log.Printf("info: "+format, args...)
	}
}

func main() {
	debugFlag := flag.Bool("debug", false, "enable debug logging")
	displayFlag := flag.Bool("display", false, "enable RGB LED matrix output")
	flag.Parse()

	debugMode = *debugFlag
	sonos.SetDebugLogging(debugMode)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := loadConfig(defaultConfigPath)
	if err != nil {
		log.Printf("warning: %v", err)
	}

	targetRoom := strings.TrimSpace(cfg.Room)
	if targetRoom != "" {
		infof("filtering to room %q", targetRoom)
	}

	discoveryCtx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	devices, err := sonos.Discover(discoveryCtx, discoveryTimeout, targetRoom)
	cancel()
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
	enrichmentCtx, cancel := context.WithTimeout(ctx, enrichmentWindow)
	enriched, enrichmentErr := sonos.EnrichDevices(enrichmentCtx, devices)
	cancel()
	if len(enriched) > 0 {
		devices = enriched
	}
	if enrichmentErr != nil {
		log.Printf("warning: failed to enrich all devices: %v", enrichmentErr)
	}

	statuses, targetDevice := sonos.GatherRoomStatuses(ctx, devices, targetRoom)
	if len(statuses) == 0 {
		fmt.Println("No Sonos devices found after filtering.")
		return
	}

	sonos.PrintRoomStatuses(statuses)

	if targetRoom == "" {
		return
	}

	if targetDevice == nil {
		log.Printf("warning: no device matched room %q for subscription", targetRoom)
		return
	}

	var display *matrixdisplay.Controller
	if *displayFlag {
		ctrl, err := matrixdisplay.NewController()
		if err != nil {
			log.Printf("warning: init matrix display: %v", err)
		} else {
			display = ctrl
			infof("matrix display initialized")
			defer func() {
				if err := display.Close(); err != nil {
					log.Printf("warning: close display: %v", err)
				}
			}()
		}
	} else {
		infof("matrix display disabled")
	}

	fmt.Println("Listening for updates. Press Ctrl+C to exit.")
	opts := sonos.ListenerOptions{
		Debug:       debugMode,
		Display:     display,
		IdleTimeout: 5 * time.Minute,
	}
	if err := sonos.ListenForEvents(ctx, *targetDevice, targetRoom, defaultCallbackPath, opts); err != nil {
		log.Printf("warning: %v", err)
	}
}

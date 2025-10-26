package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/image/draw"

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
	displayTestFlag := flag.String("display-test", "", "path to an image to display on the matrix and exit")
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

	var brightness int
	if cfg.Brightness != nil {
		brightness = *cfg.Brightness
		infof("matrix brightness override set to %d", brightness)
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
	needDisplay := *displayFlag || strings.TrimSpace(*displayTestFlag) != ""
	if needDisplay {
		ctrl, err := matrixdisplay.NewController(brightness)
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

	if display == nil && strings.TrimSpace(*displayTestFlag) != "" {
		log.Printf("warning: display test requested but matrix initialization failed")
	}

	if display != nil && strings.TrimSpace(*displayTestFlag) != "" {
		if err := showTestImage(ctx, display, strings.TrimSpace(*displayTestFlag)); err != nil {
			log.Fatalf("display test failed: %v", err)
		}
		return
	}

	fmt.Println("Listening for updates. Press Ctrl+C to exit.")
	opts := sonos.ListenerOptions{
		Debug:       debugMode,
		Display:     display,
		IdleTimeout: 2 * time.Minute,
	}
	if err := sonos.ListenForEvents(ctx, *targetDevice, targetRoom, defaultCallbackPath, opts); err != nil {
		log.Printf("warning: %v", err)
	}
}

func showTestImage(ctx context.Context, display *matrixdisplay.Controller, path string) error {
	img, err := loadAndScaleImage(path)
	if err != nil {
		return err
	}

	if err := display.Show(img); err != nil {
		return fmt.Errorf("matrixdisplay: show test image: %w", err)
	}

	fmt.Printf("Displayed %q on the matrix. Press Ctrl+C to exit.\n", path)
	select {
	case <-ctx.Done():
		return nil
	}
}

func loadAndScaleImage(path string) (image.Image, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("matrixdisplay: image path is empty")
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("matrixdisplay: open image %q: %w", path, err)
	}
	defer file.Close()

	src, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("matrixdisplay: decode image %q: %w", path, err)
	}

	srcBounds := src.Bounds()
	if srcBounds.Dx() == matrixdisplay.PanelWidth && srcBounds.Dy() == matrixdisplay.PanelHeight {
		return src, nil
	}

	dst := image.NewRGBA(image.Rect(0, 0, matrixdisplay.PanelWidth, matrixdisplay.PanelHeight))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, srcBounds, draw.Src, nil)
	return dst, nil
}

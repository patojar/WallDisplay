package main

import (
	"context"
	"time"

	"musicDisplay/sonos"
)

func discoverDevices(ctx context.Context, targetRoom string) ([]sonos.Device, error) {
	discoveryCtx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	return sonos.Discover(discoveryCtx, discoveryTimeout, targetRoom)
}

func enrichDeviceMetadata(ctx context.Context, devices []sonos.Device) ([]sonos.Device, error) {
	if len(devices) == 0 {
		return devices, nil
	}

	enrichmentWindow := time.Duration(len(devices)) * enrichmentPerDevice
	if enrichmentWindow < enrichmentMinimumTotal {
		enrichmentWindow = enrichmentMinimumTotal
	}

	enrichmentCtx, cancel := context.WithTimeout(ctx, enrichmentWindow)
	defer cancel()

	enriched, err := sonos.EnrichDevices(enrichmentCtx, devices)
	if len(enriched) > 0 {
		return enriched, err
	}
	return devices, err
}

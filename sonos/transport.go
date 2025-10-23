package sonos

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

func avTransportControlURL(device Device) (string, error) {
	return avTransportURL(device, "Control")
}

func avTransportEventURL(device Device) (string, error) {
	return avTransportURL(device, "Event")
}

func avTransportURL(device Device, suffix string) (string, error) {
	if strings.TrimSpace(device.Location) == "" {
		return "", errors.New("sonos: device location is empty")
	}

	baseURL, err := url.Parse(device.Location)
	if err != nil {
		return "", fmt.Errorf("sonos: parse device location: %w", err)
	}

	baseURL.Path = ""
	baseURL.RawPath = ""
	baseURL.RawQuery = ""
	baseURL.Fragment = ""

	return strings.TrimRight(baseURL.String(), "/") + "/MediaRenderer/AVTransport/" + suffix, nil
}

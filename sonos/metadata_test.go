package sonos

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const sonosXML = `<?xml version="1.0" encoding="utf-8"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
  <specVersion>
    <major>1</major>
    <minor>0</minor>
  </specVersion>
  <device>
    <deviceType>urn:schemas-upnp-org:device:ZonePlayer:1</deviceType>
    <friendlyName>Kitchen</friendlyName>
    <roomName>Kitchen</roomName>
    <manufacturer>Sonos, Inc.</manufacturer>
    <modelName>Sonos One</modelName>
    <modelNumber>S13</modelNumber>
    <serialNumber>RINCON_12345</serialNumber>
    <softwareVersion>65.1-123456</softwareVersion>
    <deviceList>
      <device>
        <deviceType>urn:schemas-upnp-org:device:MediaServer:1</deviceType>
        <friendlyName>Nested Server</friendlyName>
      </device>
    </deviceList>
  </device>
</root>`

const otherXML = `<?xml version="1.0" encoding="utf-8"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
  <device>
    <deviceType>urn:schemas-upnp-org:device:MediaRenderer:1</deviceType>
    <friendlyName>Generic Speaker</friendlyName>
    <roomName>Living Room</roomName>
    <manufacturer>Acme</manufacturer>
    <modelName>Speaker 2000</modelName>
  </device>
</root>`

const ampXML = `<?xml version="1.0" encoding="utf-8"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
  <specVersion>
    <major>1</major>
    <minor>0</minor>
  </specVersion>
  <device>
    <deviceType>urn:schemas-upnp-org:device:ZonePlayer:1</deviceType>
    <friendlyName>Office Pato Amp</friendlyName>
    <roomName>Office Pato</roomName>
    <manufacturer>Sonos, Inc.</manufacturer>
    <modelName>Sonos Amp</modelName>
    <modelNumber>S16</modelNumber>
    <serialNumber>RINCON_F0F6C19DB2C101400</serialNumber>
    <softwareVersion>91.0-70070</softwareVersion>
    <deviceList>
      <device>
        <deviceType>urn:schemas-upnp-org:device:MediaServer:1</deviceType>
        <friendlyName>Sonos Amp Media Server</friendlyName>
      </device>
      <device>
        <deviceType>urn:schemas-upnp-org:device:MediaRenderer:1</deviceType>
        <friendlyName>Sonos Amp Media Renderer</friendlyName>
      </device>
    </deviceList>
  </device>
</root>`

func TestParseDeviceDescriptionHandlesNamespaces(t *testing.T) {
	meta, err := parseDeviceDescription([]byte(ampXML))
	if err != nil {
		t.Fatalf("parseDeviceDescription returned error: %v", err)
	}

	if meta.FriendlyName != "Office Pato Amp" {
		t.Fatalf("unexpected friendly name: %q", meta.FriendlyName)
	}

	if meta.RoomName != "Office Pato" {
		t.Fatalf("unexpected room name: %q", meta.RoomName)
	}

	if meta.DeviceType != "urn:schemas-upnp-org:device:ZonePlayer:1" {
		t.Fatalf("unexpected device type: %q", meta.DeviceType)
	}
}

func TestEnrichMetadataMarksSonos(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/desc") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(sonosXML))
	}))
	defer server.Close()

	device := Device{Location: server.URL + "/desc.xml"}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	enriched, err := enrichMetadata(ctx, device)
	if err != nil {
		t.Fatalf("enrichMetadata returned error: %v", err)
	}

	if !enriched.IsSonos {
		t.Fatalf("expected IsSonos to be true")
	}

	if enriched.Metadata.FriendlyName != "Kitchen" {
		t.Fatalf("unexpected friendly name: %q", enriched.Metadata.FriendlyName)
	}

	if enriched.Metadata.RoomName != "Kitchen" {
		t.Fatalf("unexpected room name: %q", enriched.Metadata.RoomName)
	}
}

func TestEnrichMetadataSkipsNonSonos(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(otherXML))
	}))
	defer server.Close()

	device := Device{Location: server.URL}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	enriched, err := enrichMetadata(ctx, device)
	if err != nil {
		t.Fatalf("enrichMetadata returned error: %v", err)
	}

	if enriched.IsSonos {
		t.Fatalf("expected IsSonos to be false")
	}

	if enriched.Metadata.FriendlyName != "Generic Speaker" {
		t.Fatalf("unexpected friendly name: %q", enriched.Metadata.FriendlyName)
	}

	if enriched.Metadata.RoomName != "Living Room" {
		t.Fatalf("unexpected room name: %q", enriched.Metadata.RoomName)
	}
}

func TestEnrichDevicesUsesContextDeadline(t *testing.T) {
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(sonosXML))
	}))
	defer slowServer.Close()

	device := Device{Location: slowServer.URL}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if _, err := EnrichDevices(ctx, []Device{device}); err == nil {
		t.Fatalf("expected context deadline error")
	}
}

package sonos

import (
	"net"
	"testing"
	"time"
)

func TestParseResponse(t *testing.T) {
	raw := "HTTP/1.1 200 OK\r\n" +
		"CACHE-CONTROL: max-age=1800\r\n" +
		"EXT:\r\n" +
		"LOCATION: http://192.168.1.23:1400/xml/device_description.xml\r\n" +
		"SERVER: Linux UPnP/1.0 Sonos/58.1-74220 (ZP90)\r\n" +
		"ST: urn:schemas-upnp-org:device:ZonePlayer:1\r\n" +
		"USN: uuid:RINCON_1234567890ABCD00::urn:schemas-upnp-org:device:ZonePlayer:1\r\n" +
		"\r\n"

	device, err := parseResponse([]byte(raw))
	if err != nil {
		t.Fatalf("parseResponse returned error: %v", err)
	}

	if device.Location != "http://192.168.1.23:1400/xml/device_description.xml" {
		t.Fatalf("unexpected location: %q", device.Location)
	}

	if device.Server != "Linux UPnP/1.0 Sonos/58.1-74220 (ZP90)" {
		t.Fatalf("unexpected server: %q", device.Server)
	}

	if device.ST != "urn:schemas-upnp-org:device:ZonePlayer:1" {
		t.Fatalf("unexpected ST: %q", device.ST)
	}

	if device.USN != "uuid:RINCON_1234567890ABCD00::urn:schemas-upnp-org:device:ZonePlayer:1" {
		t.Fatalf("unexpected USN: %q", device.USN)
	}
}

func TestDiscoverRejectsNilContext(t *testing.T) {
	if _, err := Discover(nil, time.Second); err == nil {
		t.Fatal("expected error when passing nil context")
	}
}

func TestSendSearchRequests(t *testing.T) {
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("client listen: %v", err)
	}
	defer client.Close()

	if err := listener.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}

	received := make(chan struct{})
	go func() {
		defer close(received)
		buf := make([]byte, 512)
		if _, _, err := listener.ReadFromUDP(buf); err != nil {
			t.Errorf("read: %v", err)
		}
	}()

	if err := sendSearchRequests(client, listener.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatalf("sendSearchRequests: %v", err)
	}

	select {
	case <-received:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("did not receive UDP packet from sendSearchRequests")
	}
}

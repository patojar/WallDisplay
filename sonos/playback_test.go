package sonos

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNowPlayingTrack(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/MediaRenderer/AVTransport/Control" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		defer r.Body.Close()
		switch {
		case strings.Contains(string(payload), "GetPositionInfo"):
			body := `<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">
  <s:Body>
    <u:GetPositionInfoResponse xmlns:u="urn:schemas-upnp-org:service:AVTransport:1">
      <Track>1</Track>
      <TrackDuration>0:03:30</TrackDuration>
      <TrackMetaData>&lt;DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/" xmlns:r="urn:schemas-rinconnetworks-com:metadata-1-0/"&gt;&lt;item id="1" parentID="0" restricted="true"&gt;&lt;dc:title&gt;My Song&lt;/dc:title&gt;&lt;dc:creator&gt;The Artists&lt;/dc:creator&gt;&lt;upnp:album&gt;My Album&lt;/upnp:album&gt;&lt;r:streamContent&gt;Artist - My Song&lt;/r:streamContent&gt;&lt;/item&gt;&lt;/DIDL-Lite&gt;</TrackMetaData>
      <TrackURI>x-sonos-spotify:spotify%3atrack%3a123</TrackURI>
    </u:GetPositionInfoResponse>
  </s:Body>
</s:Envelope>`
			fmt.Fprint(w, body)
		case strings.Contains(string(payload), "GetTransportInfo"):
			body := `<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">
  <s:Body>
    <u:GetTransportInfoResponse xmlns:u="urn:schemas-upnp-org:service:AVTransport:1">
      <CurrentTransportState>PLAYING</CurrentTransportState>
      <CurrentTransportStatus>OK</CurrentTransportStatus>
      <CurrentSpeed>1</CurrentSpeed>
    </u:GetTransportInfoResponse>
  </s:Body>
</s:Envelope>`
			fmt.Fprint(w, body)
		default:
			t.Fatalf("unexpected SOAP action: %s", string(payload))
		}
	}))
	defer server.Close()

	device := Device{
		Location: server.URL + "/xml/device_description.xml",
	}

	info, err := NowPlaying(context.Background(), device)
	if err != nil {
		t.Fatalf("NowPlaying error: %v", err)
	}

	if got, want := info.Title, "My Song"; got != want {
		t.Fatalf("Title = %q, want %q", got, want)
	}
	if got, want := info.Artist, "The Artists"; got != want {
		t.Fatalf("Artist = %q, want %q", got, want)
	}
	if got, want := info.Album, "My Album"; got != want {
		t.Fatalf("Album = %q, want %q", got, want)
	}
	if got, want := info.URI, "x-sonos-spotify:spotify%3atrack%3a123"; got != want {
		t.Fatalf("URI = %q, want %q", got, want)
	}
	if got := info.StreamInfo; !strings.Contains(got, "Artist") {
		t.Fatalf("StreamInfo = %q, expected to contain 'Artist'", got)
	}
	if got, want := info.State, "PLAYING"; got != want {
		t.Fatalf("State = %q, want %q", got, want)
	}
}

func TestNowPlayingFault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">
  <s:Body>
    <s:Fault>
      <faultcode>s:Client</faultcode>
      <faultstring>UPnPError</faultstring>
      <detail>
        <UPnPError xmlns="urn:schemas-upnp-org:control-1-0">
          <errorCode>401</errorCode>
          <errorDescription>Invalid Action</errorDescription>
        </UPnPError>
      </detail>
    </s:Fault>
  </s:Body>
</s:Envelope>`
		fmt.Fprint(w, body)
	}))
	defer server.Close()

	device := Device{
		Location: server.URL + "/xml/device_description.xml",
	}

	_, err := NowPlaying(context.Background(), device)
	if err == nil {
		t.Fatal("expected NowPlaying to return error")
	}
	if !strings.Contains(err.Error(), "Invalid Action") {
		t.Fatalf("error %q does not contain expected description", err)
	}
}

func TestBuildTrackInfoParsesMetadata(t *testing.T) {
	meta := positionInfoResponse{
		TrackMetaData: `&lt;DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/" xmlns:r="urn:schemas-rinconnetworks-com:metadata-1-0/"&gt;&lt;item&gt;&lt;dc:title&gt;Unit Test Song&lt;/dc:title&gt;&lt;dc:creator&gt;Tester&lt;/dc:creator&gt;&lt;/item&gt;&lt;/DIDL-Lite&gt;`,
	}

	info, err := buildTrackInfo(meta)
	if err != nil {
		t.Fatalf("buildTrackInfo error: %v", err)
	}
	if info.Title != "Unit Test Song" {
		t.Fatalf("Title = %q, want Unit Test Song", info.Title)
	}
	if info.Artist != "Tester" {
		t.Fatalf("Artist = %q, want Tester", info.Artist)
	}
}

func TestBuildTrackInfoRepairsInvalidEntities(t *testing.T) {
	meta := positionInfoResponse{
		TrackMetaData: `&lt;DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/"&gt;&lt;item&gt;&lt;dc:title&gt;Song &amp;amp; Dance&lt;/dc:title&gt;&lt;upnp:album&gt;Club &vli Nights&lt;/upnp:album&gt;&lt;/item&gt;&lt;/DIDL-Lite&gt;`,
	}

	info, err := buildTrackInfo(meta)
	if err != nil {
		t.Fatalf("buildTrackInfo error: %v", err)
	}
	if info.Title != "Song & Dance" {
		t.Fatalf("Title = %q, want Song & Dance", info.Title)
	}
	if info.Album != "Club &vli Nights" {
		t.Fatalf("Album = %q, want Club &vli Nights", info.Album)
	}
}

func TestSanitizeInvalidEntities(t *testing.T) {
	input := "Rock &vibe &amp; Roll &"
	want := "Rock &amp;vibe &amp; Roll &amp;"
	if got := sanitizeInvalidEntities(input); got != want {
		t.Fatalf("sanitizeInvalidEntities = %q, want %q", got, want)
	}
}

package sonos

import (
	"encoding/xml"
	"testing"
)

func TestParseAVTransportEventWithMetadata(t *testing.T) {
	const body = `<?xml version="1.0" encoding="utf-8"?>
<e:propertyset xmlns:e="urn:schemas-upnp-org:event-1-0">
  <e:property>
    <LastChange>&lt;Event xmlns=&quot;urn:schemas-upnp-org:metadata-1-0/AVT/&quot;&gt;&lt;InstanceID val=&quot;0&quot;&gt;&lt;TransportState val=&quot;PLAYING&quot;/&gt;&lt;CurrentTrackMetaData val=&quot;&lt;DIDL-Lite xmlns=&quot;urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/&quot; xmlns:dc=&quot;http://purl.org/dc/elements/1.1/&quot; xmlns:upnp=&quot;urn:schemas-upnp-org:metadata-1-0/upnp/&quot;&gt;&lt;item&gt;&lt;dc:title&gt;Song &amp;amp; Dance&lt;/dc:title&gt;&lt;dc:creator&gt;Artist&lt;/dc:creator&gt;&lt;/item&gt;&lt;/DIDL-Lite&gt;&quot;/&gt;&lt;CurrentTrackURI val=&quot;x-sonosapi-stream:s1234?sid=254&amp;flags=8224&amp;sn=23&quot;/&gt;&lt;/InstanceID&gt;&lt;/Event&gt;</LastChange>
  </e:property>
</e:propertyset>`

	event, err := ParseAVTransportEvent([]byte(body))
	if err != nil {
		var props eventPropertySet
		if err := xml.Unmarshal([]byte(body), &props); err != nil {
			t.Fatalf("ParseAVTransportEvent error and unmarshal body: %v", err)
		}
		if len(props.Properties) == 0 {
			t.Fatalf("ParseAVTransportEvent error and no properties: %v", err)
		}
		raw := string(props.Properties[0].LastChange.Data)
		prepared := prepareLastChangeXML(raw)
		t.Fatalf("ParseAVTransportEvent error: %v\nraw: %q\nprepared: %q", err, raw, prepared)
	}

	if event.TransportState != "PLAYING" {
		t.Fatalf("TransportState = %q, want PLAYING", event.TransportState)
	}
	if event.Track.Title != "Song & Dance" {
		t.Fatalf("Track.Title = %q, want Song & Dance", event.Track.Title)
	}
	if event.Track.Artist != "Artist" {
		t.Fatalf("Track.Artist = %q, want Artist", event.Track.Artist)
	}
}

func TestParseAVTransportEventWithRealPayload(t *testing.T) {
	const body = `<?xml version="1.0" encoding="utf-8"?>
<e:propertyset xmlns:e="urn:schemas-upnp-org:event-1-0">
  <e:property>
    <LastChange>&lt;Event xmlns=&quot;urn:schemas-upnp-org:metadata-1-0/AVT/&quot; xmlns:r=&quot;urn:schemas-rinconnetworks-com:metadata-1-0/&quot;&gt;&lt;InstanceID val=&quot;0&quot;&gt;&lt;TransportState val=&quot;PAUSED_PLAYBACK&quot;/&gt;&lt;CurrentPlayMode val=&quot;NORMAL&quot;/&gt;&lt;CurrentCrossfadeMode val=&quot;0&quot;/&gt;&lt;NumberOfTracks val=&quot;1&quot;/&gt;&lt;CurrentTrack val=&quot;1&quot;/&gt;&lt;CurrentSection val=&quot;0&quot;/&gt;&lt;CurrentTrackURI val=&quot;x-sonos-vli:RINCON_F0F6C19DB2C101400:1,airplay:3e4acedc271c488c9f7a78dc0cb819df&quot;/&gt;&lt;CurrentTrackDuration val=&quot;0:04:59&quot;/&gt;&lt;CurrentTrackMetaData val=&quot;&amp;lt;DIDL-Lite xmlns:dc=&amp;quot;http://purl.org/dc/elements/1.1/&amp;quot; xmlns:upnp=&amp;quot;urn:schemas-upnp-org:metadata-1-0/upnp/&amp;quot; xmlns:r=&amp;quot;urn:schemas-rinconnetworks-com:metadata-1-0/&amp;quot; xmlns=&amp;quot;urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/&amp;quot;&amp;gt;&amp;lt;item id=&amp;quot;-1&amp;quot; parentID=&amp;quot;-1&amp;quot;&amp;gt;&amp;lt;res duration=&amp;quot;0:04:59&amp;quot;&amp;gt;&amp;lt;/res&amp;gt;&amp;lt;upnp:albumArtURI&amp;gt;http://192.168.7.119:1400/getaa?v=0&amp;amp;amp;vli=1&amp;amp;amp;u=2507217148&amp;lt;/upnp:albumArtURI&amp;gt;&amp;lt;upnp:class&amp;gt;object.item.audioItem.musicTrack&amp;lt;/upnp:class&amp;gt;&amp;lt;dc:title&amp;gt;Tigers&amp;lt;/dc:title&amp;gt;&amp;lt;dc:creator&amp;gt;The Submarines&amp;lt;/dc:creator&amp;gt;&amp;lt;upnp:album&amp;gt;Love Notes/Letter Bombs (Deluxe Edition)&amp;lt;/upnp:album&amp;gt;&amp;lt;r:tiid&amp;gt;8894326627379687932&amp;lt;/r:tiid&amp;gt;&amp;lt;/item&amp;gt;&amp;lt;/DIDL-Lite&amp;gt;&quot;/&gt;&lt;r:NextTrackURI val=&quot;&quot;/&gt;&lt;r:NextTrackMetaData val=&quot;&amp;lt;DIDL-Lite xmlns:dc=&amp;quot;http://purl.org/dc/elements/1.1/&amp;quot; xmlns:upnp=&amp;quot;urn:schemas-upnp-org:metadata-1-0/upnp/&amp;quot; xmlns:r=&amp;quot;urn:schemas-rinconnetworks-com:metadata-1-0/&amp;quot; xmlns=&amp;quot;urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/&amp;quot;&amp;gt;&amp;lt;item id=&amp;quot;-1&amp;quot; parentID=&amp;quot;-1&amp;quot;&amp;gt;&amp;lt;res&amp;gt;&amp;lt;/res&amp;gt;&amp;lt;upnp:albumArtURI&amp;gt;&amp;lt;/upnp:albumArtURI&amp;gt;&amp;lt;upnp:class&amp;gt;object.item.audioItem.musicTrack&amp;lt;/upnp:class&amp;gt;&amp;lt;/item&amp;gt;&amp;lt;/DIDL-Lite&amp;gt;&quot;/&gt;&lt;r:EnqueuedTransportURI val=&quot;&quot;/&gt;&lt;r:EnqueuedTransportURIMetaData val=&quot;&amp;lt;DIDL-Lite xmlns:dc=&amp;quot;http://purl.org/dc/elements/1.1/&amp;quot; xmlns:upnp=&amp;quot;urn:schemas-upnp-org:metadata-1-0/upnp/&amp;quot; xmlns:r=&amp;quot;urn:schemas-rinconnetworks-com:metadata-1-0/&amp;quot; xmlns=&amp;quot;urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/&amp;quot;&amp;gt;&amp;lt;item id=&amp;quot;-1&amp;quot; parentID=&amp;quot;-1&amp;quot; restricted=&amp;quot;true&amp;quot;&amp;gt;&amp;lt;dc:title&amp;gt;&amp;lt;/dc:title&amp;gt;&amp;lt;upnp:class&amp;gt;object.item.audioItem.linein.airplay&amp;lt;/upnp:class&amp;gt;&amp;lt;desc id=&amp;quot;cdudn&amp;quot; nameSpace=&amp;quot;urn:schemas-rinconnetworks-com:metadata-1-0/&amp;quot;&amp;gt;&amp;lt;/desc&amp;gt;&amp;lt;upnp:albumArtURI&amp;gt;&amp;lt;/upnp:albumArtURI&amp;gt;&amp;lt;r:contentService name=&amp;quot;Airplay&amp;quot;/&amp;gt;&amp;lt;/item&amp;gt;&amp;lt;/DIDL-Lite&amp;gt;&quot;/&gt;&lt;/InstanceID&gt;&lt;/Event&gt;</LastChange>
  </e:property>
</e:propertyset>`

	event, err := ParseAVTransportEvent([]byte(body))
	if err != nil {
		t.Fatalf("ParseAVTransportEvent error: %v", err)
	}

	if event.TransportState != "PAUSED_PLAYBACK" {
		t.Fatalf("TransportState = %q, want PAUSED_PLAYBACK", event.TransportState)
	}
	if event.Track.Title != "Tigers" {
		t.Fatalf("Track.Title = %q, want Tigers", event.Track.Title)
	}
	if event.Track.Artist != "The Submarines" {
		t.Fatalf("Track.Artist = %q, want The Submarines", event.Track.Artist)
	}
	if event.Track.Album != "Love Notes/Letter Bombs (Deluxe Edition)" {
		t.Fatalf("Track.Album = %q, want Love Notes/Letter Bombs (Deluxe Edition)", event.Track.Album)
	}
	if event.Track.URI != "x-sonos-vli:RINCON_F0F6C19DB2C101400:1,airplay:3e4acedc271c488c9f7a78dc0cb819df" {
		t.Fatalf("Track.URI = %q, want x-sonos-vli:RINCON_F0F6C19DB2C101400:1,airplay:3e4acedc271c488c9f7a78dc0cb819df", event.Track.URI)
	}
	if event.Track.AlbumArtURI != "http://192.168.7.119:1400/getaa?v=0&vli=1&u=2507217148" {
		t.Fatalf("Track.AlbumArtURI = %q, want http://192.168.7.119:1400/getaa?v=0&vli=1&u=2507217148", event.Track.AlbumArtURI)
	}
}

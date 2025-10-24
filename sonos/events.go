package sonos

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"time"
)

// Subscription represents an active Sonos UPnP event subscription.
type Subscription struct {
	ID       string
	Timeout  time.Duration
	EventURL string
}

// AVTransportEvent captures the interesting fields from AVTransport event notifications.
type AVTransportEvent struct {
	TransportState string
	Track          TrackInfo
}

// SubscribeAVTransport registers a callback URL to receive AVTransport NOTIFY events.
func SubscribeAVTransport(ctx context.Context, device Device, callbackURL string, timeout time.Duration) (Subscription, error) {
	eventURL, err := avTransportEventURL(device)
	if err != nil {
		return Subscription{}, err
	}

	if timeout <= 0 {
		timeout = 30 * time.Minute
	}

	req, err := http.NewRequestWithContext(ctx, "SUBSCRIBE", eventURL, nil)
	if err != nil {
		return Subscription{}, fmt.Errorf("sonos: create subscribe request: %w", err)
	}
	req.Header.Set("CALLBACK", fmt.Sprintf("<%s>", callbackURL))
	req.Header.Set("NT", "upnp:event")
	req.Header.Set("TIMEOUT", formatUPnPTimeout(timeout))

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Subscription{}, fmt.Errorf("sonos: subscribe avtransport: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Subscription{}, fmt.Errorf("sonos: subscribe status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	sid := strings.TrimSpace(resp.Header.Get("SID"))
	if sid == "" {
		return Subscription{}, fmt.Errorf("sonos: subscribe missing SID header")
	}

	negotiated := parseUPnPTimeout(resp.Header.Get("TIMEOUT"))
	if negotiated <= 0 {
		negotiated = timeout
	}

	return Subscription{ID: sid, Timeout: negotiated, EventURL: eventURL}, nil
}

// RenewAVTransport refreshes an active AVTransport subscription.
func RenewAVTransport(ctx context.Context, sub Subscription, timeout time.Duration) (time.Duration, error) {
	if timeout <= 0 {
		timeout = sub.Timeout
	}
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}

	req, err := http.NewRequestWithContext(ctx, "SUBSCRIBE", sub.EventURL, nil)
	if err != nil {
		return 0, fmt.Errorf("sonos: create renew request: %w", err)
	}
	req.Header.Set("SID", sub.ID)
	req.Header.Set("TIMEOUT", formatUPnPTimeout(timeout))

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("sonos: renew avtransport: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return 0, fmt.Errorf("sonos: renew status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	return parseUPnPTimeout(resp.Header.Get("TIMEOUT")), nil
}

// UnsubscribeAVTransport cancels an active subscription.
func UnsubscribeAVTransport(ctx context.Context, sub Subscription) error {
	req, err := http.NewRequestWithContext(ctx, "UNSUBSCRIBE", sub.EventURL, nil)
	if err != nil {
		return fmt.Errorf("sonos: create unsubscribe request: %w", err)
	}
	req.Header.Set("SID", sub.ID)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sonos: unsubscribe avtransport: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("sonos: unsubscribe status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	return nil
}

func formatUPnPTimeout(d time.Duration) string {
	if d <= 0 {
		return "Second-0"
	}
	return fmt.Sprintf("Second-%d", int(d.Seconds()))
}

func parseUPnPTimeout(header string) time.Duration {
	value := strings.TrimSpace(strings.ToLower(header))
	if value == "" {
		return 0
	}
	if value == "infinite" {
		return 0
	}
	if strings.HasPrefix(value, "second-") {
		seconds := strings.TrimPrefix(value, "second-")
		if n, err := time.ParseDuration(seconds + "s"); err == nil {
			return n
		}
	}
	return 0
}

// ParseAVTransportEvent extracts state and track information from an AVTransport NOTIFY payload.
func ParseAVTransportEvent(body []byte) (AVTransportEvent, error) {
	var event AVTransportEvent

	var props eventPropertySet
	if err := xml.Unmarshal(body, &props); err != nil {
		return event, fmt.Errorf("sonos: decode avtransport event: %w", err)
	}

	lastChange := ""
	for _, p := range props.Properties {
		raw := string(p.LastChange.Data)
		if strings.TrimSpace(raw) != "" {
			lastChange = raw
			break
		}
	}
	if strings.TrimSpace(lastChange) == "" {
		return event, fmt.Errorf("sonos: event missing LastChange")
	}

	prepared := prepareLastChangeXML(lastChange)
	inner := avTransportLastChange{}
	if err := xml.Unmarshal([]byte(prepared), &inner); err != nil {
		return event, fmt.Errorf("sonos: decode last change: %w", err)
	}

	if len(inner.Instances) == 0 {
		return event, fmt.Errorf("sonos: last change missing InstanceID")
	}

	instance := inner.Instances[0]
	event.TransportState = strings.TrimSpace(instance.TransportState.Value)

	meta := strings.TrimSpace(instance.CurrentTrackMetaData.Value)
	uri := strings.TrimSpace(instance.CurrentTrackURI.Value)

	if strings.EqualFold(meta, "not_implemented") {
		meta = ""
	}

	if meta != "" || uri != "" {
		info, err := buildTrackInfo(positionInfoResponse{TrackMetaData: meta, TrackURI: uri})
		if err == nil {
			event.Track = info
		} else {
			event.Track.URI = uri
		}
	}

	return event, nil
}

func prepareLastChangeXML(raw string) string {
	const (
		placeholderQuot = "__SONOS_ATTR_QUOT__"
		placeholderApos = "__SONOS_ATTR_APOS__"
	)

	temp := strings.ReplaceAll(raw, "&amp;quot;", placeholderQuot)
	temp = strings.ReplaceAll(temp, "&amp;apos;", placeholderApos)

	decoded := html.UnescapeString(temp)

	decoded = strings.ReplaceAll(decoded, placeholderQuot, "&quot;")
	decoded = strings.ReplaceAll(decoded, placeholderApos, "&apos;")

	escaped := escapeAttributeMarkup(decoded)
	return sanitizeInvalidEntities(escaped)
}

func escapeAttributeMarkup(s string) string {
	if s == "" {
		return s
	}

	inAttr := false
	var attrQuote byte
	nestedDepth := 0
	inNestedTag := false
	var nestedAttrQuote byte

	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if !inAttr {
			if ch == '"' || ch == '\'' {
				inAttr = true
				attrQuote = ch
			}
			b.WriteByte(ch)
			continue
		}

		switch ch {
		case '"', '\'':
			if nestedAttrQuote != 0 {
				if ch == nestedAttrQuote {
					nestedAttrQuote = 0
				}
				if ch == '"' {
					b.WriteString("&quot;")
				} else {
					b.WriteString("&apos;")
				}
				continue
			}

			if inNestedTag {
				nestedAttrQuote = ch
				if ch == '"' {
					b.WriteString("&quot;")
				} else {
					b.WriteString("&apos;")
				}
				continue
			}

			if ch == attrQuote && nestedDepth == 0 {
				inAttr = false
				b.WriteByte(ch)
				continue
			}

			if ch == '"' {
				b.WriteString("&quot;")
			} else {
				b.WriteString("&apos;")
			}
		case '&':
			b.WriteString("&amp;")
		case '<':
			if i+1 < len(s) && s[i+1] == '/' {
				if nestedDepth > 0 {
					nestedDepth--
				}
				inNestedTag = true
			} else {
				nestedDepth++
				inNestedTag = true
			}
			b.WriteString("&lt;")
		case '>':
			if inNestedTag {
				inNestedTag = false
			}
			if nestedDepth > 0 && i > 0 && s[i-1] == '/' {
				nestedDepth--
			}
			b.WriteString("&gt;")
		default:
			b.WriteByte(ch)
		}
	}

	return b.String()
}

type eventPropertySet struct {
	Properties []eventProperty `xml:"property"`
}

type eventProperty struct {
	LastChange innerXML `xml:"LastChange"`
}

type innerXML struct {
	Data []byte `xml:",innerxml"`
}

type avTransportLastChange struct {
	Instances []avTransportInstance `xml:"InstanceID"`
}

type avTransportInstance struct {
	TransportState       avTransportValue `xml:"TransportState"`
	CurrentTrackMetaData avTransportValue `xml:"CurrentTrackMetaData"`
	CurrentTrackURI      avTransportValue `xml:"CurrentTrackURI"`
}

type avTransportValue struct {
	Value string `xml:"val,attr"`
}

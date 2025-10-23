package sonos

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TrackInfo represents the primary metadata for the track playing on a Sonos device.
type TrackInfo struct {
	Title      string
	Artist     string
	Album      string
	StreamInfo string
	URI        string
	State      string
}

// NowPlaying queries a Sonos device for the currently playing track metadata.
func NowPlaying(ctx context.Context, device Device) (TrackInfo, error) {
	if ctx == nil {
		return TrackInfo{}, errors.New("sonos: nil context")
	}

	controlURL, err := avTransportControlURL(device)
	if err != nil {
		return TrackInfo{}, err
	}

	payload := buildGetPositionInfoPayload()
	log.Printf("debug: querying now playing at %s", controlURL)
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, controlURL, bytes.NewReader(payload))
	if err != nil {
		return TrackInfo{}, fmt.Errorf("sonos: create now playing request: %w", err)
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPACTION", `"urn:schemas-upnp-org:service:AVTransport:1#GetPositionInfo"`)

	resp, err := client.Do(req)
	if err != nil {
		return TrackInfo{}, fmt.Errorf("sonos: fetch now playing: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return TrackInfo{}, fmt.Errorf("sonos: read now playing body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 256 {
			snippet = snippet[:256]
		}
		return TrackInfo{}, fmt.Errorf("sonos: now playing http status %s: %s", resp.Status, snippet)
	}

	position, err := parsePositionInfoResponse(body)
	if err != nil {
		return TrackInfo{}, err
	}

	info, err := buildTrackInfo(position)
	if err != nil {
		return TrackInfo{}, err
	}
	if state, err := fetchTransportState(ctx, client, controlURL); err != nil {
		log.Printf("debug: transport state fetch failed: %v", err)
	} else {
		info.State = state
	}
	return info, nil
}

func avTransportControlURL(device Device) (string, error) {
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

	return strings.TrimRight(baseURL.String(), "/") + "/MediaRenderer/AVTransport/Control", nil
}

func buildGetPositionInfoPayload() []byte {
	const payload = `<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">
  <s:Body>
    <u:GetPositionInfo xmlns:u="urn:schemas-upnp-org:service:AVTransport:1">
      <InstanceID>0</InstanceID>
      <Channel>Master</Channel>
    </u:GetPositionInfo>
  </s:Body>
</s:Envelope>`
	return []byte(payload)
}

func buildGetTransportInfoPayload() []byte {
	const payload = `<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">
  <s:Body>
    <u:GetTransportInfo xmlns:u="urn:schemas-upnp-org:service:AVTransport:1">
      <InstanceID>0</InstanceID>
    </u:GetTransportInfo>
  </s:Body>
</s:Envelope>`
	return []byte(payload)
}

func fetchTransportState(ctx context.Context, client *http.Client, controlURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, controlURL, bytes.NewReader(buildGetTransportInfoPayload()))
	if err != nil {
		return "", fmt.Errorf("sonos: create transport info request: %w", err)
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPACTION", `"urn:schemas-upnp-org:service:AVTransport:1#GetTransportInfo"`)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("sonos: fetch transport info: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("sonos: read transport info body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 256 {
			snippet = snippet[:256]
		}
		return "", fmt.Errorf("sonos: transport info http status %s: %s", resp.Status, snippet)
	}

	info, err := parseTransportInfoResponse(body)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(info.CurrentTransportState), nil
}

type positionInfoEnvelope struct {
	Body positionInfoBody `xml:"Body"`
}

type positionInfoBody struct {
	Response *positionInfoResponse `xml:"GetPositionInfoResponse"`
	Fault    *soapFault            `xml:"Fault"`
}

type positionInfoResponse struct {
	TrackMetaData string `xml:"TrackMetaData"`
	TrackURI      string `xml:"TrackURI"`
}

type soapFault struct {
	FaultCode   string `xml:"faultcode"`
	FaultString string `xml:"faultstring"`
	Detail      struct {
		UPnPError struct {
			ErrorCode        string `xml:"errorCode"`
			ErrorDescription string `xml:"errorDescription"`
		} `xml:"UPnPError"`
	} `xml:"detail"`
}

type transportInfoEnvelope struct {
	Body transportInfoBody `xml:"Body"`
}

type transportInfoBody struct {
	Response *transportInfoResponse `xml:"GetTransportInfoResponse"`
	Fault    *soapFault             `xml:"Fault"`
}

type transportInfoResponse struct {
	CurrentTransportState  string `xml:"CurrentTransportState"`
	CurrentTransportStatus string `xml:"CurrentTransportStatus"`
	CurrentSpeed           string `xml:"CurrentSpeed"`
}

func parsePositionInfoResponse(body []byte) (positionInfoResponse, error) {
	var envelope positionInfoEnvelope
	if err := xml.Unmarshal(body, &envelope); err != nil {
		return positionInfoResponse{}, fmt.Errorf("sonos: decode position info: %w", err)
	}

	if envelope.Body.Fault != nil {
		fault := envelope.Body.Fault
		desc := fault.FaultString
		if fault.Detail.UPnPError.ErrorDescription != "" {
			desc = fault.Detail.UPnPError.ErrorDescription
		}
		if desc == "" && fault.Detail.UPnPError.ErrorCode != "" {
			desc = "UPnPError " + fault.Detail.UPnPError.ErrorCode
		}
		return positionInfoResponse{}, fmt.Errorf("sonos: avtransport fault %s: %s", fault.FaultCode, desc)
	}

	if envelope.Body.Response == nil {
		return positionInfoResponse{}, errors.New("sonos: empty position info response")
	}

	return *envelope.Body.Response, nil
}

func parseTransportInfoResponse(body []byte) (transportInfoResponse, error) {
	var envelope transportInfoEnvelope
	if err := xml.Unmarshal(body, &envelope); err != nil {
		return transportInfoResponse{}, fmt.Errorf("sonos: decode transport info: %w", err)
	}

	if envelope.Body.Fault != nil {
		fault := envelope.Body.Fault
		desc := fault.FaultString
		if fault.Detail.UPnPError.ErrorDescription != "" {
			desc = fault.Detail.UPnPError.ErrorDescription
		}
		if desc == "" && fault.Detail.UPnPError.ErrorCode != "" {
			desc = "UPnPError " + fault.Detail.UPnPError.ErrorCode
		}
		return transportInfoResponse{}, fmt.Errorf("sonos: avtransport fault %s: %s", fault.FaultCode, desc)
	}

	if envelope.Body.Response == nil {
		return transportInfoResponse{}, errors.New("sonos: empty transport info response")
	}

	return *envelope.Body.Response, nil
}

type didlItem struct {
	Title        string
	Creator      string
	Album        string
	StreamInfo   string
	ProgramTitle string
	RadioShow    string
}

func buildTrackInfo(resp positionInfoResponse) (TrackInfo, error) {
	info := TrackInfo{
		URI: strings.TrimSpace(resp.TrackURI),
	}

	meta := strings.TrimSpace(resp.TrackMetaData)
	if meta == "" {
		return info, nil
	}

	decoded := sanitizeInvalidEntities(html.UnescapeString(meta))
	item, err := parseTrackMetadata(decoded)
	if err != nil {
		return info, fmt.Errorf("sonos: parse track metadata: %w", err)
	}

	info.Title = strings.TrimSpace(item.Title)
	info.Artist = strings.TrimSpace(item.Creator)
	info.Album = strings.TrimSpace(item.Album)
	info.StreamInfo = strings.TrimSpace(item.StreamInfo)

	if info.Title == "" {
		if strings.TrimSpace(item.ProgramTitle) != "" {
			info.Title = strings.TrimSpace(item.ProgramTitle)
		} else if strings.TrimSpace(item.RadioShow) != "" {
			info.Title = strings.TrimSpace(item.RadioShow)
		} else if info.StreamInfo != "" {
			info.Title = info.StreamInfo
		}
	}

	return info, nil
}

func parseTrackMetadata(xmlString string) (didlItem, error) {
	var item didlItem
	decoder := xml.NewDecoder(strings.NewReader(xmlString))
	var stack []xml.StartElement
	capturing := false
	itemDepth := 0

	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return item, err
		}

		switch tok := token.(type) {
		case xml.StartElement:
			stack = append(stack, tok)
			if !capturing && tok.Name.Local == "item" {
				capturing = true
				itemDepth = len(stack)
			}
		case xml.EndElement:
			if capturing && tok.Name.Local == "item" && len(stack) == itemDepth {
				return item, nil
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if !capturing {
				continue
			}
			if len(stack) != itemDepth+1 {
				continue
			}

			value := strings.TrimSpace(string(tok))
			if value == "" {
				continue
			}

			field := stack[len(stack)-1].Name
			switch field.Space {
			case "http://purl.org/dc/elements/1.1/":
				switch field.Local {
				case "title":
					item.Title = value
				case "creator":
					item.Creator = value
				}
			case "urn:schemas-upnp-org:metadata-1-0/upnp/":
				switch field.Local {
				case "album":
					item.Album = value
				case "albumArtURI":
					// Ignored for now.
				}
			case "urn:schemas-rinconnetworks-com:metadata-1-0/":
				switch field.Local {
				case "streamContent":
					item.StreamInfo = value
				case "programTitle":
					item.ProgramTitle = value
				case "radioShow":
					item.RadioShow = value
				}
			default:
				// Some services omit namespaces; fall back on local names.
				switch field.Local {
				case "title":
					if item.Title == "" {
						item.Title = value
					}
				case "creator":
					if item.Creator == "" {
						item.Creator = value
					}
				case "album":
					if item.Album == "" {
						item.Album = value
					}
				}
			}
		}
	}

	return item, nil
}

func sanitizeInvalidEntities(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch != '&' {
			b.WriteByte(ch)
			continue
		}

		if i+1 >= len(s) {
			b.WriteString("&amp;")
			continue
		}

		j := i + 1
		for j < len(s) && s[j] != ';' && s[j] != '&' && !isEntityTerminator(s[j]) {
			j++
		}

		if j < len(s) && s[j] == ';' {
			entity := s[i+1 : j]
			if isValidEntityName(entity) {
				b.WriteString(s[i : j+1])
				i = j
				continue
			}
		}

		b.WriteString("&amp;")
	}

	return b.String()
}

func isEntityTerminator(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', '\r', '<', '>', '"', '\'':
		return true
	default:
		return false
	}
}

func isValidEntityName(name string) bool {
	if name == "" {
		return false
	}
	if name[0] == '#' {
		if len(name) == 1 {
			return false
		}
		if name[1] == 'x' || name[1] == 'X' {
			if len(name) == 2 {
				return false
			}
			for _, c := range name[2:] {
				if !isHexDigit(c) {
					return false
				}
			}
			return true
		}
		for _, c := range name[1:] {
			if c < '0' || c > '9' {
				return false
			}
		}
		return true
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

func isHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

package google

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultPickerBase is the Picker API.
const DefaultPickerBase = "https://photospicker.googleapis.com/v1"

// MaxPhoto is the largest photo read: the limit of aminte for a picture.
const MaxPhoto = 64 << 20

const (
	pageSize = 100
	maxPages = 50 // a session holds 2000 items at most, so 50 pages of 100 is a bound that is never reached
)

// TokenSource gives the access token for a call.
type TokenSource interface {
	AccessToken(ctx context.Context) (string, error)
}

// Picker is a client of the Photos Picker API.
type Picker struct {
	Base   string // DefaultPickerBase, or a fake in a test
	HTTP   *http.Client
	Tokens TokenSource
}

// StatusError is an answer of Google that is not a success.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string { return fmt.Sprintf("Google answered %d: %s", e.Status, e.Body) }

// Session is a picking session. The owner picks the items on PickerURI.
type Session struct {
	ID            string
	PickerURI     string
	MediaItemsSet bool
	PollInterval  time.Duration
}

type sessionJSON struct {
	ID            string `json:"id"`
	PickerURI     string `json:"pickerUri"`
	MediaItemsSet bool   `json:"mediaItemsSet"`
	PollingConfig struct {
		PollInterval string `json:"pollInterval"`
	} `json:"pollingConfig"`
}

func (s sessionJSON) session() Session {
	d, err := time.ParseDuration(s.PollingConfig.PollInterval)
	if err != nil || d < time.Second {
		d = 5 * time.Second
	}
	return Session{ID: s.ID, PickerURI: s.PickerURI, MediaItemsSet: s.MediaItemsSet, PollInterval: d}
}

// Item is a picked item.
type Item struct {
	ID         string
	CreateTime time.Time
	Type       string // PHOTO, VIDEO or TYPE_UNSPECIFIED
	BaseURL    string
	MIME       string
	Filename   string
}

// Day is the local day of the item in the zone.
func (i Item) Day(zone *time.Location) string { return i.CreateTime.In(zone).Format("2006-01-02") }

// CreateSession starts a session for up to 2000 items.
func (p *Picker) CreateSession(ctx context.Context) (Session, error) {
	var out sessionJSON
	err := p.call(ctx, http.MethodPost, "/sessions", nil, strings.NewReader(`{"pickingConfig":{"maxItemCount":"2000"}}`), &out)
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	return out.session(), nil
}

// GetSession reads a session. MediaItemsSet is true when the owner has picked.
func (p *Picker) GetSession(ctx context.Context, id string) (Session, error) {
	var out sessionJSON
	if err := p.call(ctx, http.MethodGet, "/sessions/"+url.PathEscape(id), nil, nil, &out); err != nil {
		return Session{}, fmt.Errorf("get session: %w", err)
	}
	return out.session(), nil
}

// DeleteSession ends a session.
func (p *Picker) DeleteSession(ctx context.Context, id string) error {
	if err := p.call(ctx, http.MethodDelete, "/sessions/"+url.PathEscape(id), nil, nil, nil); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// ListItems reads every page of the picked items. It keeps photos only: a video, or an item with no type, is left
// out, as is an item with no usable time or address.
func (p *Picker) ListItems(ctx context.Context, sessionID string) ([]Item, error) {
	var items []Item
	token := ""
	for range maxPages {
		q := url.Values{"sessionId": {sessionID}, "pageSize": {fmt.Sprint(pageSize)}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var out struct {
			MediaItems []struct {
				ID         string `json:"id"`
				CreateTime string `json:"createTime"`
				Type       string `json:"type"`
				MediaFile  struct {
					BaseURL  string `json:"baseUrl"`
					MIMEType string `json:"mimeType"`
					Filename string `json:"filename"`
				} `json:"mediaFile"`
			} `json:"mediaItems"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := p.call(ctx, http.MethodGet, "/mediaItems", q, nil, &out); err != nil {
			return nil, fmt.Errorf("list items: %w", err)
		}
		for _, m := range out.MediaItems {
			t, err := time.Parse(time.RFC3339, m.CreateTime)
			if m.Type != "PHOTO" || err != nil || m.ID == "" || m.MediaFile.BaseURL == "" {
				continue
			}
			items = append(items, Item{ID: m.ID, CreateTime: t, Type: m.Type, BaseURL: m.MediaFile.BaseURL,
				MIME: m.MediaFile.MIMEType, Filename: m.MediaFile.Filename})
		}
		if out.NextPageToken == "" {
			return items, nil
		}
		token = out.NextPageToken
	}
	return nil, fmt.Errorf("list items: more than %d pages", maxPages)
}

// Fetch downloads baseURL+suffix ("=d", "=w1280-h1280", "=w256-h256-c") with the access token. It gives the bytes and
// the media type of the answer. It fails when the body is larger than MaxPhoto.
func (p *Picker) Fetch(ctx context.Context, baseURL, suffix string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+suffix, nil)
	if err != nil {
		return nil, "", fmt.Errorf("fetch: %w", err)
	}
	if err := p.authorize(ctx, req); err != nil {
		return nil, "", err
	}
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", &StatusError{Status: resp.StatusCode, Body: snippet(resp.Body)}
	}
	b, err := readLimited(resp.Body, MaxPhoto)
	if err != nil {
		return nil, "", fmt.Errorf("fetch: %w", err)
	}
	typ, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	return b, typ, nil
}

func (p *Picker) authorize(ctx context.Context, req *http.Request) error {
	tok, err := p.Tokens.AccessToken(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	return nil
}

// call sends one JSON request and decodes the JSON answer into out, when out is not nil.
func (p *Picker) call(ctx context.Context, method, path string, q url.Values, body io.Reader, out any) error {
	u := p.Base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if err := p.authorize(ctx, req); err != nil {
		return err
	}
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &StatusError{Status: resp.StatusCode, Body: snippet(resp.Body)}
	}
	b, err := readLimited(resp.Body, maxJSON)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(b, out)
}

// snippet reads the start of an error body, for a message.
func snippet(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, 300))
	return strings.TrimSpace(string(b))
}

// Package aminte is the client of the extension. It is a thin layer over the shared client of the root module
// (aminte/client): it reads Siren entities and writes through the catalog's add-file.
package aminte

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"aminte/client"
	"aminte/siren"
)

// Source is the provenance of every write of the extension.
const Source = siren.ImportPrefix + "google-photos"

const maxTitles = 5 // titles tried for one day: "<day>.jpg", "<day> 2.jpg" ... "<day> 5.jpg"

// Client talks to one aminte server.
type Client struct {
	c *client.Client

	mu      sync.Mutex
	addFile *siren.Action // the add-file action, read from the catalog once; a failed read is not kept
}

// New makes the client of the extension over c, the client of the server. c writes as Source: app.API(command, url, Source) makes
// it from the address and the token of the environment.
func New(c *client.Client) *Client {
	return &Client{c: c}
}

// MonthsWithoutCover gives, for each month (YYYY-MM), the days that show no picture. A day with no page is one.
func (c *Client) MonthsWithoutCover(ctx context.Context, months []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, m := range months {
		t, ok := siren.MonthOf(m)
		if !ok {
			return nil, fmt.Errorf("read month %s: not YYYY-MM", m)
		}
		e, err := c.c.Get(ctx, siren.MonthHref(t))
		if err != nil {
			return nil, fmt.Errorf("read month %s: %w", m, err)
		}
		props, _ := e.Properties.(map[string]any)
		days, _ := props["days"].([]any)
		for _, d := range days {
			day, _ := d.(map[string]any)
			if picture, _ := day["picture"].(string); picture == "" {
				name, _ := day["day"].(string)
				out[name] = true
			}
		}
	}
	return out, nil
}

// DayHasCover reads the day now: has its page a cover?
func (c *Client) DayHasCover(ctx context.Context, day string) (bool, error) {
	e, err := c.c.Get(ctx, siren.DayHref(day))
	if err != nil {
		return false, fmt.Errorf("read day %s: %w", day, err)
	}
	props, _ := e.Properties.(map[string]any)
	return props["cover"] != nil, nil
}

// addFileAction gives the add-file action of the catalog. It reads the catalog on the first call only.
func (c *Client) addFileAction(ctx context.Context) (*siren.Action, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.addFile != nil {
		return c.addFile, nil
	}
	catalog, err := c.c.Catalog(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the catalog: %w", err)
	}
	for i := range catalog {
		if catalog[i].Name == "add-file" {
			c.addFile = &catalog[i]
			return c.addFile, nil
		}
	}
	return nil, errors.New("the catalog has no add-file action")
}

// AddCover keeps the original with the day (add-file). The title is "<day>.<ext>". After a 409 "exists" (another file
// holds the title) it tries "<day> 2.<ext>" and so on, up to a small bound. preview may be nil. It gives the title kept.
// The same original sent again writes nothing: aminte keeps one file for one hash.
func (c *Client) AddCover(ctx context.Context, day, mimeType string, original, preview []byte) (string, error) {
	ext := Ext(mimeType)
	addFile, err := c.addFileAction(ctx)
	if err != nil {
		return "", err
	}
	for n := 1; n <= maxTitles; n++ {
		title := day + "." + ext
		if n > 1 {
			title = fmt.Sprintf("%s %d.%s", day, n, ext)
		}
		files := map[string]client.File{"original": client.Bytes(title, original)}
		if preview != nil {
			files["preview"] = client.Bytes("preview.jpg", preview)
		}
		_, err := c.c.DoFiles(ctx, *addFile, map[string]string{"day": day, "title": title, "mime": mimeType}, files)
		var ce *client.Error
		switch {
		case err == nil:
			return title, nil
		case errors.As(err, &ce) && ce.Status == http.StatusConflict && ce.Code() == siren.CodeExists:
			continue
		default:
			return "", fmt.Errorf("add file %q: %w", title, err)
		}
	}
	return "", fmt.Errorf("add file for %s: the titles %s.%s to %s %d.%s are all in use", day, day, ext, day, maxTitles, ext)
}

// Ext is the file extension for a media type.
func Ext(mimeType string) string {
	switch strings.ToLower(mimeType) {
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	case "image/heic":
		return "heic"
	case "image/heif":
		return "heif"
	case "image/webp":
		return "webp"
	case "image/avif":
		return "avif"
	case "image/tiff":
		return "tiff"
	case "image/bmp":
		return "bmp"
	}
	return "jpg"
}

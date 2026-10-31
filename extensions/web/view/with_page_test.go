package view

import (
	"net/url"
	"strings"
	"testing"

	"lifelog/siren"
)

// TestAPageUnderWithIsDrawnByGeneric: a connected app names its own classes. A class that is also a class of this
// server (day) must not choose the template of the server.
func TestAPageUnderWithIsDrawnByGeneric(t *testing.T) {
	e := &siren.Entity{Class: []string{"day"}, Title: "Their day", Properties: map[string]any{"count": 3}}
	for path, wantGeneric := range map[string]bool{"/with/app/": true, "/with/app/x/1": true, "/days/2026-09-01": false} {
		b, err := Draw(e, Facts{URL: &url.URL{Path: path}})
		generic := err == nil && strings.Contains(string(b), `<p class="meta-line">count: 3</p>`) // the scalar properties of the generic view
		if wantGeneric && !generic {
			t.Errorf("%s: not drawn by generic (err %v)", path, err)
		}
		if !wantGeneric && generic {
			t.Errorf("%s: drawn by generic, want the template of the class day", path)
		}
	}
}

// TestTheRailMarksTheAppOfThePage: the link of an app is current on its home and on every page under it.
func TestTheRailMarksTheAppOfThePage(t *testing.T) {
	shell := &Shell{Apps: []AppLink{{Name: "a", Title: "App A"}, {Name: "ab", Title: "App AB"}}}
	for path, want := range map[string]string{"/with/a": "App A", "/with/a/items/1": "App A", "/with/ab/": "App AB", "/browse": "Browse"} {
		var current []string
		for _, it := range (&view{Path: path, Shell: shell}).Nav() {
			if it.Current {
				current = append(current, it.Title)
			}
		}
		if len(current) != 1 || current[0] != want {
			t.Errorf("%s: current %v, want only %s", path, current, want)
		}
	}
}

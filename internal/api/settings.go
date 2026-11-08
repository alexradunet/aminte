package api

// The settings of the application: the owner's file beside life.db (README: the settings file).

import (
	"net/http"
	"strings"

	"aminte/internal/core"
	"aminte/siren"
)

// settingsEntity is the settings resource. It holds the values, the key too: only the owner reads it
// (GET /settings), while the catalog (GET /actions) offers the action with no value in it.
func settingsEntity(s core.Settings) *siren.Entity {
	props := map[string]any{"model_url": s.ModelURL, "model": s.Model}
	if s.OwnerNumber != "" {
		props["owner_number"] = s.OwnerNumber
	}
	if s.ModelKey != "" {
		props["model_key"] = s.ModelKey
	}
	return &siren.Entity{Class: []string{"settings"}, Title: "Settings", Properties: props,
		// The backup of life.db is a snapshot (one way to do a thing): the link opens its form, and the answer of the
		// snapshot offers the file for download.
		Links: []siren.Link{link("self", "/settings", "Settings"), link("index", "/", "Home"),
			link("snapshot", "/actions?new=snapshot", "Take a snapshot")},
		Actions: []siren.Action{action("settings", nil,
			map[string]any{"model_url": s.ModelURL, "model": s.Model, "model_key": s.ModelKey, "owner_number": s.OwnerNumber})}}
}

// settings is GET /settings: the owner's alone.
func (h *server) settings(r *http.Request) (*siren.Entity, error) {
	if err := requireOwner(source(r)); err != nil {
		return nil, err
	}
	s, err := h.s.ReadSettings()
	if err != nil {
		return nil, err
	}
	return settingsEntity(s), nil
}

// saveSettings is the action settings. A field that the request leaves out stays as it is, and an empty one clears
// that setting (a form sends all of them).
func (h *server) saveSettings(r *http.Request, a admitted) (*siren.Entity, error) {
	s, err := h.s.ReadSettings()
	if err != nil {
		return nil, err
	}
	for name, to := range map[string]*string{"model_url": &s.ModelURL, "model": &s.Model, "model_key": &s.ModelKey, "owner_number": &s.OwnerNumber} {
		if a.v.Has(name) {
			*to = strings.TrimSpace(a.v.Get(name))
		}
	}
	if s.OwnerNumber != "" && strings.Trim(s.OwnerNumber, "0123456789") != "" {
		return nil, core.Invalid("owner_number: digits with the country code, such as 40712345678")
	}
	if err := h.s.WriteSettings(s); err != nil {
		return nil, err
	}
	e := settingsEntity(s)
	e.Result = "Settings saved."
	return e, nil
}

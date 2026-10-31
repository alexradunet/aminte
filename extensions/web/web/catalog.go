package web

import (
	"errors"
	"log"
	"maps"
	"net/http"
	"slices"
	"strings"

	"lifelog/app"
	"lifelog/siren"
)

// The catalog of this program (extensions/README.md, "A catalog of its own"). `lifelog serve --with web` gives the
// program an address in LIFELOG_LISTEN, and the core merges this catalog into its own as /with/web: the action
// settings is web-settings there. The core holds the owner rule (owner: true), so an agent or an import never sets the
// PIN, and the program does not check the writer.

// settingsAction is the one action of the catalog. GET /actions offers it with no value, as the core offers its own
// settings; the home of the app and the page /settings fill the values.
var settingsAction = siren.Action{
	Name: "settings", Class: []string{"edit"}, Owner: true, Title: "Settings of this browser", Method: "POST", Href: "/settings",
	Description: "The floor under the pages of this browser and its lock. ground is boards, oak, firelight or embers. " +
		"lock_after is the idle minutes after which the browser locks: off, 5, 10, 15, 30 or 60. " +
		"pin is the 4 to 8 digits that open a locked browser. " +
		"A field that is left out stays as it is. An empty field clears that setting, but an empty pin keeps the stored PIN. " +
		"lock_after other than off with no stored PIN and no new one is refused. " +
		"The settings are in a file of lifelog-web. The file holds a salted hash of the PIN and never the PIN.",
	Fields: []siren.Field{
		{Name: "ground", Type: "text", Title: "Ground", Options: grounds},
		{Name: "lock_after", Type: "text", Title: "Lock after idle minutes", Options: lockAfters},
		{Name: "pin", Type: "password", Title: "Lock PIN, 4 to 8 digits; empty keeps the PIN now stored"},
	},
}

// maxSettingsBody is the largest body of the settings action. Its three fields are short.
const maxSettingsBody = 4096

// Catalog is the handler of the catalog of this program. Serve it on LIFELOG_LISTEN with lifelog/app.
func (b *Browser) Catalog() http.Handler {
	return app.Catalog("This browser", b.home, []siren.Action{settingsAction},
		map[string]func(*http.Request) (*siren.Entity, error){"settings": b.saveSettings})
}

// settingsEntity is the settings of this browser as an entity. It holds no PIN and no hash: only whether a PIN is set
// would be a hint to a reader, and the lock_after value says the same.
func settingsEntity(s browserSettings) *siren.Entity {
	return &siren.Entity{
		Class: []string{"browser"}, Title: "This browser",
		Properties: map[string]any{"ground": groundOf(s), "lock_after": lockAfterOf(s)},
		// The self link is the home of the app. A save answers with it too, so the core sends the browser to the page that
		// it came from (back), or here.
		Links:   []siren.Link{{Rel: []string{"self"}, Href: "/", Title: "This browser"}},
		Actions: []siren.Action{withValues(settingsAction, s)},
	}
}

// withValues is the action a with the current settings as the values of its fields. The PIN is never shown, so its
// field stays empty. The fields are a copy.
func withValues(a siren.Action, s browserSettings) siren.Action {
	a.Fields = slices.Clone(a.Fields)
	for i, f := range a.Fields {
		switch f.Name {
		case "ground":
			a.Fields[i].Value = groundOf(s)
		case "lock_after":
			a.Fields[i].Value = lockAfterOf(s)
		}
	}
	return a
}

func (b *Browser) home(*http.Request) (*siren.Entity, error) {
	return settingsEntity(b.settings()), nil
}

// refused is the 422 of a request that the action does not take.
func refused(msg string) *app.Error {
	return &app.Error{Status: http.StatusUnprocessableEntity, Code: siren.CodeInvalid, Msg: msg}
}

// saveSettings is the action settings. The core has checked the request against the catalog already, but this program
// is a server and checks too: a field the action does not list, and a value outside the options of a field, are refused,
// and nothing is written. A field that the request leaves out stays as it is, and an empty one clears that setting.
// The PIN is the one field whose empty value keeps the setting, because no read ever shows it.
func (b *Browser) saveSettings(r *http.Request) (*siren.Entity, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, maxSettingsBody)
	if err := r.ParseForm(); err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			return nil, &app.Error{Status: http.StatusRequestEntityTooLarge, Code: siren.CodeTooLarge, Msg: "request body is larger than 4 KiB"}
		}
		return nil, &app.Error{Status: http.StatusBadRequest, Code: siren.CodeBadRequest, Msg: "body: " + err.Error()}
	}
	for _, name := range slices.Sorted(maps.Keys(r.Form)) {
		if settingsAction.Field(name) == nil {
			return nil, refused("unknown field " + name + ": settings takes ground, lock_after, pin")
		}
	}
	for _, f := range settingsAction.Fields {
		if v := strings.TrimSpace(r.Form.Get(f.Name)); v != "" && len(f.Options) > 0 && !slices.Contains(f.Options, v) {
			return nil, refused(f.Name + " must be one of " + strings.Join(f.Options, ", "))
		}
	}
	var hash string // made before the lock of the store: it takes about 100 ms, and every page waits for that lock
	if pin := strings.TrimSpace(r.Form.Get("pin")); pin != "" {
		if !pinOK(pin) {
			return nil, refused("pin: 4 to 8 digits")
		}
		var err error
		if hash, err = hashPIN(pin); err != nil {
			log.Printf("lifelog-web: settings: %v", err)
			return nil, err
		}
	}
	saved, err := b.store.update(func(s *browserSettings) error {
		if r.Form.Has("ground") {
			s.Ground = strings.TrimSpace(r.Form.Get("ground"))
		}
		if r.Form.Has("lock_after") {
			s.LockAfter = strings.TrimSpace(r.Form.Get("lock_after"))
		}
		if s.LockAfter == "off" {
			s.LockAfter = ""
		}
		if hash != "" {
			s.LockPINHash = hash
		}
		if s.LockAfter != "" && s.LockPINHash == "" {
			return refused("lock_after: set pin as well, a PIN of 4 to 8 digits")
		}
		return nil
	})
	if err != nil {
		var refusal *app.Error
		if !errors.As(err, &refusal) {
			log.Printf("lifelog-web: settings: %v", err) // the cause stays in the log: the client gets "internal error"
		}
		return nil, err
	}
	e := settingsEntity(saved)
	e.Result = "Settings saved."
	return e, nil
}

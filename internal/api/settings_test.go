package api_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"lifelog/client"
	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
	"lifelog/internal/core"
	"lifelog/internal/db/dbtest"
)

// TestSettingsAreTheOwnersFile: the owner saves the model and reads it back; the file is made with the mode 0600
// through a temporary file (none is left); a field left out stays and an empty one clears; GET /actions fills no value,
// whatever the file holds.
func TestSettingsAreTheOwnersFile(t *testing.T) {
	c, _, s := apitest.Fresh(t, "cli")
	read := func() map[string]any {
		t.Helper()
		p, _ := apitest.Must(t)(c.Get(t.Context(), "/settings")).Properties.(map[string]any)
		return p
	}
	if p := read(); p["model_url"] != "" || p["model"] != "" || p["model_key"] != nil {
		t.Errorf("the settings with no file: %v, want empty and no key", p)
	}
	catalog := apitest.Must(t)(c.Get(t.Context(), "/actions"))
	settings := apitest.Action(t, catalog, "settings")
	if !settings.Owner || settings.Method != "POST" || settings.Href != "/settings" || settings.Field("model_key").Type != "password" {
		t.Fatalf("the action settings: %+v", settings)
	}
	e, err := c.Do(t.Context(), settings, map[string]string{"model_url": " http://127.0.0.1:1234/v1 ", "model": "synthetic-model", "model_key": "synthetic-key"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Result != "Settings saved." {
		t.Errorf("the result: %v", e.Result)
	}
	if want := filepath.Join(filepath.Dir(s.DB.Path()), strings.TrimSuffix(filepath.Base(s.DB.Path()), ".db")+".settings.json"); s.SettingsPath != want {
		t.Errorf("the settings path %q, want %q", s.SettingsPath, want)
	}
	if fi, err := os.Stat(s.SettingsPath); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("the mode %v, want 0600", fi.Mode().Perm())
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(s.SettingsPath), ".settings-*")); len(left) != 0 {
		t.Errorf("temporary files are left: %v", left)
	}
	if p := read(); p["model_url"] != "http://127.0.0.1:1234/v1" || p["model"] != "synthetic-model" || p["model_key"] != "synthetic-key" {
		t.Errorf("GET /settings: %v", p)
	}
	// The owner's form carries the values, the key too; the catalog carries none.
	form := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/settings")), "settings")
	if form.Field("model_key").Value != "synthetic-key" || form.Field("model").Value != "synthetic-model" {
		t.Errorf("the form of GET /settings: %+v", form.Fields)
	}
	for _, a := range apitest.Must(t)(c.Get(t.Context(), "/actions")).Actions {
		for _, f := range a.Fields {
			if a.Name == "settings" && (f.Value != nil && f.Value != "") {
				t.Errorf("GET /actions fills %s with %v", f.Name, f.Value)
			}
		}
	}
	if _, err := c.Do(t.Context(), settings, map[string]string{"model": "other"}); err != nil {
		t.Fatal(err)
	}
	if p := read(); p["model"] != "other" || p["model_key"] != "synthetic-key" || p["model_url"] != "http://127.0.0.1:1234/v1" {
		t.Errorf("after a save of one field: %v", p)
	}
	if _, err := c.Do(t.Context(), settings, map[string]string{"model_key": ""}); err != nil {
		t.Fatal(err)
	}
	if p := read(); p["model_key"] != nil || p["model"] != "other" {
		t.Errorf("after an empty key: %v", p)
	}
	b, err := os.ReadFile(s.SettingsPath)
	if err != nil || strings.Contains(string(b), "model_key") {
		t.Errorf("the file after the key was cleared: %q %v", b, err)
	}
}

// TestTheSettingsOfTheBrowserAreNotTheCores: the ground, the lock and the PIN are the settings of the browser
// extension. The action takes four fields. A request that names one of the others is 422 and writes nothing, and the
// settings resource shows none of them. A file of an older version that holds them still reads, and the next save
// drops them.
func TestTheSettingsOfTheBrowserAreNotTheCores(t *testing.T) {
	c, _, s := apitest.Fresh(t, "cli")
	act := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/actions")), "settings")
	var fields []string
	for _, f := range act.Fields {
		fields = append(fields, f.Name)
	}
	if got := strings.Join(fields, ","); got != "model_url,model,model_key,owner_number" {
		t.Errorf("the fields of settings: %s", got)
	}
	for _, name := range []string{"ground", "lock_after", "lock_pin"} {
		_, err := c.Do(t.Context(), act, map[string]string{"model": "x", name: "1234"})
		if apitest.Status(err) != 422 || !strings.Contains(err.Error(), "unknown field "+name) {
			t.Errorf("a request with %s: %v, want 422 that names the field", name, err)
		}
	}
	if _, err := os.Stat(s.SettingsPath); !os.IsNotExist(err) {
		t.Errorf("a refused save wrote the file: %v", err)
	}
	old := `{"model_url": "http://127.0.0.1:1/v1", "ground": "embers", "lock_after": "15", "lock_pin_hash": "pbkdf2-sha256$1$AA$AA"}`
	if err := os.WriteFile(s.SettingsPath, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	read := apitest.Must(t)(c.Get(t.Context(), "/settings"))
	props, _ := read.Properties.(map[string]any)
	if props["model_url"] != "http://127.0.0.1:1/v1" || props["ground"] != nil || props["lock_after"] != nil || props["lock_pin_hash"] != nil {
		t.Errorf("the settings of an older file: %v", props)
	}
	if _, err := c.Do(t.Context(), act, map[string]string{"model": "synthetic-model"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(s.SettingsPath)
	if err != nil || strings.Contains(string(b), "ground") || strings.Contains(string(b), "lock_") || !strings.Contains(string(b), "synthetic-model") {
		t.Errorf("the file after a save: %q %v, want the model and none of the settings of the browser", b, err)
	}
}

// TestTheOwnerNumberIsDigits: digits with the country code are kept and read back; letters, a plus sign and spaces are
// 422 with the sentence, and a refused save leaves the setting as it was; an empty value clears it.
func TestTheOwnerNumberIsDigits(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	act := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/actions")), "settings")
	if f := act.Field("owner_number"); f.Type != "text" || !strings.Contains(f.Title, "digits with the country code") {
		t.Errorf("the field owner_number: %+v", f)
	}
	read := func() any {
		t.Helper()
		p, _ := apitest.Must(t)(c.Get(t.Context(), "/settings")).Properties.(map[string]any)
		return p["owner_number"]
	}
	if got := read(); got != nil {
		t.Errorf("owner_number with none set: %v, want absent", got)
	}
	if _, err := c.Do(t.Context(), act, map[string]string{"owner_number": " 40712345678 "}); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != "40712345678" {
		t.Errorf("owner_number after a save: %v", got)
	}
	for _, bad := range []string{"abc", "+40712345678", "0712 345 678", "40712345678x"} {
		_, err := c.Do(t.Context(), act, map[string]string{"owner_number": bad})
		if apitest.Status(err) != 422 || !strings.Contains(err.Error(), "owner_number: digits with the country code, such as 40712345678") {
			t.Errorf("owner_number %q: %v, want 422 with the sentence", bad, err)
		}
	}
	if got := read(); got != "40712345678" {
		t.Errorf("owner_number after refused saves: %v, want it unchanged", got)
	}
	if _, err := c.Do(t.Context(), act, map[string]string{"owner_number": ""}); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != nil {
		t.Errorf("owner_number after an empty save: %v, want absent", got)
	}
}

// TestSettingsAreRefusedToAnAgentAndAnImport: both routes answer 403 forbidden, and a refused save writes nothing.
func TestSettingsAreRefusedToAnAgentAndAnImport(t *testing.T) {
	_, h, s := apitest.Fresh(t, "cli")
	catalog := apitest.Must(t)(client.InProcess(h, "cli").Get(t.Context(), "/actions"))
	for _, source := range []string{"agent:x", "import:x"} {
		c := client.InProcess(h, source)
		if _, err := c.Get(t.Context(), "/settings"); apitest.Status(err) != 403 {
			t.Errorf("%s GET /settings: %v, want 403", source, err)
		}
		if _, err := c.Do(t.Context(), apitest.Action(t, catalog, "settings"), map[string]string{"model": "x"}); apitest.Status(err) != 403 {
			t.Errorf("%s POST /settings: %v, want 403", source, err)
		}
	}
	if _, err := os.Stat(s.SettingsPath); !os.IsNotExist(err) {
		t.Errorf("a refused save wrote the file: %v", err)
	}
	// The catalog is readable by an agent, and carries no value.
	for _, a := range apitest.Must(t)(client.InProcess(h, "agent:x").Get(t.Context(), "/actions")).Actions {
		if a.Name == "settings" && a.Field("model_key").Value != nil {
			t.Errorf("an agent reads the key in the catalog: %v", a.Field("model_key").Value)
		}
	}
}

// TestAStoreWithNoSettingsPathRefusesBoth: a read and a write say that the store has no settings file.
func TestAStoreWithNoSettingsPathRefusesBoth(t *testing.T) {
	h := api.New(&core.Store{DB: dbtest.New(t)})
	c := client.InProcess(h, "cli")
	_, err := c.Get(t.Context(), "/settings")
	if apitest.Status(err) != 500 || !strings.Contains(err.Error(), "no settings file") {
		t.Errorf("GET /settings: %v, want an error that says there is no settings file", err)
	}
	catalog := apitest.Must(t)(c.Get(t.Context(), "/actions"))
	_, err = c.Do(t.Context(), apitest.Action(t, catalog, "settings"), map[string]string{"model": "x"})
	if apitest.Status(err) != 500 || !strings.Contains(err.Error(), "no settings file") {
		t.Errorf("POST /settings: %v, want an error that says there is no settings file", err)
	}
}

// TestSettingsPathFor: the path of the database with .db cut and .settings.json added.
func TestSettingsPathFor(t *testing.T) {
	for in, want := range map[string]string{"/a/life.db": "/a/life.settings.json", "/a/other": "/a/other.settings.json", "life.db": "life.settings.json"} {
		if got := core.SettingsPathFor(in); got != want {
			t.Errorf("SettingsPathFor(%q) = %q, want %q", in, got, want)
		}
	}
}

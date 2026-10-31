package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/db/dbtest"
)

// TestSettingsOnTheCommandLine: `lifelog do settings` writes the file beside the database (NAME.db gives
// NAME.settings.json) and `lifelog get /settings` reads it back.
func TestSettingsOnTheCommandLine(t *testing.T) {
	live := dbtest.File(t)
	e, stdout, _ := testEnv(t)
	if err := runArgs(t.Context(), e, []string{"do", "settings", "model_url=http://127.0.0.1:1234/v1", "model=synthetic-model", "--db", live}); err != nil {
		t.Fatal(err)
	}
	file := strings.TrimSuffix(live, ".db") + ".settings.json"
	if filepath.Dir(file) != filepath.Dir(live) {
		t.Fatalf("the file %s is not beside %s", file, live)
	}
	b, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(b), "synthetic-model") {
		t.Fatalf("the settings file: %q %v", b, err)
	}
	stdout.Reset()
	if err := runArgs(t.Context(), e, []string{"get", "/settings", "--db", live}); err != nil {
		t.Fatal(err)
	}
	if out := stdout.String(); !strings.Contains(out, "http://127.0.0.1:1234/v1") || !strings.Contains(out, "synthetic-model") {
		t.Errorf("lifelog get /settings: %s", out)
	}
}

// TestTheSettingsOfTheBrowserAreNotOnTheCommandLine: `lifelog do settings ground=embers` is refused with the field
// named, and so are the lock settings. They are the settings of the browser extension (lifelog-web), which has its own
// action. The refused line writes no file.
func TestTheSettingsOfTheBrowserAreNotOnTheCommandLine(t *testing.T) {
	live := dbtest.File(t)
	e, _, _ := testEnv(t)
	for _, field := range []string{"ground=embers", "lock_after=15", "lock_pin=1234"} {
		name, _, _ := strings.Cut(field, "=")
		err := runArgs(t.Context(), e, []string{"do", "settings", field, "--db", live})
		if err == nil || !strings.Contains(err.Error(), "unknown field "+name) {
			t.Errorf("%s: %v, want a refusal that names the field", field, err)
		}
	}
	if _, err := os.Stat(strings.TrimSuffix(live, ".db") + ".settings.json"); !os.IsNotExist(err) {
		t.Errorf("a refused line wrote the settings file: %v", err)
	}
}

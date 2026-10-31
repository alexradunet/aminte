package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Settings are the settings of the application: a file beside life.db, not a part of the database. The key is a
// secret and a snapshot copies the database, so the settings stay out of it. Each setting is added when a feature
// needs it (README: the settings file).
type Settings struct {
	ModelURL string `json:"model_url,omitempty"` // the base URL of a server that speaks the chat-completions protocol, ending in /v1
	Model    string `json:"model,omitempty"`     // the model id
	ModelKey string `json:"model_key,omitempty"` // a bearer credential, when the server needs one
	// OwnerNumber is the digits of the owner's phone number with the country code: the one number that the agent on
	// the phone answers. The action settings refuses a value that is not digits.
	OwnerNumber string `json:"owner_number,omitempty"`
}

// SettingsPathFor is the settings file of the database at dbPath: the path with .db cut and .settings.json added
// (life.db gives life.settings.json). A path that does not end in .db gets the suffix added whole.
func SettingsPathFor(dbPath string) string {
	return strings.TrimSuffix(dbPath, ".db") + ".settings.json"
}

var errNoSettingsPath = &Error{Status: http.StatusInternalServerError, Msg: "this store has no settings file: SettingsPath is empty"}

// ReadSettings reads the settings file. A file that does not exist gives empty settings.
func (s *Store) ReadSettings() (Settings, error) {
	var v Settings
	if s.SettingsPath == "" {
		return v, errNoSettingsPath
	}
	b, err := os.ReadFile(s.SettingsPath)
	if errors.Is(err, os.ErrNotExist) {
		return v, nil
	}
	if err != nil {
		return v, fmt.Errorf("read the settings: %w", err)
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return Settings{}, fmt.Errorf("read the settings %s: %w", s.SettingsPath, err)
	}
	return v, nil
}

// WriteSettings writes the settings file with mode 0600: a temporary file in the same folder, then a rename, so a
// reader never sees half a file.
func (s *Store) WriteSettings(v Settings) (err error) {
	if s.SettingsPath == "" {
		return errNoSettingsPath
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.SettingsPath), ".settings-*.tmp") // CreateTemp makes the file with mode 0600
	if err != nil {
		return fmt.Errorf("write the settings: %w", err)
	}
	defer func() {
		if err != nil {
			os.Remove(f.Name())
		}
	}()
	if _, err = f.Write(append(b, '\n')); err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return fmt.Errorf("write the settings: %w", err)
	}
	if err = os.Rename(f.Name(), s.SettingsPath); err != nil {
		return fmt.Errorf("write the settings: %w", err)
	}
	return nil
}

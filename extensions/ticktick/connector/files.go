// Package connector is the TickTick connected app: the sync from TickTick to the day pages, the catalog that the
// browser, `aminte do` and the MCP tools carry, and the loop that serve runs. It holds the files of its state folder.
package connector

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"aminte/app"
)

// Name is the name of the app: the program aminte-ticktick, the writer import:ticktick, the state folder
// aminte-ticktick.
const Name = "ticktick"

// ErrLoginAgain says that TickTick refused the token, or that there is none. The owner must run
// `aminte ticktick login`; a program that meets it ends with app.ExitOwnerActs (78), because the error is app.OwnerActs.
var ErrLoginAgain = app.OwnerActs(errors.New("login again: aminte ticktick login"))

// Dir is the state folder: os.UserConfigDir()/aminte-ticktick.
func Dir() (string, error) { return app.StateDir(Name) }

// Config is config.json. A missing key has its default.
type Config struct {
	API          string `json:"api"`
	AuthorizeURL string `json:"authorize_url"`
	TokenURL     string `json:"token_url"`
	Poll         string `json:"poll"` // a duration such as 15m
}

// DefaultConfig is the service of ticktick.com. The Dida365 variant is a config value, not code.
func DefaultConfig() Config {
	return Config{
		API:          "https://api.ticktick.com/open/v1",
		AuthorizeURL: "https://ticktick.com/oauth/authorize",
		TokenURL:     "https://ticktick.com/oauth/token",
		Poll:         "15m",
	}
}

// Every returns the poll interval. The interval must be at least one second.
func (c Config) Every() (time.Duration, error) {
	d, err := time.ParseDuration(c.Poll)
	if err != nil || d < time.Second {
		return 0, fmt.Errorf("config.json: poll %q is not a duration of one second or more, for example 15m", c.Poll)
	}
	return d, nil
}

// LoadConfig reads config.json in dir. A missing file gives the defaults.
func LoadConfig(dir string) (Config, error) {
	c := DefaultConfig()
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	var in Config
	if err := json.Unmarshal(data, &in); err != nil {
		return c, fmt.Errorf("config.json: %w", err)
	}
	for _, f := range []struct{ to, from *string }{{&c.API, &in.API}, {&c.AuthorizeURL, &in.AuthorizeURL},
		{&c.TokenURL, &in.TokenURL}, {&c.Poll, &in.Poll}} {
		if *f.from != "" {
			*f.to = *f.from
		}
	}
	return c, nil
}

// WriteDefaultConfig writes config.json with the defaults when the file is missing. The owner edits it by hand.
func WriteDefaultConfig(dir string) error {
	path := filepath.Join(dir, "config.json")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	data, err := json.MarshalIndent(DefaultConfig(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// SaveToken writes token.json with mode 0600.
func SaveToken(dir, token string) error {
	data, err := json.Marshal(map[string]string{"access_token": token})
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "token.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600) // WriteFile keeps the mode of a file that exists
}

// LoadToken reads token.json. No file is ErrLoginAgain.
func LoadToken(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "token.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrLoginAgain
	}
	if err != nil {
		return "", err
	}
	var in struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(data, &in); err != nil || in.AccessToken == "" {
		return "", fmt.Errorf("token.json holds no access_token: %w", ErrLoginAgain)
	}
	return in.AccessToken, nil
}

// State is state.json: what the next poll needs to know.
type State struct {
	// Cursor is the instant from which the next poll asks for done tasks. Zero: a first run.
	Cursor time.Time `json:"cursor"`
	// Seen maps the id of each open task that the last poll saw to its list. The way that has no /task/completed uses it.
	Seen map[string]string `json:"seen"`
	// fetchOnly is set for the rest of the run when the server has no /task/completed. It is not saved: a new run asks again.
	fetchOnly bool
}

// LoadState reads state.json. A missing file is an empty state.
func LoadState(dir string) (*State, error) {
	st := &State{Seen: map[string]string{}}
	data, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, st); err != nil {
		return nil, fmt.Errorf("state.json: %w", err)
	}
	if st.Seen == nil {
		st.Seen = map[string]string{}
	}
	return st, nil
}

// Save writes state.json through a temporary file, so that a stop never leaves half a file.
func (s *State) Save(dir string) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "state.json.tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "state.json"))
}

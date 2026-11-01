package relay

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// state is what the relay keeps between two runs, in state.json of its state folder. The page is the truth about
// the talk. The state says only which turns of it the relay has dealt with.
type state struct {
	// Seen holds the keys of the turns of a conversation page that the relay has dealt with (the key that the API gives
	// each turn), for each day that it watches: today, and the day before while its last turn is the owner's. It is a
	// set and not a position, because the core puts a reply right after the turn that it answers, and that can be
	// before a newer turn. A turn that moves (an insert above it, a delete and a revive of the page) is still a turn
	// that was dealt with. After each look the keys of a day are the keys of the turns that the page has: a turn that
	// was removed leaves no key. nil is a first run: the relay adopts the page as it is, and sends no earlier turn. The
	// file of an earlier version is a first run too: it has no seen.
	Seen map[string][]string `json:"seen"`
	// Said is the time of the last message of the owner that the relay said to lifelog. The chat gives a message
	// that came while the relay was off, and the relay says it when it is newer than Said.
	Said time.Time `json:"said"`
	// SavedAt is a field of the state file of the agent that lived in this program: the time of its last save of the
	// page. Every message of the owner before it was answered. loadState moves it to Said when the file has no Said,
	// so that a message that came during the switch is said. It is never written.
	SavedAt *time.Time `json:"saved_at,omitempty"`
}

// loadState reads the state file. A file that does not exist is a first run: a state with no seen.
func loadState(path string) (*state, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &state{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("state file: %w", err)
	}
	var s state
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("state file %s is not JSON: %w (delete it to start again)", path, err)
	}
	if s.Said.IsZero() && s.SavedAt != nil {
		s.Said = *s.SavedAt
	}
	s.SavedAt = nil
	return &s, nil
}

// LastSaid is the time of the last message of the owner that the relay said, and false when there is none.
func LastSaid(path string) (time.Time, bool, error) {
	s, err := loadState(path)
	if err != nil || s.Said.IsZero() {
		return time.Time{}, false, err
	}
	return s.Said, true, nil
}

// save writes the state file through a temporary file, so a crash leaves the old file or the new one.
func (s *state) save(path string) error {
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("state file: %w", err)
	}
	tmp := filepath.Join(filepath.Dir(path), ".state.json.tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("state file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("state file: %w", err)
	}
	return nil
}

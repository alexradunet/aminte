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
// the talk. The state says only how far the relay has sent it to the chat.
type state struct {
	// Days is the number of turns of a conversation page that the relay has dealt with, for each day that it watches:
	// today, and the day before while its last turn is the owner's. nil is a first run: the relay adopts the page as it
	// is, and sends no earlier turn. The file of an earlier version (a page, with no days) is a first run too.
	Days map[string]int `json:"days"`
	// Said is the time of the last message of the owner that the relay said to lifelog. The chat gives a message
	// that came while the relay was off, and the relay says it when it is newer than Said.
	Said time.Time `json:"said"`
}

// loadState reads the state file. A file that does not exist is a state with no days.
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

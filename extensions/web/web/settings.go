package web

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// The settings of the browser are the choices of the owner that only this program needs. They live in a file of its own
// state folder, never in life.db and never in the settings file of the core. The core does not read them. The keys are
// the keys of the settings file of the core (ground, lock_after, lock_pin_hash), so that the owner copies them with one
// jq line (README.md).

// grounds are the floors of the pages, the options of the field ground, in the order of the select. The first is the
// floor of a page with no choice (docs/plans/153-the-ground-in-settings.md).
var grounds = []string{"boards", "oak", "firelight", "embers"}

// lockAfters are the idle minutes of the lock, the options of the field lock_after. "off" is the default
// (docs/plans/160-the-lock.md).
var lockAfters = []string{"off", "5", "10", "15", "30", "60"}

// settingsFile is the name of the file in the state folder.
const settingsFile = "settings.json"

// browserSettings are the values in the file.
type browserSettings struct {
	// Ground is the floor of the page: one of grounds. Empty is the first.
	Ground string `json:"ground,omitempty"`
	// LockAfter is the idle minutes after which the lock closes the page, as text. Empty is off.
	LockAfter string `json:"lock_after,omitempty"`
	// LockPINHash is the stored form of the PIN that opens a locked browser (hashPIN). The file never holds the PIN.
	LockPINHash string `json:"lock_pin_hash,omitempty"`
}

// lockMinutes is the idle time of the lock in minutes; 0 means that the lock is off. The lock is on only when a PIN
// is stored: without one, nothing could open a locked browser.
func (s browserSettings) lockMinutes() int {
	n, err := strconv.Atoi(s.LockAfter)
	if err != nil || n <= 0 || s.LockPINHash == "" {
		return 0
	}
	return n
}

// groundOf is the floor that a page draws: the setting, and the first floor when nothing is set or the file holds a word
// that is not a floor (a hand edit: the action refuses it).
func groundOf(s browserSettings) string {
	if slices.Contains(grounds, s.Ground) {
		return s.Ground
	}
	return grounds[0]
}

// lockAfterOf is the value that a form shows for lock_after: off when the file holds none.
func lockAfterOf(s browserSettings) string {
	if s.LockAfter == "" {
		return "off"
	}
	return s.LockAfter
}

// settingsStore holds the settings in memory and in their file. This program is the only writer of the file, so the
// copy in memory is always the file, and a request reads memory. A change that the owner makes in the file by hand is
// live after the next start.
type settingsStore struct {
	mu   sync.RWMutex
	path string
	cur  browserSettings
}

// openSettings reads the file in dir, which it makes (mode 0700) when it is missing. A missing file is empty settings.
// A file that cannot be read is an error: the program does not start on settings that it cannot read, because the lock
// would have to guess what they say.
func openSettings(dir string) (*settingsStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("the state folder: %w", err)
	}
	s := &settingsStore{path: filepath.Join(dir, settingsFile)}
	b, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, fmt.Errorf("read the settings: %w", err)
	}
	if err := json.Unmarshal(b, &s.cur); err != nil {
		return nil, fmt.Errorf("read the settings %s: %w", s.path, err)
	}
	return s, nil
}

func (s *settingsStore) get() browserSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

// update applies change to a copy of the settings and writes the copy. Memory changes only after the file is written, so
// a failed write leaves both as they were. It returns the new settings. change is quick: it holds every reader.
func (s *settingsStore) update(change func(*browserSettings) error) (browserSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cur
	if err := change(&next); err != nil {
		return s.cur, err
	}
	if err := writeAtomically(s.path, next); err != nil {
		return s.cur, err
	}
	s.cur = next
	return next, nil
}

// writeAtomically writes v as JSON with mode 0600: a temporary file in the same folder, synced, then a rename, so a
// reader never sees half a file.
func writeAtomically(path string, v browserSettings) (err error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".settings-*.tmp") // CreateTemp makes the file with mode 0600
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
	if err = os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("write the settings: %w", err)
	}
	return nil
}

// pinIterations is the cost of one PBKDF2 try. A PIN has few values, so the cost protects little if the file is read;
// the limit of tries (wrongPINLimit) protects the PIN.
const pinIterations = 200_000

// hashPIN is the stored form of pin: "pbkdf2-sha256$ITERATIONS$SALT$KEY", with the salt and the key in base64. The salt
// and the count are in the text, so a later version can change the count and still read an old hash. It is the form
// that the core stored, so a hash copied from its settings file works here.
func hashPIN(pin string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("hash the PIN: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, pin, salt, pinIterations, 32)
	if err != nil {
		return "", fmt.Errorf("hash the PIN: %w", err)
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pinIterations, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// pinMatches reports whether pin is the PIN that stored (from hashPIN) was made from. A stored text that is not of that
// form matches nothing.
func pinMatches(stored, pin string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil || n < 1 || n > 10_000_000 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pin, salt, n, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

// pinOK reports a PIN of 4 to 8 ASCII digits.
func pinOK(pin string) bool {
	return len(pin) >= 4 && len(pin) <= 8 && strings.Trim(pin, "0123456789") == ""
}

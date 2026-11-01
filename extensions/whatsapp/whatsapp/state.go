// Package whatsapp is the adapter between the relay and WhatsApp. It uses whatsmeow, the Go client of the WhatsApp
// Web protocol. The state folder holds the session store, which has the keys of the account.
//
// The connection to WhatsApp has no test: it needs a real number. The filter, the QR renderer and the state folder
// have tests.
package whatsapp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	_ "modernc.org/sqlite" // the pure-Go driver, the one that lifelog pins: no cgo

	"lifelog/app"
)

// ErrNotPaired is the error of a store with no device: run the login first. It is an error that the owner acts on
// (app.OwnerActs): a restart does not pair the device.
var ErrNotPaired = app.OwnerActs(errors.New("not paired: run lifelog whatsapp login"))

// Prepare makes the state folder dir (app.StateDir("whatsapp")) with mode 0700 and returns the paths of the session
// store and the state file of the conversation. A folder that exists gets mode 0700 too, because the store in it holds
// the keys of the account.
func Prepare(dir string) (session, state string, err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("state folder: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("state folder: %w", err)
	}
	return filepath.Join(dir, "session.db"), filepath.Join(dir, "state.json"), nil
}

// Session is the store of the keys of the account, and its device.
type Session struct {
	db   *sql.DB
	dev  *store.Device
	path string
}

// Open opens the session store at path and makes it with mode 0600 when it is new. The store is a SQLite file.
func Open(ctx context.Context, path string) (*Session, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) // the mode is set before SQLite makes the file
	if err != nil {
		return nil, fmt.Errorf("session store: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("session store: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("session store: %w", err)
	}
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // a path of Windows: C:/dir/file
	}
	dsn := (&url.URL{Scheme: "file", Path: p, RawQuery: "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("session store: %w", err)
	}
	c := sqlstore.NewWithDB(db, "sqlite3", nil) // the dialect string picks the SQL flavour only
	if err := c.Upgrade(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("session store: %w", err), db.Close())
	}
	dev, err := c.GetFirstDevice(ctx)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("session store: %w", err), db.Close())
	}
	return &Session{db: db, dev: dev, path: path}, nil
}

// Number is the number that the device is paired as (digits), "" when it is not paired.
func (s *Session) Number() string {
	if s.dev.ID == nil {
		return ""
	}
	return s.dev.ID.User
}

// Path is the path of the session file.
func (s *Session) Path() string { return s.path }

// Close closes the store.
func (s *Session) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("session store: %w", err)
	}
	return nil
}

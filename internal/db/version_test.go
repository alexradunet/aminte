package db

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenRefusesUnsupportedSchemaVersion(t *testing.T) {
	for _, opener := range []struct {
		name string
		open func(context.Context, string) (*DB, error)
	}{{"writer", Open}, {"snapshot", OpenSnapshot}} {
		for _, version := range []int{0, -1, 2, 2147483647} {
			t.Run(fmt.Sprintf("%s/%d", opener.name, version), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "unsupported.db")
				if err := Init(t.Context(), path); err != nil {
					t.Fatal(err)
				}
				fixture, err := sql.Open("sqlite", sqliteFileURI(path, nil))
				if err != nil {
					t.Fatal(err)
				}
				fixture.SetMaxOpenConns(1)
				for _, statement := range []string{
					"PRAGMA journal_mode=DELETE",
					"INSERT INTO aminte_meta(key,value) VALUES('version-test','retained synthetic contents')",
					fmt.Sprintf("PRAGMA user_version=%d", version),
				} {
					if _, err := fixture.Exec(statement); err != nil {
						fixture.Close()
						t.Fatal(err)
					}
				}
				if err := fixture.Close(); err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				opened, openErr := opener.open(t.Context(), path)
				if opened != nil {
					if err := opened.Close(); err != nil {
						t.Fatal(err)
					}
				}
				if openErr == nil || opened != nil || !strings.Contains(openErr.Error(), "user_version") {
					t.Errorf("unsupported user_version %d: opened=%v err=%v", version, opened != nil, openErr)
				}
				after, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(before, after) {
					t.Error("refused database contents changed")
				}
			})
		}
	}
}

func TestOpenAcceptsCurrentSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "current.db")
	if err := Init(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	for _, open := range []func(context.Context, string) (*DB, error){Open, OpenSnapshot} {
		d, err := open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		var version int
		queryErr := d.W.QueryRow("PRAGMA user_version").Scan(&version)
		closeErr := d.Close()
		if queryErr != nil || closeErr != nil || version != 1 {
			t.Fatalf("current schema version=%d query=%v close=%v", version, queryErr, closeErr)
		}
	}
}

func TestOpenReadsSchemaVersionFromWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal-version.db")
	if err := Init(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	fixture, err := sql.Open("aminte", dsn(path, false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := fixture.Close(); err != nil {
			t.Error(err)
		}
	})
	fixture.SetMaxOpenConns(1)
	for _, statement := range []string{"PRAGMA wal_autocheckpoint=0", "PRAGMA wal_checkpoint(TRUNCATE)", "BEGIN IMMEDIATE", "PRAGMA user_version=2", "COMMIT"} {
		if _, err := fixture.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	read := func(file string) []byte {
		t.Helper()
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	mainBefore, walBefore := read(path), read(path+"-wal")
	if len(walBefore) == 0 {
		t.Fatal("fixture has no outstanding WAL data")
	}
	for _, open := range []func(context.Context, string) (*DB, error){Open, OpenSnapshot} {
		d, err := open(t.Context(), path)
		if d != nil {
			if closeErr := d.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
		}
		if err == nil || d != nil || !strings.Contains(err.Error(), "user_version 2") {
			t.Errorf("WAL version was not refused: opened=%v err=%v", d != nil, err)
		}
		if !bytes.Equal(mainBefore, read(path)) || !bytes.Equal(walBefore, read(path+"-wal")) {
			t.Error("refusal changed database or outstanding WAL contents")
		}
	}
}

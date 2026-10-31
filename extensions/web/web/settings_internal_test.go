package web

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPINHash: the core's TestPINHash. A hash is salted, so two hashes of one PIN differ; it matches its PIN and no other;
// a text that is not a hash matches nothing.
func TestPINHash(t *testing.T) {
	a, err := hashPIN("0042")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := hashPIN("0042")
	if a == b {
		t.Error("two hashes of one PIN are equal: the salt is not random")
	}
	if !pinMatches(a, "0042") || pinMatches(a, "42") || pinMatches(a, "") {
		t.Error("pinMatches")
	}
	for _, bad := range []string{"", "x", "pbkdf2-sha256$1$a$b", "pbkdf2-sha256$0$AA$AA", "md5$1$AA$AA"} {
		if pinMatches(bad, "0042") {
			t.Errorf("%q matches", bad)
		}
	}
}

// TestAHashOfAnotherProgramMatches: the form is the form of the settings file of the core, so a hash copied from it works
// here. This one was made with another implementation of PBKDF2-HMAC-SHA256 (Python hashlib), 200000 rounds, the salt
// 0x00..0x0f and the PIN 4242.
func TestAHashOfAnotherProgramMatches(t *testing.T) {
	const stored = "pbkdf2-sha256$200000$AAECAwQFBgcICQoLDA0ODw$lk3Qq3nONx8pHPLSBKyIUqKR3W/kfvuw7I33oi/LFNc"
	if !pinMatches(stored, "4242") || pinMatches(stored, "4243") {
		t.Error("the hash of Python does not match its PIN, or matches another")
	}
}

// TestOnlyAFourToEightDigitPINIsOK: ASCII digits only.
func TestOnlyAFourToEightDigitPINIsOK(t *testing.T) {
	for pin, want := range map[string]bool{"1234": true, "12345678": true, "0000": true, "123": false, "123456789": false, "": false,
		"12a4": false, "12 4": false, "١٢٣٤": false, "-123": false, "12.4": false} {
		if got := pinOK(pin); got != want {
			t.Errorf("pinOK(%q) = %v, want %v", pin, got, want)
		}
	}
}

// TestTheLockIsOnOnlyWithAPINAndAPositiveNumber: lock_after counts only when a PIN is stored; the settings of a hand edit
// that name a ground that is none draw the first ground.
func TestTheLockIsOnOnlyWithAPINAndAPositiveNumber(t *testing.T) {
	for _, c := range []struct {
		s    browserSettings
		want int
	}{
		{browserSettings{LockAfter: "15", LockPINHash: "h"}, 15},
		{browserSettings{LockAfter: "15"}, 0},
		{browserSettings{LockPINHash: "h"}, 0},
		{browserSettings{LockAfter: "off", LockPINHash: "h"}, 0},
		{browserSettings{LockAfter: "0", LockPINHash: "h"}, 0},
		{browserSettings{LockAfter: "-5", LockPINHash: "h"}, 0},
	} {
		if got := c.s.lockMinutes(); got != c.want {
			t.Errorf("%+v: %d minutes, want %d", c.s, got, c.want)
		}
	}
	if groundOf(browserSettings{Ground: "marble"}) != "boards" || groundOf(browserSettings{}) != "boards" || groundOf(browserSettings{Ground: "embers"}) != "embers" {
		t.Error("groundOf")
	}
}

// TestTheFileThatTheOwnerCopiesIsRead: the one-off step of the README copies three keys of the settings file of the core.
// A key that the core file lacks is null after jq, and a null is an empty value.
func TestTheFileThatTheOwnerCopiesIsRead(t *testing.T) {
	dir := t.TempDir()
	const stored = "pbkdf2-sha256$200000$AAECAwQFBgcICQoLDA0ODw$lk3Qq3nONx8pHPLSBKyIUqKR3W/kfvuw7I33oi/LFNc"
	for name, file := range map[string]string{
		"all three":  `{"ground":"oak","lock_after":"15","lock_pin_hash":"` + stored + `"}`,
		"with nulls": `{"ground":null,"lock_after":"15","lock_pin_hash":"` + stored + `"}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, settingsFile), []byte(file), 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := openSettings(dir)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := s.get()
		if got.lockMinutes() != 15 || !pinMatches(got.LockPINHash, "4242") || (name == "all three") != (got.Ground == "oak") {
			t.Errorf("%s: %+v", name, got)
		}
	}
}

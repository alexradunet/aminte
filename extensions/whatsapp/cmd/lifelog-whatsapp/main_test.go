package main

import (
	"strings"
	"testing"

	"lifelog-whatsapp/whatsapp"
	"lifelog/app"
)

// TestAConnectionThatEndedForGoodIsForTheOwnerToMend: a device that is logged out, replaced, banned or outdated needs
// the owner, so the exit status is 78 and serve does not start the program again. A failure of the connection that a
// new process may mend is status 1: serve starts the program again. There is no other status for either.
func TestAConnectionThatEndedForGoodIsForTheOwnerToMend(t *testing.T) {
	for _, tc := range []struct {
		name   string
		ended  whatsapp.Ended
		status int
	}{
		{"logged out", whatsapp.Ended{Reason: "the phone logged this device out"}, app.ExitOwnerActs},
		{"a failure that a new process may mend", whatsapp.Ended{Reason: "the connection failed", Retry: true}, 1},
	} {
		err := endError(tc.ended)
		if got := app.ExitCode(err); got != tc.status || !strings.Contains(err.Error(), tc.ended.Reason) {
			t.Errorf("%s: %v with exit status %d, want the reason and status %d", tc.name, err, got, tc.status)
		}
	}
}

// TestAnUnpairedDeviceIsForTheOwnerToMend: the error of a store with no device is the owner's to mend as well.
func TestAnUnpairedDeviceIsForTheOwnerToMend(t *testing.T) {
	if got := app.ExitCode(whatsapp.ErrNotPaired); got != app.ExitOwnerActs {
		t.Errorf("exit status %d, want %d", got, app.ExitOwnerActs)
	}
}

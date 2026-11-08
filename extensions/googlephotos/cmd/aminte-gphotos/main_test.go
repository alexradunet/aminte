package main

import (
	"strings"
	"testing"

	"aminte/app"
)

// TestNoAddressOfAminteIsRefused: with no AMINTE_URL and no --url the program refuses at once, before it reads the
// credentials or opens its page. A guessed address could be the server of another file (extensions/README.md). The
// refusal is the owner's to mend, so its exit status is 78.
func TestNoAddressOfAminteIsRefused(t *testing.T) {
	t.Setenv("AMINTE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no credentials here: a program that went on would fail on them, not on the address
	t.Setenv("APPDATA", t.TempDir())
	err := run(t.Context(), []string{"--addr", "127.0.0.1:0"})
	if err == nil || !strings.Contains(err.Error(), "no AMINTE_URL and no --url: run it as `aminte gphotos` on") {
		t.Fatalf("run with no address of aminte: %v, want the refusal that names AMINTE_URL and --url", err)
	}
	if got := app.ExitCode(err); got != app.ExitOwnerActs {
		t.Errorf("exit status %d, want %d", got, app.ExitOwnerActs)
	}
}

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// The nightly script of deploy/ runs against a stub program in place of aminte. The stub makes the snapshot that the
// test wants, or fails, and the test reads the exit status of the script and the files that are left.

// runSnapshotScript runs deploy/aminte-snapshot with a stub program: the stub exits with `status`, after it writes
// the files `produce` names into the folder. It returns the exit status of the script and the names left in the folder.
func runSnapshotScript(t *testing.T, folder []string, produce []string, status int, env ...string) (int, []string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the snapshot script is a POSIX sh script")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on this machine")
	}
	dir := t.TempDir()
	for _, name := range folder {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stub := filepath.Join(t.TempDir(), "aminte")
	// The stub is called as `aminte snapshot --snapshots DIR`. It checks that command line.
	var body strings.Builder
	body.WriteString("#!/bin/sh\n[ \"$1\" = snapshot ] && [ \"$2\" = --snapshots ] && [ \"$3\" = \"" + dir + "\" ] || exit 64\n")
	for _, name := range produce {
		fmt.Fprintf(&body, "echo new > \"%s/%s\"\n", dir, name)
	}
	fmt.Fprintf(&body, "exit %d\n", status)
	if err := os.WriteFile(stub, []byte(body.String()), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sh, filepath.Join("..", "..", "deploy", "aminte-snapshot"))
	cmd.Env = append(os.Environ(), "AMINTE_BIN="+stub, "AMINTE_SNAPSHOTS="+dir, "AMINTE_DB=", "AMINTE_KEEP=3")
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("run the script: %v\n%s", err, out)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, e := range entries {
		left = append(left, e.Name())
	}
	slices.Sort(left)
	t.Logf("script exit %d, left %v\n%s", code, left, out)
	return code, left
}

// TestSnapshotScriptPrunesOnlyGoodSnapshots: after a good snapshot the script keeps the newest N good names, the
// life-DATEThhmmss.db fallback included, and it neither counts nor removes a .failed.db file.
func TestSnapshotScriptPrunesOnlyGoodSnapshots(t *testing.T) {
	folder := []string{
		"life-2031-05-10.db", "life-2031-05-11.db", "life-2031-05-12.failed.db",
		"life-2031-05-12.db", "life-2031-05-12T090000.db", "life-2031-05-11.2.failed.db", "notes.txt",
	}
	code, left := runSnapshotScript(t, folder, []string{"life-2031-05-13.db"}, 0)
	want := []string{
		"life-2031-05-11.2.failed.db", "life-2031-05-12.db", "life-2031-05-12.failed.db", "life-2031-05-12T090000.db",
		"life-2031-05-13.db", "notes.txt",
	}
	if code != 0 || !slices.Equal(left, want) {
		t.Errorf("exit %d, left %v; want exit 0 and %v", code, left, want)
	}
}

// TestSnapshotScriptTestsTheExitStatusNotTheFile: the program fails, but it leaves a file with the good name (a cut
// snapshot). The script exits 1, prunes nothing and does not call the file good.
func TestSnapshotScriptTestsTheExitStatusNotTheFile(t *testing.T) {
	folder := []string{"life-2031-05-09.db", "life-2031-05-10.db", "life-2031-05-11.db", "life-2031-05-12.db"}
	code, left := runSnapshotScript(t, folder, []string{"life-2031-05-13.db"}, 1)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if want := append(slices.Clone(folder), "life-2031-05-13.db"); !slices.Equal(left, want) {
		t.Errorf("left %v, want %v: a failed run prunes nothing", left, want)
	}
}

// TestSnapshotScriptRefusesABadKeep: with a AMINTE_KEEP that is not a number above 0 the script stops
// before it runs the program or removes a file.
func TestSnapshotScriptRefusesABadKeep(t *testing.T) {
	folder := []string{"life-2031-05-09.db"}
	code, left := runSnapshotScript(t, folder, nil, 0, "AMINTE_KEEP=0")
	if code != 2 || !slices.Equal(left, folder) {
		t.Errorf("exit %d, left %v; want exit 2 and %v", code, left, folder)
	}
}

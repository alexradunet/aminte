package main

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"aminte/internal/db/dbtest"
)

var (
	fakeOnce  sync.Once
	fakeBytes []byte
	fakeErr   error
)

// fakeExtension builds testdata/fakeext once and writes a copy of it as aminte-fake into dir. It returns the path.
func fakeExtension(t *testing.T, dir string) string {
	t.Helper()
	fakeOnce.Do(func() {
		tmp, err := os.MkdirTemp("", "fakeext")
		if err != nil {
			fakeErr = err
			return
		}
		defer os.RemoveAll(tmp)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		bin := filepath.Join(tmp, "fake")
		if out, err := exec.CommandContext(ctx, "go", "build", "-o", bin, "./testdata/fakeext").CombinedOutput(); err != nil {
			fakeErr = errors.New("build: " + err.Error() + "\n" + string(out))
			return
		}
		fakeBytes, fakeErr = os.ReadFile(bin)
	})
	if fakeErr != nil {
		t.Fatal(fakeErr)
	}
	name := "aminte-fake"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, fakeBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// extensionEnvAt is a test env whose program is DIR/aminte, so that DIR is the folder of the program.
func extensionEnvAt(t *testing.T, dir string) (e env, report func(argv ...string) (map[string]string, error)) {
	t.Helper()
	e, stdout, _ := testEnv(t)
	e.executable = func() (string, error) { return filepath.Join(dir, "aminte"), nil }
	return e, func(argv ...string) (map[string]string, error) {
		stdout.Reset()
		err := runArgs(t.Context(), e, argv)
		got := map[string]string{}
		for _, line := range strings.Split(stdout.String(), "\n") {
			if k, v, ok := strings.Cut(line, "="); ok {
				got[k] = v
			}
		}
		return got, err
	}
}

func TestAnExtensionGetsTheWordsAfterItsNameInOrder(t *testing.T) {
	dir := t.TempDir()
	fakeExtension(t, dir)
	_, run := extensionEnvAt(t, dir)
	for _, tc := range []struct {
		argv []string
		want string
	}{
		{[]string{"fake", "a", "--addr", "x:1", "b"}, "a|--addr|x:1|b"},
		{[]string{"fake", "--force", "--mood=3", "--", "--x"}, "--force|--mood=3|--|--x"},
		{[]string{"fake", "--source", "x", "--human", "-h"}, "--source|x|--human|-h"}, // after the name they are the extension's
		{[]string{"fake", "one", "--db", filepath.Join(dir, "none.db"), "two", "--db=x.db", "three"}, "one|two|three"},
		{[]string{"fake", "--", "--db", "x", "--url", "u"}, "--|--db|x|--url|u"}, // after --, even these are the extension's
	} {
		got, err := run(tc.argv...)
		if err != nil {
			t.Fatalf("%v: %v", tc.argv, err)
		}
		if got["args"] != tc.want {
			t.Errorf("%v: the extension got %q, want %q", tc.argv, got["args"], tc.want)
		}
	}
}

func TestAnExtensionRefusesTheFlagsThatItsOwnerDecides(t *testing.T) {
	dir := t.TempDir()
	fakeExtension(t, dir)
	_, run := extensionEnvAt(t, dir)
	for _, tc := range []struct {
		argv []string
		want string
	}{
		{[]string{"--source", "x", "fake"}, "an extension names its own writer: drop --source"},
	} {
		if _, err := run(tc.argv...); err == nil || err.Error() != tc.want {
			t.Errorf("%v: %v, want %q", tc.argv, err, tc.want)
		}
	}
	if _, err := run("--human", "fake"); err == nil || err.Error() != "an extension prints its own output: drop --human" {
		t.Errorf("--human: %v", err)
	}
	if _, err := run("--mood", "3", "fake"); err == nil || !strings.Contains(err.Error(), "--mood") {
		t.Errorf("--mood before the name: %v, want a refusal", err)
	}
	if _, err := run("fake", "--db", "a.db", "--url", "http://x"); err == nil || !strings.Contains(err.Error(), "two databases") {
		t.Errorf("--db and --url: %v, want a refusal", err)
	}
}

func TestAnExtensionHasTheAPIOfTheFileWhileItRuns(t *testing.T) {
	dir := t.TempDir()
	fakeExtension(t, dir)
	file := dbtest.File(t)
	_, run := extensionEnvAt(t, dir)
	got, err := run("fake", "--db", file)
	if err != nil {
		t.Fatal(err)
	}
	if got["status"] != "200" {
		t.Errorf("GET /actions answered %q, want 200", got["status"])
	}
	u, err := url.Parse(got["AMINTE_URL"])
	if err != nil || u.Scheme != "http" || !strings.HasPrefix(u.Host, "127.0.0.1:") {
		t.Fatalf("AMINTE_URL = %q, want http://127.0.0.1:PORT", got["AMINTE_URL"])
	}
	if got["AMINTE_DB"] != file {
		t.Errorf("AMINTE_DB = %q, want %q", got["AMINTE_DB"], file)
	}
	if want := filepath.Join(dir, "aminte"); got["AMINTE_EXE"] != want {
		t.Errorf("AMINTE_EXE = %q, want %q", got["AMINTE_EXE"], want)
	}
	if conn, err := net.DialTimeout("tcp", u.Host, 2*time.Second); err == nil {
		conn.Close()
		t.Errorf("%s still answers after the extension ended", u.Host)
	}
}

func TestAnExtensionWithAURLOpensNoFile(t *testing.T) {
	dir := t.TempDir()
	fakeExtension(t, dir)
	_, run := extensionEnvAt(t, dir)
	t.Setenv("AMINTE_URL", "http://inherited.invalid") // the line wins over the inherited one
	got, err := run("fake", "--url", "http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	if got["AMINTE_URL"] != "http://127.0.0.1:1" {
		t.Errorf("AMINTE_URL = %q, want the --url", got["AMINTE_URL"])
	}
	if _, err := os.Stat(filepath.Join(dir, "life.db")); !os.IsNotExist(err) {
		t.Errorf("life.db beside the program: %v, want none", err)
	}
}

func TestAnExtensionWithNoFileHasNoServer(t *testing.T) {
	dir := t.TempDir()
	fakeExtension(t, dir)
	_, run := extensionEnvAt(t, dir)
	t.Setenv("AMINTE_URL", "http://inherited.invalid")
	want := filepath.Join(dir, "none", "life.db")
	got, err := run("fake", "--db", want)
	if err != nil {
		t.Fatal(err)
	}
	if got["AMINTE_URL"] != "" {
		t.Errorf("AMINTE_URL = %q, want none", got["AMINTE_URL"])
	}
	if got["AMINTE_DB"] != want {
		t.Errorf("AMINTE_DB = %q, want %q", got["AMINTE_DB"], want)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Errorf("the file was created: %v", err)
	}
	// A relative path is made absolute.
	t.Chdir(dir)
	got, err = run("fake", "--db", "rel.db")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got["AMINTE_DB"]) || filepath.Base(got["AMINTE_DB"]) != "rel.db" {
		t.Errorf("AMINTE_DB = %q, want an absolute path", got["AMINTE_DB"])
	}
}

func TestTheExitStatusIsTheExtensions(t *testing.T) {
	dir := t.TempDir()
	fakeExtension(t, dir)
	_, run := extensionEnvAt(t, dir)
	_, err := run("fake", "exit=3")
	var x exitError
	if !errors.As(err, &x) || x.code != 3 {
		t.Errorf("err = %v, want an exitError with code 3", err)
	}
	if _, err := run("fake", "exit=0"); err != nil {
		t.Errorf("exit 0: %v, want none", err)
	}
}

func TestAnUnknownExtensionNamesBothPlaces(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	_, run := extensionEnvAt(t, dir)
	for _, name := range []string{"nothing", "../x", `a\b`} {
		_, err := run(name)
		want := "no command of aminte and no aminte-" + name + " beside this program or on PATH"
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want it to contain %q", name, err, want)
		}
	}
}

// TestAFileBesideTheProgramThatCannotRunIsRefused: an aminte-NAME beside the program that is not executable is an
// error, not a silent fall-through to a program of the same name on PATH.
func TestAFileBesideTheProgramThatCannotRunIsRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no executable bit")
	}
	beside, onPath := t.TempDir(), t.TempDir()
	fakeExtension(t, onPath)
	if err := os.WriteFile(filepath.Join(beside, "aminte-fake"), []byte("not a program"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", strings.Join([]string{onPath, os.Getenv("PATH")}, string(os.PathListSeparator)))
	_, run := extensionEnvAt(t, beside)
	_, err := run("fake")
	if err == nil || !strings.Contains(err.Error(), "beside this program but cannot run") {
		t.Errorf("err = %v, want the refusal of the file beside the program", err)
	}
}

// TestAnInterruptThatTheExtensionEndsCleanlyOnIsNoError: an interrupt reaches the extension through the context; when
// it ends with status 0 on it, aminte ends with no error, as the owner asked for the stop.
func TestAnInterruptThatTheExtensionEndsCleanlyOnIsNoError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot send an interrupt to a process: the console does")
	}
	dir := t.TempDir()
	fakeExtension(t, dir)
	e, _, _ := testEnv(t)
	e.executable = func() (string, error) { return filepath.Join(dir, "aminte"), nil }
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runArgs(ctx, e, []string{"fake", "wait", "--db", filepath.Join(dir, "none.db")}) }()
	ready := filepath.Join(dir, "ready")
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case <-tick.C:
		case err := <-done:
			t.Fatalf("the extension ended before it was ready: %v", err)
		case <-deadline.C:
			t.Fatal("the extension never said it was ready")
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("after an interrupt the extension ended cleanly on: %v, want no error", err)
	}
}

func TestTheFolderOfTheProgramWinsOverPATH(t *testing.T) {
	beside, onPath := t.TempDir(), t.TempDir()
	fakeExtension(t, beside)
	fakeExtension(t, onPath)
	t.Setenv("PATH", strings.Join([]string{onPath, os.Getenv("PATH")}, string(os.PathListSeparator)))
	_, run := extensionEnvAt(t, beside)
	got, err := run("fake")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got["self"], beside) {
		t.Errorf("ran %q, want the one beside the program in %q", got["self"], beside)
	}
	// Without a copy beside the program, PATH is used.
	empty := t.TempDir()
	_, run = extensionEnvAt(t, empty)
	if got, err = run("fake"); err != nil || !strings.HasPrefix(got["self"], onPath) {
		t.Errorf("ran %q (%v), want the one on PATH in %q", got["self"], err, onPath)
	}
}

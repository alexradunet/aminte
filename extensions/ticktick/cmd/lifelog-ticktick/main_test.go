package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"lifelog-ticktick/connector"
	"lifelog/app"
)

func noEnv(string) string { return "" }

func TestUsageAndRefusals(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	tests := []struct {
		name  string
		args  []string
		env   func(string) string
		want  string
		usage bool // the program prints its usage; else the flag package prints the usage of the command
	}{
		{"no command and no LIFELOG_LISTEN", nil, noEnv, "no command", true},
		{"an unknown command", []string{"frobnicate"}, noEnv, `unknown argument "frobnicate"`, true},
		{"an unknown flag of sync", []string{"sync", "--nope"}, noEnv, "flag provided but not defined", false},
		{"a word after sync", []string{"sync", "extra"}, noEnv, `unexpected argument "extra"`, true},
		{"a word after login", []string{"login", "extra"}, noEnv, `unexpected argument "extra"`, true},
		{"a port that is not a port", []string{"login", "--port", "70000"}, noEnv, "--port", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			err := run(t.Context(), tc.args, tc.env, strings.NewReader(""), io.Discard, &stderr)
			if app.ExitCode(err) != app.ExitOwnerActs || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want status %d and %q", err, app.ExitOwnerActs, tc.want)
			}
			// app.Main prints the error as the one last line "lifelog-ticktick: err". The usage is above it, on stderr.
			if strings.Contains(err.Error(), "\n") {
				t.Errorf("the error has more than one line: %q", err)
			}
			if got := strings.Contains(stderr.String(), usage); got != tc.usage {
				t.Errorf("stderr has the usage = %v, want %v:\n%s", got, tc.usage, &stderr)
			}
		})
	}
}

func TestSyncWithoutATokenSaysToLogInAndWithoutAnAddressSaysWhatToRun(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	err := run(t.Context(), []string{"sync", "--url", "http://127.0.0.1:1"}, noEnv, strings.NewReader(""), io.Discard, io.Discard)
	if !errors.Is(err, connector.ErrLoginAgain) || app.ExitCode(err) != app.ExitOwnerActs {
		t.Errorf("no token: err = %v with status %d, want ErrLoginAgain and status %d", err, app.ExitCode(err), app.ExitOwnerActs)
	}
	err = run(t.Context(), nil, func(k string) string {
		if k == "LIFELOG_LISTEN" {
			return "127.0.0.1:1"
		}
		return ""
	}, strings.NewReader(""), io.Discard, io.Discard)
	if !errors.Is(err, connector.ErrLoginAgain) || app.ExitCode(err) != app.ExitOwnerActs {
		t.Errorf("serve with no token: err = %v with status %d, want ErrLoginAgain and status %d at once", err, app.ExitCode(err), app.ExitOwnerActs)
	}
}

// TestSyncWithoutAnAddressSaysWhatToRun: the login is there, and the address of lifelog is not. The program never guesses
// one, so it refuses with the line that says what to run. The owner mends it, so the status is 78.
func TestSyncWithoutAnAddressSaysWhatToRun(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("LIFELOG_TOKEN", "")
	dir := filepath.Join(config, "lifelog-ticktick")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := connector.SaveToken(dir, "the-token"); err != nil {
		t.Fatal(err)
	}
	err := run(t.Context(), []string{"sync"}, noEnv, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "no LIFELOG_URL and no --url: run it as `lifelog ticktick sync` on") || app.ExitCode(err) != app.ExitOwnerActs {
		t.Errorf("sync with no address: err = %v with status %d, want the refusal and status %d", err, app.ExitCode(err), app.ExitOwnerActs)
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestLoginExchangesTheCodeAndWritesTheFiles(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	state := filepath.Join(config, "lifelog-ticktick")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	var gotCode, gotUser string
	token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotCode = r.PostForm.Get("code")
		gotUser, _, _ = r.BasicAuth()
		_, _ = io.WriteString(w, `{"access_token":"the-token"}`)
	}))
	defer token.Close()
	if err := os.WriteFile(filepath.Join(state, "config.json"),
		[]byte(`{"authorize_url":"https://x.example/authorize","token_url":"`+token.URL+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	port := freePort(t)
	out := new(lockedBuffer)
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	go func() {
		done <- run(ctx, []string{"login", "--port", strconv.Itoa(port)}, noEnv,
			strings.NewReader("the-id\nthe-secret\n"), out, io.Discard)
	}()

	var link *url.URL
	for link == nil { // the program prints the address after it listens; wait for that line
		select {
		case err := <-done:
			t.Fatalf("login ended early: %v\n%s", err, out)
		case <-time.After(10 * time.Millisecond):
		}
		for _, l := range strings.Split(out.String(), "\n") {
			if strings.HasPrefix(l, "https://x.example/authorize?") {
				link, _ = url.Parse(l)
			}
		}
	}
	q := link.Query()
	if q.Get("client_id") != "the-id" || q.Get("redirect_uri") != "http://127.0.0.1:"+strconv.Itoa(port)+"/callback" {
		t.Errorf("link = %s", link)
	}
	callback := "http://127.0.0.1:" + strconv.Itoa(port) + "/callback"

	res, err := http.Get(callback + "?code=evil&state=wrong")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("a callback with the wrong state = %d, want 400", res.StatusCode)
	}
	res, err = http.Get(callback + "?code=the-code&state=" + url.QueryEscape(q.Get("state")))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if err := <-done; err != nil {
		t.Fatalf("login: %v\n%s", err, out)
	}
	if gotCode != "the-code" || gotUser != "the-id" {
		t.Errorf("the token request had code %q and user %q", gotCode, gotUser)
	}
	tok, err := connector.LoadToken(state)
	if err != nil || tok != "the-token" {
		t.Errorf("token = %q, %v", tok, err)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(filepath.Join(state, "token.json")); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("token.json mode = %v, %v, want 0600", info.Mode(), err)
		}
	}
	if !strings.Contains(out.String(), "logged in; now `lifelog ticktick sync --url http://127.0.0.1:7777`, or add --with ticktick to serve") {
		t.Errorf("the last line is missing:\n%s", out)
	}
	if strings.Contains(out.String(), "the-secret") {
		t.Error("the secret was printed")
	}
}

// TestLoginTakesTheAddressPastedOnTheTerminal: the program runs on a server, the browser lands on the owner's own
// machine, and the owner pastes the address of the redirect. The state of a pasted address is checked as the state of
// a redirect is.
func TestLoginTakesTheAddressPastedOnTheTerminal(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		ok          bool
	}{{"the right state", "", true}, {"a wrong state", "wrong", false}} {
		t.Run(tc.name, func(t *testing.T) {
			config := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", config)
			state := filepath.Join(config, "lifelog-ticktick")
			if err := os.MkdirAll(state, 0o700); err != nil {
				t.Fatal(err)
			}
			var gotCode string
			token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseForm()
				gotCode = r.PostForm.Get("code")
				_, _ = io.WriteString(w, `{"access_token":"the-token"}`)
			}))
			defer token.Close()
			if err := os.WriteFile(filepath.Join(state, "config.json"),
				[]byte(`{"authorize_url":"https://x.example/authorize","token_url":"`+token.URL+`"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			port := freePort(t)
			out := new(lockedBuffer)
			stdin, write := io.Pipe()
			done := make(chan error, 1)
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			go func() {
				done <- run(ctx, []string{"login", "--port", strconv.Itoa(port)}, noEnv, stdin, out, io.Discard)
			}()
			if _, err := io.WriteString(write, "the-id\nthe-secret\n"); err != nil {
				t.Fatal(err)
			}
			var link *url.URL
			for link == nil {
				select {
				case err := <-done:
					t.Fatalf("login ended early: %v\n%s", err, out)
				case <-time.After(10 * time.Millisecond):
				}
				for _, l := range strings.Split(out.String(), "\n") {
					if strings.HasPrefix(l, "https://x.example/authorize?") {
						link, _ = url.Parse(l)
					}
				}
			}
			st := link.Query().Get("state")
			if tc.state != "" {
				st = tc.state
			}
			paste := "http://127.0.0.1:8765/callback?code=pasted-code&state=" + url.QueryEscape(st) + "&iss=https://x.example\n"
			if _, err := io.WriteString(write, paste); err != nil {
				t.Fatal(err)
			}
			err := <-done
			if tc.ok {
				if err != nil {
					t.Fatalf("login: %v\n%s", err, out)
				}
				if gotCode != "pasted-code" {
					t.Errorf("the token request had code %q", gotCode)
				}
				if tok, err := connector.LoadToken(state); err != nil || tok != "the-token" {
					t.Errorf("token = %q, %v", tok, err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), "state") {
					t.Fatalf("login with a wrong state: %v, want the refusal", err)
				}
				if gotCode != "" {
					t.Error("the code was exchanged although the state was wrong")
				}
			}
		})
	}
}

package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The two tests of this file keep the rings of plan 135 (AGENTS.md, "The four rings"): the core is the file, the
// writer and the catalog, and everything that names a third party or talks to one is an extension. Both read the Go
// source of internal/, cmd/, siren/, client/, app/, text/ and lifelogtest/ that is not a test.

// thirdParties are the products and companies the core never names. A thing that names one is an extension; the
// change that moves it out adds its name here.
var thirdParties = []string{"google", "apple", "telegram", "claude", "codex", "opencode", "anthropic", "openai", "modelcontextprotocol", "whatsapp", "whatsmeow", "ticktick"}

// dialPackages are the packages a Go program talks to a server with. The core imports them under their own names only,
// so that the words of dials below cannot hide behind an alias or a dot import.
var dialPackages = []string{"net", "net/http", "crypto/tls"}

// dials are the ways a Go program opens a connection. Only client, the client of lifelog's own server, may
// use them: so every integration with the outside world is an extension by force. A server (net.Listen, http.Server)
// is not a dial.
var dials = []string{"http.Client", "http.DefaultClient", "http.DefaultTransport", "http.Transport", "http.Get(", "http.Head(",
	"http.Post(", "http.PostForm(", "http.NewRequest", ".RoundTrip(", "net.Dial", "tls.Dial"}

func TestTheCoreNamesNoThirdParty(t *testing.T) {
	for path, text := range coreSources(t) {
		lower := strings.ToLower(text)
		for _, name := range thirdParties {
			if strings.Contains(lower, name) {
				t.Errorf("%s names %q: the core names no third party (AGENTS.md, the four rings)", path, name)
			}
		}
	}
}

func TestTheCoreDialsOnlyItsServer(t *testing.T) {
	fset := token.NewFileSet()
	for path, text := range coreSources(t) {
		f, err := parser.ParseFile(fset, path, text, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			pkg, _ := strconv.Unquote(imp.Path.Value)
			if imp.Name != nil && slices.Contains(dialPackages, pkg) {
				t.Errorf("%s imports %s as %s: the core imports it under its own name, so a dial cannot hide", path, pkg, imp.Name.Name)
			}
		}
		if strings.HasPrefix(filepath.ToSlash(path), "client/") {
			continue
		}
		for _, d := range dials {
			if strings.Contains(text, d) {
				t.Errorf("%s uses %s: outside client the core dials nothing (AGENTS.md, the four rings)", path, d)
			}
		}
		if strings.Contains(text, "client.Remote(") && !remoteAllowed[filepath.ToSlash(path)] {
			t.Errorf("%s makes a client.Remote: only a server that the core started or was told of may be dialled (AGENTS.md, the four rings)", path)
		}
	}
}

// remoteAllowed are the files that may call client.Remote. Each names a server that the core itself started, or was
// told of by the owner. No other file of the core dials a server, so a new one is a decision of the owner.
var remoteAllowed = map[string]bool{
	"cmd/lifelog/main.go":             true, // the --url of the owner: the lifelog server that the command line talks to
	"internal/api/apitest/apitest.go": true, // the server that a test started over its own handler
	"internal/api/with.go":            true, // a connected app that `serve --with` started, on the loopback address it gave (plan 154)
	"cmd/lifelog/with.go":             true, // the same children, when serve probes them before it registers them (plan 154)
}

// coreSources is the Go source of internal/, cmd/, siren/, client/, app/, text/ and lifelogtest/ that is not a test, by its path relative to the repo root.
func coreSources(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..")
	out := map[string]string{}
	for _, dir := range []string{"internal", "cmd", "siren", "client", "app", "text", "lifelogtest"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && d.Name() == "testdata" {
				return filepath.SkipDir
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			out[rel] = string(b)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(out) < 20 {
		t.Fatalf("read %d source files; the walk missed the tree", len(out))
	}
	return out
}

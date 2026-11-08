package client_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"aminte/client"
	"aminte/internal/api/apitest"
	"aminte/internal/core"
	"aminte/siren"
)

// TestTheTwoTransportsGiveEqualAnswers is the conformance test of the client: the same requests, sent in process and
// over a socket, get equal answers from the handler of the program. A write cannot give the same answer two times on
// one file (a reading sent again answers "Already recorded", a file kept again answers existing). Thus each transport
// has its own file, and the same steps make the two files. SQLite writes created_at from its own clock, so the test
// masks the instants and compares the rest of the JSON, byte for byte. The test also holds the in-process answer to
// values that are checked by hand. The other tests of the API run in process only.
func TestTheTwoTransportsGiveEqualAnswers(t *testing.T) {
	ctx := t.Context()
	notes := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(notes, []byte("synthetic notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const notesSHA256 = "c5e496e772bbe5034ed7341d5dbfce4f83368ff765cbfbaf2e9b7f4ad29bf484" // sha256sum of the line

	type side struct {
		c     *client.Client
		store *core.Store
	}
	open := func(remote bool) side {
		c, h, s := apitest.FreshOn(t, "cli", "2031-05-14")
		root := apitest.Must(t)(c.Get(ctx, "/"))
		apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Conformance", "body": "A [[Link]]."}))
		apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "register-metric"), map[string]string{"name": "Weight", "unit": "kg"}))
		if remote {
			c = apitest.Remote(t, h, "cli")
		}
		return side{c, s}
	}
	sides := []side{open(false), open(true)}

	// readingAt adds a reading at the id n with SQL, as another writer can do, so that the next reading gets the id n+1.
	readingAt := func(n string) func(*core.Store) error {
		return func(s *core.Store) error {
			return s.DB.Write(ctx, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, `INSERT INTO measurements(id, metric_id, day, value, created_at, source)
					SELECT `+n+`, entity_id, '2031-05-01', 70, '2031-05-01T08:00:00.000Z', 'cli' FROM entity_names WHERE name_key = 'weight'`)
				return err
			})
		}
	}
	// actionOf is the action with the name of the entity at href: the catalog for /actions.
	actionOf := func(c *client.Client, href, name string) (siren.Action, error) {
		e, err := c.Get(ctx, href)
		if err != nil {
			return siren.Action{}, err
		}
		for _, a := range e.Actions {
			if a.Name == name {
				return a, nil
			}
		}
		return siren.Action{}, fmt.Errorf("no action %s at %s", name, href)
	}
	do := func(href, name string, values map[string]string) func(*client.Client) (*siren.Entity, error) {
		return func(c *client.Client) (*siren.Entity, error) {
			a, err := actionOf(c, href, name)
			if err != nil {
				return nil, err
			}
			return c.Do(ctx, a, values)
		}
	}
	get := func(href string) func(*client.Client) (*siren.Entity, error) {
		return func(c *client.Client) (*siren.Entity, error) { return c.Get(ctx, href) }
	}
	record := func(value string) func(*client.Client) (*siren.Entity, error) {
		return do("/actions", "record", map[string]string{"metric": "Weight", "day": "2031-05-14", "value": value})
	}

	for _, step := range []struct {
		name   string
		before func(*core.Store) error // on each file, before the request
		send   func(*client.Client) (*siren.Entity, error)
		status int               // the HTTP status of a refusal, 0 for none
		want   map[string]string // properties of the answer, each as its JSON
	}{
		{name: "a read", send: get("/pages?title=Conformance"),
			want: map[string]string{"title": `"Conformance"`, "body": `"A [[Link]]."`, "version": `"1"`}},
		{name: "a write", send: do("/pages?title=Conformance", "save-body", map[string]string{"body": "Saved [[Other]]."}),
			want: map[string]string{"body": `"Saved [[Other]]."`, "version": `"2"`}},
		{name: "a file sent as multipart", send: func(c *client.Client) (*siren.Entity, error) {
			a, err := actionOf(c, "/actions", "add-file")
			if err != nil {
				return nil, err
			}
			return c.Do(ctx, a, map[string]string{"title": "Notes", "original": notes})
		}, want: map[string]string{"title": `"Notes"`, "file": `{"mime":"text/plain","preview":false,"sha256":"` + notesSHA256 + `"}`}},
		{name: "a 404", send: get("/pages/424242"),
			status: 404, want: map[string]string{"status": "404", "code": `"not_found"`}},
		{name: "a 409 with a code", send: do("/actions", "create-page", map[string]string{"title": "Conformance"}),
			status: 409, want: map[string]string{"status": "409", "code": `"exists"`}},
		{name: "a 422", send: record("heavy"),
			status: 422, want: map[string]string{"status": "422", "code": `"invalid"`}},
		{name: "an id of 2^53+1 and a float", before: readingAt("9007199254740992"), send: record("0.30000000000000004"),
			want: map[string]string{"id": "9007199254740993", "value": "0.30000000000000004"}},
		{name: "the reading at 2^53+1, read by its id", send: get("/measurements/9007199254740993"),
			want: map[string]string{"id": "9007199254740993", "value": "0.30000000000000004"}},
		{name: "an id of 2^63-1", before: readingAt("9223372036854775806"), send: record("71.5"),
			want: map[string]string{"id": "9223372036854775807", "value": "71.5"}},
		{name: "the reading at 2^63-1, read by its id", send: get("/measurements/9223372036854775807"),
			want: map[string]string{"id": "9223372036854775807", "value": "71.5"}},
	} {
		var answers [2]string
		for i, s := range sides {
			if step.before != nil {
				if err := step.before(s.store); err != nil {
					t.Fatalf("%s: %v", step.name, err)
				}
			}
			e, err := step.send(s.c)
			if got := apitest.Status(err); got != step.status || e == nil {
				t.Fatalf("%s, remote %v: status %d (%v), want %d with an entity", step.name, i == 1, got, err, step.status)
			}
			answers[i] = masked(t, e)
			if i == 0 {
				props, _ := e.Properties.(map[string]any)
				for k, want := range step.want {
					if got, err := json.Marshal(props[k]); err != nil || string(got) != want {
						t.Errorf("%s: the property %s is %s, want %s", step.name, k, got, want)
					}
				}
			}
		}
		if answers[0] != answers[1] {
			t.Errorf("%s: the answers differ\nin process: %s\nremote:     %s", step.name, answers[0], answers[1])
		}
	}
}

// TestOnlyTheRemoteClientUsesASocket: the two transports of the conformance test are different. The client in process
// gives the request to the handler with no socket, so the handler sees no peer address. The remote client comes to
// the handler over a socket, from a loopback address.
func TestOnlyTheRemoteClientUsesASocket(t *testing.T) {
	peers := make(chan string, 1)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peers <- r.RemoteAddr
		w.Header().Set("Content-Type", "application/vnd.siren+json")
		fmt.Fprint(w, `{"title":"peer"}`)
	})
	peerOf := func(c *client.Client) string {
		t.Helper()
		if e, err := c.Get(t.Context(), "/"); err != nil || e.Title != "peer" {
			t.Fatalf("GET /: %+v %v", e, err)
		}
		return <-peers
	}
	if peer := peerOf(client.InProcess(h, "cli")); peer != "" {
		t.Errorf("the client in process comes from the peer %q, want none", peer)
	}
	peer := peerOf(apitest.Remote(t, h, "cli"))
	if host, _, err := net.SplitHostPort(peer); err != nil || !net.ParseIP(host).IsLoopback() {
		t.Errorf("the remote client comes from the peer %q, want a loopback address", peer)
	}
}

// instant is an instant as SQLite writes it (aminte_meta.instants).
var instant = regexp.MustCompile(`"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}Z"`)

// masked is the JSON of e with each instant replaced by the same mark.
func masked(t *testing.T, e *siren.Entity) string {
	t.Helper()
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return instant.ReplaceAllString(string(b), `"<instant>"`)
}

package main_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"aminte-ticktick/ticktick"
	"aminte/client"
	"aminte/siren"
)

// The end-to-end test builds the real aminte and aminte-ticktick into one folder and runs
// `aminte serve --with ticktick` on a new life.db, with a fake TickTick. It holds the whole line: the handshake, the
// merged catalog, the capture with its import_key, the forwarding to the app and the exit rule. The live TickTick
// and its login have no test.

// wait is a guard against a deadlock, not a measure of speed.
const wait = 2 * time.Minute

// lockedBuffer is the stderr of the server: the process writes it and the test reads it.
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

// fakeTick is a TickTick in memory with two lists, an Inbox and the two switches of the test.
type fakeTick struct {
	mu           sync.Mutex
	tasks        map[string]*ticktick.Task
	noCompleted  bool
	unauthorized bool
	failRead     bool // when set, GET of one task answers 500
	nextID       int
	extra        map[string]map[string]any // the fields of a task that Task does not know
}

var knownKeys = map[string]bool{"id": true, "projectId": true, "title": true, "content": true, "status": true,
	"completedTime": true, "dueDate": true, "startDate": true, "timeZone": true, "isAllDay": true}

// wire is the task as TickTick sends it: the fields of Task and the extra ones.
func (f *fakeTick) wire(t *ticktick.Task) map[string]any {
	b, _ := json.Marshal(t)
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	for k, v := range f.extra[t.ID] {
		out[k] = v
	}
	return out
}

var lists = []ticktick.Project{{ID: "p-home", Name: "Home"}, {ID: "p-work", Name: "Work"}}

func (f *fakeTick) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.unauthorized || r.Header.Get("Authorization") != "Bearer test-token" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/open/v1")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case r.Method == "GET" && path == "/project":
		_ = json.NewEncoder(w).Encode(lists)
	case r.Method == "GET" && len(parts) == 3 && parts[2] == "data":
		open := []map[string]any{}
		for _, t := range f.tasks {
			if t.ProjectID == parts[1] && t.Status == 0 {
				open = append(open, f.wire(t))
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": open})
	case r.Method == "GET" && len(parts) == 4 && parts[2] == "task":
		if f.failRead {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if t, ok := f.tasks[parts[3]]; ok && t.ProjectID == parts[1] {
			_ = json.NewEncoder(w).Encode(f.wire(t))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	case r.Method == "POST" && path == "/task":
		var in ticktick.Task
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.nextID++
		in.ID = fmt.Sprintf("new%d", f.nextID)
		f.tasks[in.ID] = &in
		_ = json.NewEncoder(w).Encode(in)
	case r.Method == "POST" && len(parts) == 5 && parts[4] == "complete":
		if t, ok := f.tasks[parts[3]]; ok {
			t.Status, t.CompletedTime = 2, time.Now().Add(-time.Minute).UTC().Format(ticktick.TimeLayout)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	case r.Method == "POST" && len(parts) == 2 && parts[0] == "task" && parts[1] != "completed":
		raw, _ := io.ReadAll(r.Body)
		var in ticktick.Task
		_ = json.Unmarshal(raw, &in)
		var all map[string]any
		_ = json.Unmarshal(raw, &all)
		t, ok := f.tasks[parts[1]]
		if !ok || t.ProjectID != in.ProjectID {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// TickTick may replace the task: a field that the body lacks is gone.
		t.Title, t.Content, t.Status, t.DueDate, t.StartDate = in.Title, in.Content, in.Status, in.DueDate, in.StartDate
		t.CompletedTime, t.TimeZone, t.IsAllDay = in.CompletedTime, in.TimeZone, in.IsAllDay
		extra := map[string]any{}
		for k, v := range all {
			if !knownKeys[k] {
				extra[k] = v
			}
		}
		f.extra[t.ID] = extra
		_ = json.NewEncoder(w).Encode(f.wire(t))
	case r.Method == "DELETE" && len(parts) == 4 && parts[2] == "task":
		if t, ok := f.tasks[parts[3]]; !ok || t.ProjectID != parts[1] {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		delete(f.tasks, parts[3])
	case r.Method == "POST" && path == "/task/completed":
		if f.noCompleted {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		done := []map[string]any{}
		for _, t := range f.tasks {
			if t.Status == 2 {
				done = append(done, f.wire(t))
			}
		}
		_ = json.NewEncoder(w).Encode(done)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeTick) add(t ticktick.Task) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t.TimeZone = "UTC" // the day of a completion is then the same in the fake and in the test
	f.tasks[t.ID] = &t
}

// keep sets fields of a task that Task does not know.
func (f *fakeTick) keep(id string, fields map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.extra[id] = fields
}

func (f *fakeTick) raw(id string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.wire(f.tasks[id])
}

func (f *fakeTick) complete(id string, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.tasks[id]
	t.Status, t.CompletedTime = 2, at.UTC().Format(ticktick.TimeLayout)
}

func (f *fakeTick) byTitle(title string) (ticktick.Task, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tasks {
		if t.Title == title {
			return *t, true
		}
	}
	return ticktick.Task{}, false
}

// bins is the folder with aminte and aminte-ticktick.
type bins struct{ dir, aminte string }

func build(t *testing.T) bins {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go command to build aminte: %v", err)
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "cmd", "aminte")); err != nil {
		t.Skipf("the aminte sources are not at %s: %v", root, err)
	}
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	b := bins{dir: t.TempDir()}
	b.aminte = filepath.Join(b.dir, "aminte"+exe)
	for _, args := range [][]string{
		{"build", "-C", root, "-o", b.aminte, "./cmd/aminte"},
		{"build", "-o", filepath.Join(b.dir, "aminte-ticktick"+exe), "./cmd/aminte-ticktick"},
	} {
		if out, err := exec.Command(goBin, args...).CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	return b
}

// server is a running `aminte serve --with ticktick`.
type server struct {
	t      *testing.T
	b      bins
	cmd    *exec.Cmd
	stderr *lockedBuffer
	exited chan error
	base   string
}

// startServer writes the files of the state folder (the config that points at the fake, and the token), then runs the
// server on a new life.db. XDG_CONFIG_HOME puts the state folder in a temporary folder: the owner's folder stays alone.
func startServer(t *testing.T, b bins, fake *httptest.Server) *server {
	t.Helper()
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("AMINTE_TOKEN", "")
	// The children read the day of a completion in UTC, the zone of the tasks of the fake, so dayOf names the same
	// day on a machine in any zone. Without it the test fails where the local day is not the day in UTC.
	t.Setenv("TZ", "UTC")
	state := filepath.Join(config, "aminte-ticktick")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`{"api": %q, "poll": "1h"}`, fake.URL+"/open/v1")
	for name, content := range map[string]string{"config.json": cfg, "token.json": `{"access_token": "test-token"}`} {
		if err := os.WriteFile(filepath.Join(state, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	db := filepath.Join(t.TempDir(), "life.db")
	if out, err := exec.Command(b.aminte, "init", db).CombinedOutput(); err != nil {
		t.Fatalf("aminte init: %v\n%s", err, out)
	}
	s := &server{t: t, b: b, stderr: new(lockedBuffer), exited: make(chan error, 1)}
	s.cmd = exec.Command(b.aminte, "--db", db, "serve", "--addr", "127.0.0.1:0", "--with", "ticktick")
	s.cmd.Stderr = s.stderr
	if err := s.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { s.exited <- s.cmd.Wait() }()
	t.Cleanup(s.stop)
	line := s.waitFor("aminte: serving ")
	_, rest, _ := strings.Cut(line, " on http://")
	addr, _, _ := strings.Cut(rest, " ")
	s.base = "http://" + addr
	return s
}

// waitFor waits until the stderr holds the text and returns the line that holds it. It reads the log, never the clock.
func (s *server) waitFor(text string) string {
	s.t.Helper()
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		for _, l := range strings.Split(s.stderr.String(), "\n") {
			if strings.Contains(l, text) {
				return l
			}
		}
		select {
		case err := <-s.exited:
			s.exited <- err
			s.t.Fatalf("the server ended before %q: %v\n%s", text, err, s.stderr)
		case <-time.After(20 * time.Millisecond):
		}
	}
	s.t.Fatalf("no %q in the log after %s:\n%s", text, wait, s.stderr)
	return ""
}

func (s *server) stop() {
	select {
	case err := <-s.exited:
		s.exited <- err
		return
	default:
	}
	_ = s.cmd.Process.Signal(syscall.SIGINT)
	select {
	case <-s.exited:
	case <-time.After(wait):
		_ = s.cmd.Process.Kill()
		s.t.Error("the server did not stop")
	}
}

func (s *server) client() *client.Client { return client.Remote(s.base, "", "") }

// do runs the real command line: `aminte do ACTION ARGS... --url BASE`.
func (s *server) do(args ...string) string {
	s.t.Helper()
	out, err := exec.Command(s.b.aminte, append(append([]string{"do"}, args...), "--url", s.base)...).CombinedOutput()
	if err != nil {
		s.t.Fatalf("aminte do %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// act runs an action of the merged catalog through the client.
func (s *server) act(name string, values map[string]string) *siren.Entity {
	s.t.Helper()
	actions, err := s.client().Catalog(s.t.Context())
	if err != nil {
		s.t.Fatal(err)
	}
	for _, a := range actions {
		if a.Name == name {
			e, err := s.client().Do(s.t.Context(), a, values)
			if err != nil {
				s.t.Fatalf("%s: %v", name, err)
			}
			return e
		}
	}
	s.t.Fatalf("the catalog has no action %s", name)
	return nil
}

// written is the count that sync-tasks answered.
func (s *server) written(e *siren.Entity) string {
	s.t.Helper()
	p, _ := e.Properties.(map[string]any)
	n, ok := p["written"].(json.Number)
	if !ok {
		s.t.Fatalf("the answer of sync-tasks has no written count: %+v", e)
	}
	return n.String()
}

// body is the body of the day page of the day, "" when there is none.
func (s *server) body(day string) string {
	s.t.Helper()
	e, err := s.client().Get(s.t.Context(), "/days/"+day)
	if err != nil {
		s.t.Fatal(err)
	}
	p, _ := e.Properties.(map[string]any)
	view, _ := p["view"].([]any)
	for _, row := range view {
		r, _ := row.(map[string]any)
		if r["what"] == "day page" {
			body, _ := r["detail"].(string)
			return body
		}
	}
	return ""
}

func dayOf(at time.Time) string { return at.UTC().Format("2006-01-02") }

func TestTickTickOnTheRealAminte(t *testing.T) {
	if testing.Short() {
		t.Skip("the end-to-end test builds aminte; -short skips it")
	}
	b := build(t)
	for _, tc := range []struct {
		name        string
		noCompleted bool
	}{{"with /task/completed", false}, {"vanish and fetch", true}} {
		t.Run(tc.name, func(t *testing.T) { holdsTheWholeLine(t, b, tc.noCompleted) })
	}
	t.Run("a refused token ends the app as the owner acts", func(t *testing.T) {
		fake := &fakeTick{extra: map[string]map[string]any{}, tasks: map[string]*ticktick.Task{}, unauthorized: true}
		srv := httptest.NewServer(fake)
		defer srv.Close()
		s := startServer(t, b, srv)
		s.waitFor("with ticktick: ended with status 78: not started again")
		if !strings.Contains(s.stderr.String(), "login again: aminte ticktick login") {
			t.Errorf("the log has no line that tells the owner to log in:\n%s", s.stderr)
		}
	})
}

func holdsTheWholeLine(t *testing.T, b bins, noCompleted bool) {
	now := time.Now()
	today := now.Format("2006-01-02") // the day that the app calls today
	fake := &fakeTick{extra: map[string]map[string]any{}, tasks: map[string]*ticktick.Task{}, noCompleted: noCompleted}
	fake.add(ticktick.Task{ID: "t-rent", ProjectID: "p-home", Title: "Pay rent"})
	fake.add(ticktick.Task{ID: "t-mom", ProjectID: "p-home", Title: "Call mom"})
	fake.add(ticktick.Task{ID: "t-report", ProjectID: "p-work", Title: "Write report", DueDate: today + "T00:00:00.000+0000"})
	wantFirst := "poll: wrote 0 entries"
	passportAt := now.Add(-time.Hour)
	if !noCompleted {
		// The server knows /task/completed, so a task done before the start is found by the first poll.
		fake.add(ticktick.Task{ID: "t-passport", ProjectID: "inbox1", Title: "Renew passport"})
		fake.complete("t-passport", passportAt)
		wantFirst = "poll: wrote 1 entries"
	}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	s := startServer(t, b, srv)
	s.waitFor("with ticktick: the task service, 2 lists")
	s.waitFor(wantFirst)
	if noCompleted {
		s.waitFor("the server has no /task/completed")
	}

	t.Run("the catalog has the actions of Tasks and none with the name of the service", func(t *testing.T) {
		actions, err := s.client().Catalog(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, a := range actions {
			got[a.Name] = a.Href
			if want := map[string]string{"add-task": "create", "complete-task": "toggle"}[a.Name]; want != "" && !slices.Equal(a.Class, []string{want}) {
				t.Errorf("the catalog of aminte: the class of %s = %v, want [%s]", a.Name, a.Class, want)
			}
			if strings.HasPrefix(a.Name, "ticktick-") {
				t.Errorf("the catalog has the action %s: the service has no prefix", a.Name)
			}
		}
		for name, href := range map[string]string{"add-task": "/tasks", "edit-task": "/tasks/items/{id}",
			"complete-task": "/tasks/items/{id}/complete", "reopen-task": "/tasks/items/{id}/reopen",
			"delete-task": "/tasks/items/{id}/delete", "sync-tasks": "/tasks/sync"} {
			if got[name] != href {
				t.Errorf("%s = %q, want %q", name, got[name], href)
			}
		}
	})

	if !noCompleted {
		t.Run("a task done before the start is an entry of its day, from the Inbox", func(t *testing.T) {
			body := s.body(dayOf(passportAt))
			if !regexp.MustCompile(`\(\d\d:\d\d, via ticktick\) Done: Renew passport(\n|$)`).MatchString(body) {
				t.Errorf("body of %s = %q, want an entry Done: Renew passport with no list", dayOf(passportAt), body)
			}
		})
	}

	doneAt := time.Now().Add(-5 * time.Minute)
	fake.complete("t-rent", doneAt)
	day := dayOf(doneAt)
	entry := regexp.MustCompile(`\(\d\d:\d\d, via ticktick\) Done: Pay rent · Home`)
	var afterFirst string
	t.Run("a task done in TickTick is an entry of its day, once", func(t *testing.T) {
		if n := s.written(s.act("sync-tasks", nil)); n != "1" {
			t.Errorf("the sync wrote %s entries, want 1", n)
		}
		afterFirst = s.body(day)
		if !entry.MatchString(afterFirst) {
			t.Errorf("body of %s = %q, want %v", day, afterFirst, entry)
		}
		if n := strings.Count(afterFirst, "Done: Pay rent"); n != 1 {
			t.Errorf("the entry is in the body %d times, want 1", n)
		}
	})

	t.Run("a second sync writes nothing", func(t *testing.T) {
		if n := s.written(s.act("sync-tasks", nil)); n != "0" {
			t.Errorf("the second sync wrote %s entries, want 0", n)
		}
		if got := s.body(day); got != afterFirst {
			t.Errorf("the body changed:\n%q\nwas\n%q", got, afterFirst)
		}
	})

	t.Run("aminte do add-task makes the task in the list", func(t *testing.T) {
		s.do("add-task", "title=Milk", "list=Home")
		milk, ok := fake.byTitle("Milk")
		if !ok || milk.ProjectID != "p-home" {
			t.Fatalf("task in TickTick = %+v, %v, want Milk in p-home", milk, ok)
		}
	})

	t.Run("complete-task completes it and the next sync writes its entry", func(t *testing.T) {
		s.act("sync-tasks", nil) // the way that has no /task/completed sees a task open before it sees it done
		milk, _ := fake.byTitle("Milk")
		s.act("complete-task", map[string]string{"id": milk.ID})
		if done, _ := fake.byTitle("Milk"); done.Status != 2 {
			t.Fatalf("Milk in TickTick has status %d, want 2", done.Status)
		}
		if n := s.written(s.act("sync-tasks", nil)); n != "1" {
			t.Errorf("the sync wrote %s entries, want 1", n)
		}
		completed, _ := time.Parse(ticktick.TimeLayout, func() string { m, _ := fake.byTitle("Milk"); return m.CompletedTime }())
		if body := s.body(dayOf(completed)); !strings.Contains(body, "Done: Milk · Home") {
			t.Errorf("body = %q, want Done: Milk · Home", body)
		}
	})

	t.Run("Today lists the task due today as a Siren entity, and it does not name the service", func(t *testing.T) {
		res, page := get(t, s, "/tasks")
		if res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/vnd.siren+json" {
			t.Fatalf("status %d, type %q, want 200 and Siren JSON", res.StatusCode, res.Header.Get("Content-Type"))
		}
		var today siren.Entity
		if err := json.Unmarshal([]byte(page), &today); err != nil {
			t.Fatalf("the answer is no entity: %v\n%s", err, page)
		}
		titles := []string{}
		for _, l := range today.Entities {
			titles = append(titles, l.Title)
		}
		if !slices.Contains(titles, "Write report") {
			t.Errorf("the rows of Today are %v, want Write report among them", titles)
		}
		if strings.Contains(strings.ToLower(page), "ticktick") {
			t.Errorf("the answer names the service:\n%s", page)
		}
	})

	t.Run("a list has its tasks as links with the Complete action of each, and it does not name the service", func(t *testing.T) {
		res, page := get(t, s, "/tasks/lists/p-home")
		if res.StatusCode != 200 {
			t.Fatalf("status %d, want 200", res.StatusCode)
		}
		var list siren.Entity
		if err := json.Unmarshal([]byte(page), &list); err != nil {
			t.Fatalf("the answer is no entity: %v\n%s", err, page)
		}
		var mom *siren.Link
		for i, l := range list.Entities {
			if l.Href == "/tasks/items/t-mom" {
				mom = &list.Entities[i]
			}
		}
		if mom == nil || mom.Title != "Call mom" {
			t.Fatalf("the list has no link to /tasks/items/t-mom with the title Call mom:\n%s", page)
		}
		if len(mom.Actions) != 1 || mom.Actions[0].Name != "complete-task" || mom.Actions[0].Method != "POST" || mom.Actions[0].Href != "/tasks/items/t-mom/complete" {
			t.Fatalf("the actions of the row are %+v, want complete-task, POST /tasks/items/t-mom/complete", mom.Actions)
		}
		if strings.Contains(strings.ToLower(page), "ticktick") {
			t.Errorf("the answer names the service:\n%s", page)
		}

		// The owner posts the action of the row: the core answers the entity with 200, and TickTick has the task done.
		done, _ := post(t, s, "/tasks/items/t-mom/complete")
		if done.StatusCode != http.StatusOK {
			t.Errorf("the action answered %d, want 200", done.StatusCode)
		}
		if mom, _ := fake.byTitle("Call mom"); mom.Status != 2 {
			t.Errorf("Call mom in TickTick has status %d, want 2", mom.Status)
		}
	})

	t.Run("the service has no page of its own", func(t *testing.T) {
		if res, _ := get(t, s, "/with/ticktick/"); res.StatusCode != http.StatusNotFound {
			t.Errorf("GET /with/ticktick/ = %d, want 404", res.StatusCode)
		}
	})

	t.Run("edit, reopen and delete reach TickTick", func(t *testing.T) {
		fake.keep("t-report", map[string]any{"priority": 5.0, "tags": []any{"work"}})
		s.act("edit-task", map[string]string{"id": "t-report", "title": "Write the report"})
		if r, ok := fake.byTitle("Write the report"); !ok || r.DueDate != today+"T00:00:00.000+0000" {
			t.Errorf("task in TickTick = %+v, %v, want the new title and the old due day", r, ok)
		}
		if raw := fake.raw("t-report"); raw["priority"] != 5.0 || raw["tags"] == nil {
			t.Errorf("task in TickTick = %v, want the priority and the tags kept by the edit", raw)
		}
		s.act("reopen-task", map[string]string{"id": "t-mom"})
		if mom, _ := fake.byTitle("Call mom"); mom.Status != 0 || mom.CompletedTime != "" {
			t.Errorf("Call mom in TickTick = %+v, want it open", mom)
		}
		s.act("delete-task", map[string]string{"id": "t-report"})
		if _, ok := fake.byTitle("Write the report"); ok {
			t.Error("the task is still in TickTick")
		}
	})

	t.Run("a task that TickTick does not know is not found", func(t *testing.T) {
		actions, _ := s.client().Catalog(t.Context())
		found := false
		for _, a := range actions {
			if a.Name != "complete-task" {
				continue
			}
			found = true
			_, err := s.client().Do(t.Context(), a, map[string]string{"id": "nope"})
			if ce, ok := err.(*client.Error); !ok || ce.Status != 404 || ce.Code() != "not_found" {
				t.Errorf("err = %v, want 404 not_found", err)
			}
		}
		if !found {
			t.Fatal("the catalog has no action complete-task")
		}
	})

	t.Run("an error does not name the task manager", func(t *testing.T) {
		fake.mu.Lock()
		fake.failRead = true
		fake.mu.Unlock()
		defer func() {
			fake.mu.Lock()
			fake.failRead = false
			fake.mu.Unlock()
		}()
		// The service learned t-mom from the page of the list above, so the read of the task is the call that fails.
		res, page := post(t, s, "/tasks/items/t-mom/reopen")
		if res.StatusCode < 400 {
			t.Fatalf("status %d, want an error", res.StatusCode)
		}
		if !strings.Contains(page, "the task manager answered 500") {
			t.Errorf("the error lacks the words of the status:\n%s", page)
		}
		if strings.Contains(strings.ToLower(page), "ticktick") {
			t.Errorf("the error names the task manager:\n%s", page)
		}
	})
}

// get reads a resource as a Siren client does and returns the response and its body.
func get(t *testing.T, s *server, path string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), "GET", s.base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "application/vnd.siren+json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res, string(raw)
}

// post sends an action as a Siren client does and returns the response and its body.
func post(t *testing.T, s *server, path string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), "POST", s.base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "application/vnd.siren+json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res, string(raw)
}

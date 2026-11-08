package connector

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"aminte-ticktick/ticktick"
	"aminte/client"
	"aminte/siren"
)

// fakeTick is a TickTick in memory: two lists, tasks, and switches for the missing /task/completed and for a 401.
type fakeTick struct {
	mu            sync.Mutex
	lists         []ticktick.Project
	tasks         map[string]*ticktick.Task
	noCompleted   bool
	unauthorized  bool
	calls         []string
	nextID        int
	slow          chan struct{} // when set, GET /project waits for a receive
	entered       chan struct{} // when set, GET /project signals here before it waits
	inFlight      int           // requests in progress now, and the most there were at once
	maxInFlight   int
	updates       []ticktick.Task           // the bodies of POST /task/{id}, as TickTick decoded them
	extra         map[string]map[string]any // the fields of a task that Task does not know: priority, tags, ...
	rawUpdates    []map[string]any          // the bodies of POST /task/{id}, as they were sent
	failRead      bool                      // when set, GET /project/{p}/task/{id} answers 500
	completedBody string                    // the body of the last POST /task/completed
}

func newFakeTick(t *testing.T) (*fakeTick, *ticktick.Client) {
	t.Helper()
	f := &fakeTick{
		lists: []ticktick.Project{{ID: "p-home", Name: "Home"}, {ID: "p-work", Name: "Work"}},
		tasks: map[string]*ticktick.Task{},
		extra: map[string]map[string]any{},
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, &ticktick.Client{Base: srv.URL + "/open/v1", Token: "tok"}
}

func (f *fakeTick) add(id, project, title string) *ticktick.Task {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &ticktick.Task{ID: id, ProjectID: project, Title: title, TimeZone: "UTC"}
	f.tasks[id] = t
	return t
}

// finish marks a task done at the instant.
func (f *fakeTick) finish(id string, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.tasks[id]
	t.Status, t.CompletedTime = 2, at.UTC().Format(ticktick.TimeLayout)
}

// wire is the task as TickTick sends it: the fields of Task and the extra fields.
func (f *fakeTick) wire(t *ticktick.Task) map[string]any {
	b, _ := json.Marshal(t)
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	for k, v := range f.extra[t.ID] {
		out[k] = v
	}
	return out
}

// keep sets fields of a task that Task does not know.
func (f *fakeTick) keep(id string, fields map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.extra[id] = fields
}

func (f *fakeTick) callsTo(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (f *fakeTick) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/open/v1")
	f.mu.Lock()
	f.calls = append(f.calls, r.Method+" "+path)
	bad, noCompleted, slow, entered := f.unauthorized, f.noCompleted, f.slow, f.entered
	f.inFlight++
	f.maxInFlight = max(f.maxInFlight, f.inFlight)
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	}()
	if bad || r.Header.Get("Authorization") != "Bearer tok" {
		w.WriteHeader(401)
		return
	}
	if path == "/project" && entered != nil {
		entered <- struct{}{}
		<-slow
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case r.Method == "GET" && path == "/project":
		_ = json.NewEncoder(w).Encode(f.lists)
	case r.Method == "GET" && len(parts) == 3 && parts[2] == "data":
		open := []map[string]any{}
		for _, t := range f.tasks {
			if t.ProjectID == parts[1] && t.Status == 0 {
				open = append(open, f.wire(t))
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"project": map[string]string{"id": parts[1]}, "tasks": open})
	case r.Method == "GET" && len(parts) == 4 && parts[2] == "task":
		t, ok := f.tasks[parts[3]]
		if f.failRead {
			w.WriteHeader(500)
			return
		}
		if !ok || t.ProjectID != parts[1] {
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(f.wire(t))
	case r.Method == "POST" && path == "/task":
		var in ticktick.Task
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.nextID++
		in.ID = "new" + string(rune('0'+f.nextID))
		f.tasks[in.ID] = &in
		_ = json.NewEncoder(w).Encode(in)
	case r.Method == "POST" && len(parts) == 5 && parts[4] == "complete":
		t, ok := f.tasks[parts[3]]
		if !ok {
			w.WriteHeader(404)
			return
		}
		t.Status, t.CompletedTime = 2, time.Now().UTC().Format(ticktick.TimeLayout)
	case r.Method == "POST" && path == "/task/completed":
		b, _ := io.ReadAll(r.Body)
		f.completedBody = string(b)
		if noCompleted {
			w.WriteHeader(404)
			return
		}
		done := []map[string]any{}
		for _, t := range f.tasks {
			if t.Status == 2 {
				done = append(done, f.wire(t))
			}
		}
		_ = json.NewEncoder(w).Encode(done)
	case r.Method == "POST" && len(parts) == 2 && parts[0] == "task":
		raw, _ := io.ReadAll(r.Body)
		var in ticktick.Task
		_ = json.Unmarshal(raw, &in)
		var all map[string]any
		_ = json.Unmarshal(raw, &all)
		t, ok := f.tasks[parts[1]]
		if !ok || t.ProjectID != in.ProjectID {
			w.WriteHeader(404)
			return
		}
		f.updates = append(f.updates, in)
		f.rawUpdates = append(f.rawUpdates, all)
		// TickTick may replace the task: a field that the body lacks is gone.
		t.Title, t.Content, t.Status, t.DueDate, t.StartDate, t.IsAllDay = in.Title, in.Content, in.Status, in.DueDate, in.StartDate, in.IsAllDay
		t.CompletedTime, t.TimeZone = in.CompletedTime, in.TimeZone
		extra := map[string]any{}
		for k, v := range all {
			switch k {
			case "id", "projectId", "title", "content", "status", "completedTime", "dueDate", "startDate", "timeZone", "isAllDay":
			default:
				extra[k] = v
			}
		}
		f.extra[t.ID] = extra
		_ = json.NewEncoder(w).Encode(f.wire(t))
	case r.Method == "DELETE" && len(parts) == 4 && parts[2] == "task":
		if t, ok := f.tasks[parts[3]]; !ok || t.ProjectID != parts[1] {
			w.WriteHeader(404)
			return
		}
		delete(f.tasks, parts[3])
	default:
		w.WriteHeader(404)
	}
}

// fakeLife is the part of aminte that Sync uses: GET /actions, and capture with its import_key.
type fakeLife struct {
	mu       sync.Mutex
	captures []capture
	keys     map[string]bool
	refuse   int // when not 0, capture answers this status
	source   string
}

type capture struct{ Day, Text, Key string }

func newFakeLife(t *testing.T) (*fakeLife, *client.Client) {
	t.Helper()
	f := &fakeLife{keys: map[string]bool{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, client.Remote(srv.URL, siren.ImportPrefix+"ticktick", "")
}

func (f *fakeLife) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.siren+json")
	switch {
	case r.URL.Path == "/actions":
		_ = json.NewEncoder(w).Encode(siren.Entity{Class: []string{"actions"}, Actions: []siren.Action{{
			Name: "capture", Method: "POST", Href: "/days/{day}/capture",
			Fields: []siren.Field{{Name: "day", Type: "date", In: "path"}, {Name: "text", Type: "textarea"}, {Name: "import_key", Type: "text"}}}}})
	case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/days/") && strings.HasSuffix(r.URL.Path, "/capture"):
		f.mu.Lock()
		defer f.mu.Unlock()
		f.source = r.Header.Get(siren.SourceHeader)
		if f.refuse != 0 {
			w.WriteHeader(f.refuse)
			_ = json.NewEncoder(w).Encode(siren.Entity{Class: []string{"error"}, Properties: map[string]any{"status": f.refuse, "code": "invalid", "message": "the words of the owner"}})
			return
		}
		_ = r.ParseForm()
		c := capture{Day: strings.Split(r.URL.Path, "/")[2], Text: r.PostForm.Get("text"), Key: r.PostForm.Get("import_key")}
		if f.keys[c.Key] {
			_ = json.NewEncoder(w).Encode(siren.Entity{Class: []string{"result"}, Title: "Already added", Result: map[string]any{"already": true}})
			return
		}
		f.keys[c.Key] = true
		f.captures = append(f.captures, c)
		_ = json.NewEncoder(w).Encode(siren.Entity{Class: []string{"day"}, Title: c.Day})
	default:
		w.WriteHeader(404)
	}
}

func (f *fakeLife) got() []capture {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]capture(nil), f.captures...)
}

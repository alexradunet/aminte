// Taskservice is the task service of the tests of cmd/aminte: a few lists and tasks in memory, served with
// aminte/app on the address that `aminte serve --with` gives in AMINTE_LISTEN. It answers the protocol of
// extensions/README.md, "A task service", and nothing more. It is also the example for the author of a service: a
// real one reads and writes its provider where this program reads and writes a slice. It names no third party.
//
// The days of the seed are relative to TASKSERVICE_TODAY (YYYY-MM-DD), which the test sets to the day of the clock
// of the server. A real service uses the clock of the zone of its tasks.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"

	"aminte/app"
	"aminte/siren"
)

// task is the shape of the protocol. The tags are the names on the wire.
type task struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	List    string `json:"list"`
	Notes   string `json:"notes,omitempty"`
	Due     string `json:"due,omitempty"`
	Done    bool   `json:"done"`
	DoneDay string `json:"done_day,omitempty"`
	DoneAt  string `json:"done_at,omitempty"`
	synced  bool
}

var (
	mu     sync.Mutex
	today  time.Time
	tasks  []*task // in the order of the service
	nextID = 100
	lists  = []map[string]string{{"id": "home", "name": "Home"}, {"id": "work", "name": "Work"}}
)

func day(offset int) string { return today.AddDate(0, 0, offset).Format(time.DateOnly) }

// seed: three open tasks of Home with due days around today, three of Work, one with no list and no day, and three
// done tasks, one of them older than the window of 30 days.
func seed() {
	tasks = []*task{
		{ID: "t1", Title: "Pay rent", List: "home", Due: day(-2)},
		{ID: "t2", Title: "Call mom", List: "home", Due: day(0), Notes: "about the visit"},
		{ID: "t3", Title: "Fix tap", List: "home", Due: day(-5)},
		{ID: "t4", Title: "Write report", List: "work", Due: day(3)},
		{ID: "t5", Title: "File taxes", List: "work", Due: day(3)},
		{ID: "t6", Title: "Plan year", List: "work", Due: day(10)},
		{ID: "t7", Title: "Buy milk", List: ""},
		{ID: "d1", Title: "Send invoice", List: "work", Done: true, DoneDay: day(-1), DoneAt: day(-1) + "T09:00:00Z"},
		{ID: "d2", Title: "Book dentist", List: "home", Done: true, DoneDay: day(0), DoneAt: day(0) + "T08:00:00Z"},
		{ID: "d3", Title: "Ancient chore", List: "home", Done: true, DoneDay: day(-40), DoneAt: day(-40) + "T08:00:00Z"},
	}
}

var actions = []siren.Action{
	{Name: "add-task", Method: "POST", Href: "/tasks", Title: "Add a task"},
	{Name: "edit-task", Method: "POST", Href: "/tasks/{id}", Title: "Edit a task",
		Fields: []siren.Field{{Name: "id", Type: "text", Required: true, In: "path"}}},
	{Name: "complete-task", Method: "POST", Href: "/tasks/{id}/complete", Title: "Complete a task",
		Fields: []siren.Field{{Name: "id", Type: "text", Required: true, In: "path"}}},
	{Name: "reopen-task", Method: "POST", Href: "/tasks/{id}/reopen", Title: "Reopen a task",
		Fields: []siren.Field{{Name: "id", Type: "text", Required: true, In: "path"}}},
	{Name: "delete-task", Method: "POST", Href: "/tasks/{id}/delete", Title: "Delete a task",
		Fields: []siren.Field{{Name: "id", Type: "text", Required: true, In: "path"}}},
	{Name: "sync", Method: "POST", Href: "/sync", Title: "Sync"},
}

func fail(status int, code, msg string) error {
	return &app.Error{Status: status, Code: code, Msg: msg}
}

func taskEntity(t *task) *siren.Entity {
	return &siren.Entity{Class: []string{"task"}, Title: t.Title, Properties: t}
}

func listEntity(ts []*task) *siren.Entity {
	if ts == nil {
		ts = []*task{}
	}
	return &siren.Entity{Class: []string{"tasks"}, Properties: map[string]any{"tasks": ts}}
}

func home(*http.Request) (*siren.Entity, error) {
	return &siren.Entity{Class: []string{"taskservice"}, Title: "Task service"}, nil
}

func showLists(*http.Request) (*siren.Entity, error) {
	return &siren.Entity{Class: []string{"lists"}, Properties: map[string]any{"lists": lists}}, nil
}

func open(r *http.Request) (*siren.Entity, error) {
	mu.Lock()
	defer mu.Unlock()
	var out []*task
	for _, t := range tasks {
		if !t.Done && (r.URL.Query().Get("list") == "" || t.List == r.URL.Query().Get("list")) {
			out = append(out, t)
		}
	}
	return listEntity(out), nil
}

func done(r *http.Request) (*siren.Entity, error) {
	since := r.URL.Query().Get("since")
	if _, err := time.Parse(time.DateOnly, since); err != nil {
		return nil, fail(http.StatusUnprocessableEntity, "invalid", "since must be a day, YYYY-MM-DD")
	}
	mu.Lock()
	defer mu.Unlock()
	var out []*task
	for _, t := range tasks {
		if t.Done && t.DoneDay >= since && (r.URL.Query().Get("list") == "" || t.List == r.URL.Query().Get("list")) {
			out = append(out, t)
		}
	}
	return listEntity(out), nil
}

// find is the task with the id of the path. The caller holds mu.
func find(r *http.Request) (*task, int, error) {
	for i, t := range tasks {
		if t.ID == r.PathValue("id") {
			return t, i, nil
		}
	}
	return nil, 0, fail(http.StatusNotFound, "not_found", "no such task")
}

func show(r *http.Request) (*siren.Entity, error) {
	mu.Lock()
	defer mu.Unlock()
	t, _, err := find(r)
	if err != nil {
		return nil, err
	}
	return taskEntity(t), nil
}

func checkDue(due string) error {
	if _, err := time.Parse(time.DateOnly, due); due != "" && err != nil {
		return fail(http.StatusUnprocessableEntity, "invalid", "due must be a day, YYYY-MM-DD")
	}
	return nil
}

func add(r *http.Request) (*siren.Entity, error) {
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		return nil, fail(http.StatusUnprocessableEntity, "invalid", "title is required")
	}
	if err := checkDue(r.FormValue("due")); err != nil {
		return nil, err
	}
	list := r.FormValue("list")
	known := list == ""
	for _, l := range lists {
		known = known || l["id"] == list
	}
	if !known {
		return nil, fail(http.StatusUnprocessableEntity, "invalid", "no such list")
	}
	mu.Lock()
	defer mu.Unlock()
	nextID++
	t := &task{ID: "n" + strconv.Itoa(nextID), Title: title, List: list, Notes: r.FormValue("notes"), Due: r.FormValue("due")}
	tasks = append(tasks, t)
	return taskEntity(t), nil
}

// edit changes the fields that the request names. A field that is not sent stays as it is; an empty due or notes
// clears it.
func edit(r *http.Request) (*siren.Entity, error) {
	if err := r.ParseForm(); err != nil {
		return nil, fail(http.StatusBadRequest, "bad_request", "the form cannot be read")
	}
	mu.Lock()
	defer mu.Unlock()
	t, _, err := find(r)
	if err != nil {
		return nil, err
	}
	if v, ok := r.PostForm["title"]; ok {
		if strings.TrimSpace(v[0]) == "" {
			return nil, fail(http.StatusUnprocessableEntity, "invalid", "title must not be blank")
		}
		t.Title = v[0]
	}
	if v, ok := r.PostForm["due"]; ok {
		if err := checkDue(v[0]); err != nil {
			return nil, err
		}
		t.Due = v[0]
	}
	if v, ok := r.PostForm["notes"]; ok {
		t.Notes = v[0]
	}
	return taskEntity(t), nil
}

func complete(r *http.Request) (*siren.Entity, error) {
	mu.Lock()
	defer mu.Unlock()
	t, _, err := find(r)
	if err != nil {
		return nil, err
	}
	now := today.Add(12 * time.Hour)
	t.Done, t.DoneDay, t.DoneAt = true, now.Format(time.DateOnly), now.Format(time.RFC3339)
	return taskEntity(t), nil
}

func reopen(r *http.Request) (*siren.Entity, error) {
	mu.Lock()
	defer mu.Unlock()
	t, _, err := find(r)
	if err != nil {
		return nil, err
	}
	t.Done, t.DoneDay, t.DoneAt, t.synced = false, "", "", false
	return taskEntity(t), nil
}

func remove(r *http.Request) (*siren.Entity, error) {
	mu.Lock()
	defer mu.Unlock()
	t, i, err := find(r)
	if err != nil {
		return nil, err
	}
	tasks = append(tasks[:i], tasks[i+1:]...)
	return taskEntity(t), nil
}

// syncNow stands for the write of a Done entry for each task that is done: it counts the done tasks that it did not
// count before.
func syncNow(*http.Request) (*siren.Entity, error) {
	mu.Lock()
	defer mu.Unlock()
	n := 0
	for _, t := range tasks {
		if t.Done && !t.synced {
			t.synced = true
			n++
		}
	}
	return &siren.Entity{Class: []string{"sync"}, Properties: map[string]any{"written": n}}, nil
}

func main() {
	var err error
	if today, err = time.Parse(time.DateOnly, os.Getenv("TASKSERVICE_TODAY")); err != nil {
		fmt.Fprintln(os.Stderr, "taskservice: TASKSERVICE_TODAY must be a day, YYYY-MM-DD:", err)
		os.Exit(1)
	}
	seed()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	inner := app.CatalogOf("tasks", "Task service", home, actions, map[string]func(*http.Request) (*siren.Entity, error){
		"add-task": add, "edit-task": edit, "complete-task": complete, "reopen-task": reopen, "delete-task": remove, "sync": syncNow,
	})
	// app.CatalogOf serves the actions and the home. The reads of the protocol are routes beside it.
	mux := http.NewServeMux()
	mux.Handle("/", inner)
	for pattern, f := range map[string]func(*http.Request) (*siren.Entity, error){
		"GET /lists": showLists, "GET /open": open, "GET /done": done, "GET /tasks/{id}": show,
	} {
		mux.HandleFunc(pattern, app.Entity(f))
	}
	if err := app.Listen(ctx, mux); err != nil {
		fmt.Fprintln(os.Stderr, "taskservice:", err)
		os.Exit(1)
	}
}

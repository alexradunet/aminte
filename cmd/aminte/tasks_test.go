package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"aminte/client"
	"aminte/siren"
)

// The task service of the tests is cmd/aminte/testdata/taskservice. Its clock is TASKSERVICE_TODAY and the clock of
// the server is withServer.start (2031-02-03), so the days of the seed are fixed:
//
//	open, in the order of the service: Pay rent (Home, 2031-02-01), Call mom (Home, 2031-02-03, notes "about the
//	visit"), Fix tap (Home, 2031-01-29), Write report (Work, 2031-02-06), File taxes (Work, 2031-02-06), Plan year
//	(Work, 2031-02-13), Buy milk (no list, no day).
//	done: Send invoice (Work, 2031-02-02 09:00Z), Book dentist (Home, 2031-02-03 08:00Z), Ancient chore (Home,
//	2030-12-25, older than the 30 days of Done).
const taskToday = "2031-02-03"

var (
	taskServiceOnce  sync.Once
	taskServiceBytes []byte
	taskServiceErr   error
)

// taskServiceAs builds testdata/taskservice once and writes a copy of it as aminte-NAME into dir.
func taskServiceAs(t *testing.T, dir, name string) {
	t.Helper()
	taskServiceOnce.Do(func() {
		tmp, err := os.MkdirTemp("", "taskservice")
		if err != nil {
			taskServiceErr = err
			return
		}
		defer os.RemoveAll(tmp)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		bin := filepath.Join(tmp, "taskservice")
		if out, err := exec.CommandContext(ctx, "go", "build", "-o", bin, "./testdata/taskservice").CombinedOutput(); err != nil {
			taskServiceErr = errors.New("build: " + err.Error() + "\n" + string(out))
			return
		}
		taskServiceBytes, taskServiceErr = os.ReadFile(bin)
	})
	if taskServiceErr != nil {
		t.Fatal(taskServiceErr)
	}
	path := filepath.Join(dir, "aminte-"+name)
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	if err := os.WriteFile(path, taskServiceBytes, 0o755); err != nil {
		t.Fatal(err)
	}
}

// startTaskServer starts `aminte serve --with NAME...`, each NAME a copy of the task service, and returns when the
// first of them said that it is the task service.
func startTaskServer(t *testing.T, names ...string) *withServer {
	t.Helper()
	w := newWithServer(t)
	t.Setenv("TASKSERVICE_TODAY", taskToday)
	argv := []string{"--db", w.db, "serve", "--addr", "127.0.0.1:0"}
	for _, name := range names {
		taskServiceAs(t, w.dir, name)
		argv = append(argv, "--with", name)
	}
	w.start(t, argv, nil)
	w.waitFor(t, ": the task service, 2 lists, on 127.0.0.1:")
	return w
}

// startNoTaskServer starts `aminte serve` with no child.
func startNoTaskServer(t *testing.T) *withServer {
	t.Helper()
	w := newWithServer(t)
	w.start(t, []string{"--db", w.db, "serve", "--addr", "127.0.0.1:0"}, nil)
	return w
}

// linksTasks says whether the root of the server links the task service (the link tasks, to /tasks): what a UI reads to
// draw Tasks in its rail.
func linksTasks(t *testing.T, w *withServer) bool {
	t.Helper()
	l := mustGet(t, w, "", "/").LinkByRel("tasks")
	return l != nil && l.Href == "/tasks"
}

// rowTitles are the titles of the rows (embedded links) of an entity, in order.
func rowTitles(e *siren.Entity) []string {
	var out []string
	for _, l := range e.Entities {
		out = append(out, l.Title)
	}
	return out
}

func mustGet(t *testing.T, w *withServer, source, href string) *siren.Entity {
	t.Helper()
	e, err := client.Remote(w.base, source, "").Get(t.Context(), href)
	if err != nil {
		t.Fatalf("GET %s: %v", href, err)
	}
	return e
}

// statusOf is the status of the error of a call, 0 for none, and its code.
func statusOf(err error) (int, string) {
	var ce *client.Error
	if errors.As(err, &ce) {
		return ce.Status, ce.Code()
	}
	return 0, ""
}

func chipsOf(l siren.Link) map[string]any {
	p, _ := l.Properties.(map[string]any)
	return p
}

func linkTitles(e *siren.Entity, rel string) []string {
	var out []string
	for _, l := range e.Links {
		if slices.Contains(l.Rel, rel) {
			out = append(out, l.Title)
		}
	}
	return out
}

func TestTheTaskServiceIsTasksAndNotAnApp(t *testing.T) {
	w := startTaskServer(t, "taskservice")
	got := catalogOf(t, w, "")
	for _, name := range []string{"add-task", "edit-task", "complete-task", "reopen-task", "delete-task", "sync-tasks"} {
		if _, ok := got[name]; !ok {
			t.Errorf("the catalog lacks %s", name)
		}
	}
	for name := range got {
		if strings.HasPrefix(name, "taskservice-") {
			t.Errorf("the catalog holds %s: a task service has no prefix", name)
		}
	}
	if !got["delete-task"].Owner || !got["delete-task"].Danger {
		t.Errorf("delete-task must be owner and danger: %+v", got["delete-task"])
	}
	if opts := fieldOf(got["add-task"], "list").Options; !slices.Equal(opts, []string{"Home", "Work"}) {
		t.Errorf("the options of list = %v, want [Home Work]", opts)
	}
	if a := got["edit-task"]; a.Href != "/tasks/items/{id}" {
		t.Errorf("edit-task href = %q", a.Href)
	}

	_, err := client.Remote(w.base, "", "").Get(t.Context(), "/with/taskservice/")
	if status, _ := statusOf(err); status != 404 {
		t.Errorf("/with/taskservice: got %v, want 404", err)
	}

	// The answer of /tasks names neither the service nor an app address, and the root links it.
	b, err := json.Marshal(mustGet(t, w, "", "/tasks"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "/with/") || strings.Contains(strings.ToLower(string(b)), "taskservice") {
		t.Errorf("the entity of Tasks names the service or an app address: %s", b)
	}
	if !linksTasks(t, w) {
		t.Errorf("the root does not link /tasks while the service runs")
	}
}

func TestThePagesOfTasksGiveTheRowsInOrder(t *testing.T) {
	w := startTaskServer(t, "taskservice")

	t.Run("Today: due today or before, by due day, with the counts", func(t *testing.T) {
		e := mustGet(t, w, "", "/tasks")
		if got, want := rowTitles(e), []string{"Fix tap", "Pay rent", "Call mom"}; !slices.Equal(got, want) {
			t.Fatalf("rows = %v, want %v", got, want)
		}
		first := e.Entities[0]
		if p := chipsOf(first); p["due"] != "2031-01-29" || p["list"] != "Home" {
			t.Errorf("chips of Fix tap = %v, want due 2031-01-29 and list Home", p)
		}
		if first.Href != "/tasks/items/t3" {
			t.Errorf("href = %q", first.Href)
		}
		if len(first.Actions) != 1 || first.Actions[0].Name != "complete-task" || first.Actions[0].Href != "/tasks/items/t3/complete" {
			t.Errorf("the action of the row = %+v, want complete-task at /tasks/items/t3/complete", first.Actions)
		}
		p, _ := e.Properties.(map[string]any)
		if p["open"].(interface{ String() string }).String() != "7" || p["overdue"].(interface{ String() string }).String() != "2" {
			t.Errorf("properties = %v, want open 7 and overdue 2", p)
		}
		if got, want := linkTitles(e, "nav"), []string{"Today", "Next 7 days", "Home · 3", "Work · 3"}; !slices.Equal(got, want) {
			t.Errorf("the side column = %v, want %v", got, want)
		}
		names := map[string]siren.Action{}
		for _, a := range e.Actions {
			names[a.Name] = a
		}
		if v := fieldOf(names["add-task"], "due").Value; v != taskToday {
			t.Errorf("add-task due = %v, want %s", v, taskToday)
		}
		if _, ok := names["sync-tasks"]; !ok {
			t.Error("Today has no sync-tasks")
		}
	})

	t.Run("Next 7 days: from today to today + 6, ties in the order of the service", func(t *testing.T) {
		e := mustGet(t, w, "", "/tasks/week")
		if got, want := rowTitles(e), []string{"Call mom", "Write report", "File taxes"}; !slices.Equal(got, want) {
			t.Errorf("rows = %v, want %v", got, want)
		}
	})

	t.Run("a list: its open tasks, no list chip, add-task with the list", func(t *testing.T) {
		e := mustGet(t, w, "", "/tasks/lists/home")
		if got, want := rowTitles(e), []string{"Fix tap", "Pay rent", "Call mom"}; !slices.Equal(got, want) {
			t.Fatalf("rows = %v, want %v", got, want)
		}
		if _, has := chipsOf(e.Entities[0])["list"]; has {
			t.Error("a page of one list has a list chip")
		}
		if v := fieldOf(e.Actions[0], "list").Value; e.Actions[0].Name != "add-task" || v != "Home" {
			t.Errorf("action = %s list %v, want add-task with list Home", e.Actions[0].Name, v)
		}
		if got := linkTitles(e, "done"); !slices.Contains(got, "Done in this list") {
			t.Errorf("done links = %v, want Done in this list", got)
		}
		e = mustGet(t, w, "", "/tasks/lists/work")
		if got, want := rowTitles(e), []string{"Write report", "File taxes", "Plan year"}; !slices.Equal(got, want) {
			t.Errorf("rows of Work = %v, want %v", got, want)
		}
	})

	t.Run("Done: the last 30 days, newest first, with Reopen", func(t *testing.T) {
		e := mustGet(t, w, "", "/tasks/done")
		if got, want := rowTitles(e), []string{"Book dentist", "Send invoice"}; !slices.Equal(got, want) {
			t.Fatalf("rows = %v, want %v", got, want)
		}
		if p := chipsOf(e.Entities[0]); p["done"] != taskToday || p["list"] != "Home" {
			t.Errorf("chips = %v, want done %s and list Home", p, taskToday)
		}
		if a := e.Entities[0].Actions; len(a) != 1 || a[0].Name != "reopen-task" || a[0].Href != "/tasks/items/d2/reopen" {
			t.Errorf("the action of the row = %+v, want reopen-task", a)
		}
		e = mustGet(t, w, "", "/tasks/lists/work/done")
		if got, want := rowTitles(e), []string{"Send invoice"}; !slices.Equal(got, want) {
			t.Errorf("rows of Work = %v, want %v", got, want)
		}
		if e.Title != "Done · Work" {
			t.Errorf("title = %q", e.Title)
		}
	})

	t.Run("a task: its properties, Edit filled in and Delete", func(t *testing.T) {
		e := mustGet(t, w, "", "/tasks/items/t2")
		p, _ := e.Properties.(map[string]any)
		if p["title"] != "Call mom" || p["notes"] != "about the visit" || p["due"] != taskToday || p["list"] != "Home" {
			t.Errorf("properties = %v", p)
		}
		if len(e.Actions) != 2 || e.Actions[0].Name != "edit-task" || e.Actions[0].Href != "/tasks/items/t2" || e.Actions[1].Name != "delete-task" {
			t.Fatalf("actions = %+v, want edit-task then delete-task", e.Actions)
		}
		if v := fieldOf(e.Actions[0], "notes").Value; v != "about the visit" {
			t.Errorf("the notes of the form = %v", v)
		}
		for _, href := range []string{"/tasks/items/nosuch", "/tasks/lists/nosuch", "/tasks/lists/nosuch/done"} {
			_, err := client.Remote(w.base, "", "").Get(t.Context(), href)
			if status, code := statusOf(err); status != 404 || code != "not_found" {
				t.Errorf("GET %s: got %v, want 404 not_found", href, err)
			}
		}
	})
}

func TestTheActionsOfTasksReachTheService(t *testing.T) {
	w := startTaskServer(t, "taskservice")
	ctx := t.Context()
	owner := client.Remote(w.base, "", "")
	actions := catalogOf(t, w, "")
	syncNow := func(t *testing.T) int {
		t.Helper()
		e, err := owner.Do(ctx, actions["sync-tasks"], nil)
		if err != nil {
			t.Fatalf("sync-tasks: %v", err)
		}
		p, _ := e.Properties.(map[string]any)
		n, _ := p["written"].(interface{ Int64() (int64, error) }).Int64()
		return int(n)
	}

	t.Run("sync-tasks writes the done tasks once", func(t *testing.T) {
		// Three tasks are done in the seed. The service counts each once.
		if got := syncNow(t); got != 3 {
			t.Errorf("first sync wrote %d, want 3", got)
		}
		if got := syncNow(t); got != 0 {
			t.Errorf("second sync wrote %d, want 0", got)
		}
	})

	t.Run("complete and reopen change the task and the pages", func(t *testing.T) {
		e, err := owner.Do(ctx, actions["complete-task"], map[string]string{"id": "t2"})
		if err != nil {
			t.Fatal(err)
		}
		if p, _ := e.Properties.(map[string]any); p["done"] != true || p["done_day"] != taskToday {
			t.Errorf("the answer = %v, want a done task of %s", p, taskToday)
		}
		if got, want := rowTitles(mustGet(t, w, "", "/tasks")), []string{"Fix tap", "Pay rent"}; !slices.Equal(got, want) {
			t.Errorf("Today after complete = %v, want %v", got, want)
		}
		// Call mom is done at 12:00Z of today, after Book dentist at 08:00Z.
		if got, want := rowTitles(mustGet(t, w, "", "/tasks/done")), []string{"Call mom", "Book dentist", "Send invoice"}; !slices.Equal(got, want) {
			t.Errorf("Done after complete = %v, want %v", got, want)
		}
		if got := syncNow(t); got != 1 {
			t.Errorf("sync after complete wrote %d, want 1", got)
		}
		if _, err := owner.Do(ctx, actions["reopen-task"], map[string]string{"id": "t2"}); err != nil {
			t.Fatal(err)
		}
		if got, want := rowTitles(mustGet(t, w, "", "/tasks")), []string{"Fix tap", "Pay rent", "Call mom"}; !slices.Equal(got, want) {
			t.Errorf("Today after reopen = %v, want %v", got, want)
		}
		if got, want := rowTitles(mustGet(t, w, "", "/tasks/done")), []string{"Book dentist", "Send invoice"}; !slices.Equal(got, want) {
			t.Errorf("Done after reopen = %v, want %v", got, want)
		}
	})

	t.Run("edit sends the fields that the request names, and the others stay", func(t *testing.T) {
		if _, err := owner.Do(ctx, actions["edit-task"], map[string]string{"id": "t2", "title": "Call mum"}); err != nil {
			t.Fatal(err)
		}
		p, _ := mustGet(t, w, "", "/tasks/items/t2").Properties.(map[string]any)
		if p["title"] != "Call mum" || p["notes"] != "about the visit" || p["due"] != taskToday {
			t.Errorf("after a title only: %v, want the notes and the day kept", p)
		}
		if _, err := owner.Do(ctx, actions["edit-task"], map[string]string{"id": "t2", "due": "", "notes": ""}); err != nil {
			t.Fatal(err)
		}
		p, _ = mustGet(t, w, "", "/tasks/items/t2").Properties.(map[string]any)
		if _, has := p["due"]; has || p["title"] != "Call mum" {
			t.Errorf("after an empty due and notes: %v, want them cleared and the title kept", p)
		}
		if _, has := p["notes"]; has {
			t.Errorf("the notes are not cleared: %v", p)
		}
		for name, values := range map[string]map[string]string{
			"a blank title": {"id": "t2", "title": "  "}, "a bad day": {"id": "t2", "due": "tomorrow"},
			"a field the action does not list": {"id": "t2", "priority": "high"},
		} {
			_, err := owner.Do(ctx, actions["edit-task"], values)
			if status, code := statusOf(err); status != 422 || code != "invalid" {
				t.Errorf("%s: got %v, want 422 invalid", name, err)
			}
		}
		if _, err := owner.Do(ctx, actions["edit-task"], map[string]string{"id": "nosuch", "title": "x"}); err == nil {
			t.Error("edit of an unknown task gave no error")
		} else if status, code := statusOf(err); status != 404 || code != "not_found" {
			t.Errorf("edit of an unknown task: got %v, want 404 not_found", err)
		}
	})

	t.Run("add-task maps the name of the list to its id and refuses what it cannot take", func(t *testing.T) {
		e, stdout, _ := testEnv(t)
		err := runArgs(ctx, e, []string{"do", "add-task", "title=Water plants", "list=Work", "due=2031-02-04", "notes=twice", "--url", w.base})
		if err != nil {
			t.Fatalf("do add-task: %v", err)
		}
		if !strings.Contains(stdout.String(), "/tasks/items/n101") {
			t.Fatalf("the output does not name /tasks/items/n101:\n%s", stdout)
		}
		page := mustGet(t, w, "", "/tasks/items/n101")
		p, _ := page.Properties.(map[string]any)
		if p["title"] != "Water plants" || p["list"] != "Work" || p["due"] != "2031-02-04" || p["notes"] != "twice" {
			t.Errorf("the new task = %v", p)
		}
		if got := rowTitles(mustGet(t, w, "", "/tasks/week")); !slices.Contains(got, "Water plants") {
			t.Errorf("Next 7 days = %v, want Water plants in it", got)
		}
		if _, err := owner.Do(ctx, actions["add-task"], map[string]string{"title": "No list"}); err != nil {
			t.Errorf("add-task with no list: %v", err)
		}
		for name, values := range map[string]map[string]string{
			"an unknown list": {"title": "x", "list": "Nowhere"}, "a bad day": {"title": "x", "due": "31-02-2031"},
			"no title": {"list": "Home"}, "a field the action does not list": {"title": "x", "priority": "high"},
		} {
			_, err := owner.Do(ctx, actions["add-task"], values)
			if status, code := statusOf(err); status != 422 || code != "invalid" {
				t.Errorf("%s: got %v, want 422 invalid", name, err)
			}
		}
	})

	t.Run("delete is the owner's: an agent is refused and the task stays", func(t *testing.T) {
		agent := client.Remote(w.base, "agent:x", "")
		_, err := agent.Do(ctx, actions["delete-task"], map[string]string{"id": "t7"})
		if status, code := statusOf(err); status != 403 || code != "forbidden" {
			t.Errorf("agent: got %v, want 403 forbidden", err)
		}
		if p, _ := mustGet(t, w, "", "/tasks/items/t7").Properties.(map[string]any); p["title"] != "Buy milk" {
			t.Errorf("the task changed after a refused delete: %v", p)
		}
		// An agent may complete: the action is not the owner's.
		if _, err := agent.Do(ctx, actions["complete-task"], map[string]string{"id": "t7"}); err != nil {
			t.Errorf("agent complete: %v", err)
		}
		if _, err := owner.Do(ctx, actions["delete-task"], map[string]string{"id": "t7"}); err != nil {
			t.Fatalf("owner delete: %v", err)
		}
		_, err = owner.Get(ctx, "/tasks/items/t7")
		if status, _ := statusOf(err); status != 404 {
			t.Errorf("after delete: got %v, want 404", err)
		}
	})
}

func TestASecondTaskServiceIsRefused(t *testing.T) {
	w := startTaskServer(t, "first", "second")
	// Both start at once; the one that registers first is the service, and the other is refused.
	w.waitFor(t, "catalog refused: a task service runs already: ")
	log := w.stderr.String()
	if n := strings.Count(log, ": the task service, 2 lists, on "); n != 1 {
		t.Errorf("%d children are the task service, want 1\n%s", n, log)
	}
	for _, name := range []string{"first", "second"} {
		if _, err := client.Remote(w.base, "", "").Get(t.Context(), "/with/"+name+"/"); err == nil {
			t.Errorf("/with/%s answers: a task service is not an app", name)
		}
	}
	if _, ok := catalogOf(t, w, "")["add-task"]; !ok {
		t.Error("the catalog lost add-task")
	}
}

func TestWithNoTaskServiceTasksIsUnavailable(t *testing.T) {
	w := startNoTaskServer(t)
	ctx := t.Context()
	for _, href := range []string{"/tasks", "/tasks/week", "/tasks/done", "/tasks/lists/x", "/tasks/items/x"} {
		_, err := client.Remote(w.base, "", "").Get(ctx, href)
		if status, code := statusOf(err); status != 503 || code != "unavailable" {
			t.Errorf("GET %s: got %v, want 503 unavailable", href, err)
		}
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", w.base+"/tasks", strings.NewReader("title=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/vnd.siren+json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 503 || !strings.Contains(string(b), "no task service runs") {
		t.Errorf("POST /tasks: status %d, body %s, want 503 no task service runs", res.StatusCode, b)
	}
	for name := range catalogOf(t, w, "") {
		if strings.HasSuffix(name, "-task") || name == "sync-tasks" {
			t.Errorf("the catalog holds %s with no task service", name)
		}
	}
	if linksTasks(t, w) {
		t.Error("the root links Tasks with no task service")
	}
}

// fieldOf is the field of the action with that name; the zero field when the action has none.
func fieldOf(a siren.Action, name string) siren.Field {
	if f := a.Field(name); f != nil {
		return *f
	}
	return siren.Field{}
}

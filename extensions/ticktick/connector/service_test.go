package connector

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"aminte/client"
	"aminte/siren"
)

// serviceEnv is the task service on a server, with a fake TickTick and a fake aminte behind it.
type serviceEnv struct {
	tick  *fakeTick
	life  *fakeLife
	c     *client.Client
	login bool // onLogin was called
}

func newServiceEnv(t *testing.T) *serviceEnv {
	t.Helper()
	tick, tt := newFakeTick(t)
	life, ll := newFakeLife(t)
	p, err := NewPoller(tt, ll, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p.Now = func() time.Time { return now }
	env := &serviceEnv{tick: tick, life: life}
	p.onLogin = func() { env.login = true }
	srv := httptest.NewServer(Service(p, tick.lists))
	t.Cleanup(srv.Close)
	env.c = client.Remote(srv.URL, "", "")
	return env
}

// do runs the action of the service with the name.
func (e *serviceEnv) do(t *testing.T, name string, values map[string]string) (*siren.Entity, error) {
	t.Helper()
	for _, a := range actions() {
		if a.Name == name {
			return e.c.Do(t.Context(), a, values)
		}
	}
	t.Fatalf("no action %s", name)
	return nil, nil
}

// reread turns the properties of an entity into the value of out. It reads the wire form, not the Go value.
func reread(t *testing.T, e *siren.Entity, out any) {
	t.Helper()
	b, err := json.Marshal(e.Properties)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatalf("properties %s: %v", b, err)
	}
}

func tasksOf(t *testing.T, e *siren.Entity) []task {
	t.Helper()
	var p struct {
		Tasks []task `json:"tasks"`
	}
	reread(t, e, &p)
	return p.Tasks
}

func taskIn(t *testing.T, e *siren.Entity) task {
	t.Helper()
	var got task
	reread(t, e, &got)
	return got
}

func ids(tasks []task) []string {
	out := []string{}
	for _, t := range tasks {
		out = append(out, t.ID)
	}
	return out
}

func wantError(t *testing.T, err error, status int, code, msg string) {
	t.Helper()
	var ce *client.Error
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want an error entity", err)
	}
	if ce.Status != status || ce.Code() != code {
		t.Errorf("error = %d %s, want %d %s", ce.Status, ce.Code(), status, code)
	}
	var p struct {
		Message string `json:"message"`
	}
	reread(t, ce.Entity, &p)
	if p.Message != msg {
		t.Errorf("message = %q, want %q", p.Message, msg)
	}
}

func TestTheCatalogIsMarkedTasksAndHasTheSixWrites(t *testing.T) {
	env := newServiceEnv(t)
	e, err := env.c.Get(t.Context(), "/actions")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(e.Class, []string{"actions", "tasks"}) {
		t.Errorf("class = %v, want [actions tasks]", e.Class)
	}
	got := []string{}
	for _, a := range e.Actions {
		got = append(got, a.Method+" "+a.Href)
		if a.Owner {
			t.Errorf("%s is owner-only: the server decides that, not the service", a.Name)
		}
	}
	want := []string{"POST /tasks", "POST /tasks/{id}", "POST /tasks/{id}/complete", "POST /tasks/{id}/reopen",
		"POST /tasks/{id}/delete", "POST /sync"}
	if !slices.Equal(got, want) {
		t.Errorf("writes = %v, want %v", got, want)
	}
}

func TestTheListsComeInTheOrderOfTickTick(t *testing.T) {
	env := newServiceEnv(t)
	e, err := env.c.Get(t.Context(), "/lists")
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Lists []struct{ ID, Name string } `json:"lists"`
	}
	reread(t, e, &p)
	if len(p.Lists) != 2 || p.Lists[0].ID != "p-home" || p.Lists[0].Name != "Home" || p.Lists[1].ID != "p-work" || p.Lists[1].Name != "Work" {
		t.Errorf("lists = %+v, want p-home Home, p-work Work", p.Lists)
	}
}

func TestOpenReadsEveryListOrOne(t *testing.T) {
	env := newServiceEnv(t)
	home := env.tick.add("t1", "p-home", "Pay rent")
	home.Content, home.DueDate = "before the 5th", "2030-03-09T00:00:00.000+0000"
	env.tick.add("t2", "p-work", "Write report")
	env.tick.add("t3", "", "Buy milk") // the Inbox is not listed
	env.tick.add("t4", "p-home", "Done already")
	env.tick.finish("t4", now)

	e, err := env.c.Get(t.Context(), "/open")
	if err != nil {
		t.Fatal(err)
	}
	got := tasksOf(t, e)
	if !slices.Equal(ids(got), []string{"t1", "t2"}) {
		t.Fatalf("open = %v, want t1 t2", ids(got))
	}
	want := task{ID: "t1", Title: "Pay rent", List: "p-home", Notes: "before the 5th", Due: "2030-03-09"}
	if got[0] != want {
		t.Errorf("t1 = %+v, want %+v", got[0], want)
	}

	e, err = env.c.Get(t.Context(), "/open?list=p-work")
	if err != nil {
		t.Fatal(err)
	}
	if got := tasksOf(t, e); !slices.Equal(ids(got), []string{"t2"}) {
		t.Errorf("open of p-work = %v, want t2", ids(got))
	}

	_, err = env.c.Get(t.Context(), "/open?list=nope")
	wantError(t, err, 404, "not_found", "no such list")
}

func TestDoneKeepsTheDaysFromSinceInTheZoneOfTheTask(t *testing.T) {
	env := newServiceEnv(t)
	env.tick.add("old", "p-home", "Old")
	env.tick.finish("old", time.Date(2030, 2, 20, 10, 0, 0, 0, time.UTC))
	env.tick.add("tokyo", "p-work", "Late").TimeZone = "Asia/Tokyo"
	env.tick.finish("tokyo", time.Date(2030, 3, 1, 23, 30, 0, 0, time.UTC)) // 08:30 on March 2 in Tokyo
	env.tick.add("inbox", "inbox1", "Milk")
	env.tick.finish("inbox", time.Date(2030, 3, 3, 9, 0, 0, 0, time.UTC))
	env.tick.add("open", "p-home", "Still open")

	e, err := env.c.Get(t.Context(), "/done?since=2030-03-02")
	if err != nil {
		t.Fatal(err)
	}
	got := tasksOf(t, e)
	byID := map[string]task{}
	for _, g := range got {
		byID[g.ID] = g
	}
	if len(got) != 2 {
		t.Fatalf("done = %v, want tokyo and inbox", ids(got))
	}
	wantTokyo := task{ID: "tokyo", Title: "Late", List: "p-work", Done: true, DoneDay: "2030-03-02", DoneAt: "2030-03-02T08:30:00+09:00"}
	if byID["tokyo"] != wantTokyo {
		t.Errorf("tokyo = %+v, want %+v", byID["tokyo"], wantTokyo)
	}
	wantInbox := task{ID: "inbox", Title: "Milk", List: "", Done: true, DoneDay: "2030-03-03", DoneAt: "2030-03-03T09:00:00Z"}
	if byID["inbox"] != wantInbox {
		t.Errorf("inbox = %+v, want %+v", byID["inbox"], wantInbox)
	}
	// One rule for the day: the entry of the same task is on the same day.
	if en, _ := entryOf(*env.tick.tasks["tokyo"], nil); en.day != byID["tokyo"].DoneDay {
		t.Errorf("entry day %s and done_day %s differ", en.day, byID["tokyo"].DoneDay)
	}

	e, err = env.c.Get(t.Context(), "/done?since=2030-02-01&list=p-home")
	if err != nil {
		t.Fatal(err)
	}
	if got := tasksOf(t, e); !slices.Equal(ids(got), []string{"old"}) {
		t.Errorf("done of p-home = %v, want old", ids(got))
	}

	_, err = env.c.Get(t.Context(), "/done")
	wantError(t, err, 422, "invalid", "since must be a day, YYYY-MM-DD")
	_, err = env.c.Get(t.Context(), "/done?since=2030-03-02&list=nope")
	wantError(t, err, 404, "not_found", "no such list")
}

func TestDoneOfAServerWithoutTheCompletedCallIsEmpty(t *testing.T) {
	env := newServiceEnv(t)
	env.tick.noCompleted = true
	e, err := env.c.Get(t.Context(), "/done?since=2030-01-01")
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(e.Properties)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"tasks":[]}` {
		t.Errorf("properties = %s, want {\"tasks\":[]}", b)
	}
}

func TestAnUnknownTaskIsNotFoundForEveryRoute(t *testing.T) {
	env := newServiceEnv(t)
	_, err := env.c.Get(t.Context(), "/tasks/nope")
	wantError(t, err, 404, "not_found", "no such task")
	for _, name := range []string{"edit-task", "complete-task", "reopen-task", "delete-task"} {
		_, err := env.do(t, name, map[string]string{"id": "nope"})
		wantError(t, err, 404, "not_found", "no such task")
	}

	// A task that this service learned and TickTick forgot: the 404 of TickTick is no such task.
	env.tick.add("t1", "p-work", "Gone soon")
	if _, err := env.c.Get(t.Context(), "/tasks/t1"); err != nil {
		t.Fatal(err)
	}
	delete(env.tick.tasks, "t1")
	_, err = env.do(t, "complete-task", map[string]string{"id": "t1"})
	wantError(t, err, 404, "not_found", "no such task")
}

func TestATaskIsReadByItsId(t *testing.T) {
	env := newServiceEnv(t)
	env.tick.add("t1", "p-work", "Write report").DueDate = "2030-03-09T00:00:00.000+0000"
	e, err := env.c.Get(t.Context(), "/tasks/t1")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(e.Class, []string{"task"}) {
		t.Errorf("class = %v, want task", e.Class)
	}
	if got, want := taskIn(t, e), (task{ID: "t1", Title: "Write report", List: "p-work", Due: "2030-03-09"}); got != want {
		t.Errorf("task = %+v, want %+v", got, want)
	}
}

func TestAddMakesTheTaskInTheListOrTheInbox(t *testing.T) {
	env := newServiceEnv(t)
	e, err := env.do(t, "add-task", map[string]string{"title": "Milk", "list": "p-home", "due": "2030-03-09", "notes": "2 litres"})
	if err != nil {
		t.Fatal(err)
	}
	made := env.tick.tasks["new1"]
	if made == nil || made.Title != "Milk" || made.ProjectID != "p-home" || made.Content != "2 litres" || !made.IsAllDay ||
		!strings.HasPrefix(made.DueDate, "2030-03-09T00:00:00.000") {
		t.Fatalf("task in TickTick = %+v", made)
	}
	if got, want := taskIn(t, e), (task{ID: "new1", Title: "Milk", List: "p-home", Notes: "2 litres", Due: "2030-03-09"}); got != want {
		t.Errorf("answer = %+v, want %+v", got, want)
	}

	e, err = env.do(t, "add-task", map[string]string{"title": "Stamps"})
	if err != nil {
		t.Fatal(err)
	}
	if inbox := env.tick.tasks["new2"]; inbox == nil || inbox.ProjectID != "" {
		t.Errorf("task in TickTick = %+v, want one in the Inbox (no projectId)", inbox)
	}
	if got := taskIn(t, e); got.List != "" || got.Title != "Stamps" {
		t.Errorf("answer = %+v, want Stamps with the list \"\"", got)
	}
	// The Inbox task can be read at once: the service remembers where it made it.
	if _, err := env.c.Get(t.Context(), "/tasks/new2"); err != nil {
		t.Errorf("reading the new Inbox task: %v", err)
	}
}

func TestAddRefusals(t *testing.T) {
	env := newServiceEnv(t)
	tests := []struct {
		name   string
		values map[string]string
		msg    string
	}{
		{"a due that is not a day", map[string]string{"title": "x", "due": "09/03/2030"}, "due must be a day, YYYY-MM-DD"},
		{"a due with a time", map[string]string{"title": "x", "due": "2030-03-09T10:00"}, "due must be a day, YYYY-MM-DD"},
		{"a list that is not a list", map[string]string{"title": "x", "list": "Nowhere"}, "no such list"},
		{"a blank title", map[string]string{"title": "  "}, "title is required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := env.do(t, "add-task", tc.values)
			wantError(t, err, 422, "invalid", tc.msg)
		})
	}
	if len(env.tick.tasks) != 0 {
		t.Errorf("a refused add made %d tasks in TickTick", len(env.tick.tasks))
	}
}

func TestEditChangesOnlyTheFieldsThatTheRequestNames(t *testing.T) {
	env := newServiceEnv(t)
	tk := env.tick.add("t1", "p-home", "Pay rent")
	tk.Content, tk.DueDate, tk.StartDate, tk.IsAllDay = "before the 5th", "2030-03-09T00:00:00.000+0000", "2030-03-09T00:00:00.000+0000", true

	e, err := env.do(t, "edit-task", map[string]string{"id": "t1", "title": "Pay the rent"})
	if err != nil {
		t.Fatal(err)
	}
	if tk.Title != "Pay the rent" || tk.Content != "before the 5th" || tk.DueDate != "2030-03-09T00:00:00.000+0000" || tk.ProjectID != "p-home" {
		t.Errorf("after a title edit: %+v, want only the title changed", tk)
	}
	if got, want := taskIn(t, e), (task{ID: "t1", Title: "Pay the rent", List: "p-home", Notes: "before the 5th", Due: "2030-03-09"}); got != want {
		t.Errorf("answer = %+v, want %+v", got, want)
	}

	if _, err := env.do(t, "edit-task", map[string]string{"id": "t1", "due": "2030-03-12"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tk.DueDate, "2030-03-12T00:00:00.000") || tk.Title != "Pay the rent" || tk.Content != "before the 5th" {
		t.Errorf("after a due edit: %+v, want only the due day changed", tk)
	}

	if _, err := env.do(t, "edit-task", map[string]string{"id": "t1", "due": "", "notes": ""}); err != nil {
		t.Fatal(err)
	}
	if tk.DueDate != "" || tk.StartDate != "" || tk.Content != "" || tk.Title != "Pay the rent" {
		t.Errorf("after an empty due and notes: %+v, want both cleared and the title kept", tk)
	}
	last := env.tick.updates[len(env.tick.updates)-1]
	if last.ID != "t1" || last.ProjectID != "p-home" {
		t.Errorf("the body of the update = %+v, want the id and the projectId", last)
	}
}

// A browser form sends every field, the due day too. The same day keeps the due of TickTick as it is, with its time.
func TestEditWithTheSameDueDayKeepsTheTimeOfTheDue(t *testing.T) {
	env := newServiceEnv(t)
	tk := env.tick.add("t1", "p-home", "Call the bank")
	tk.DueDate, tk.StartDate, tk.TimeZone, tk.IsAllDay = "2030-03-09T14:30:00.000+0000", "2030-03-09T14:30:00.000+0000", "UTC", false

	if _, err := env.do(t, "edit-task", map[string]string{"id": "t1", "title": "Call the bank at 2", "notes": "", "due": "2030-03-09"}); err != nil {
		t.Fatal(err)
	}
	if tk.Title != "Call the bank at 2" || tk.DueDate != "2030-03-09T14:30:00.000+0000" || tk.StartDate != "2030-03-09T14:30:00.000+0000" || tk.IsAllDay {
		t.Errorf("after an edit with the same due day: %+v, want the new title and the due at 14:30 kept", tk)
	}
}

func TestEditWithNoFieldCallsNothing(t *testing.T) {
	env := newServiceEnv(t)
	env.tick.add("t1", "p-home", "Pay rent")
	if _, err := env.do(t, "edit-task", map[string]string{"id": "t1"}); err != nil {
		t.Fatal(err)
	}
	if len(env.tick.updates) != 0 {
		t.Errorf("%d updates, want none: no field was sent", len(env.tick.updates))
	}
}

func TestEditRefusals(t *testing.T) {
	env := newServiceEnv(t)
	env.tick.add("t1", "p-home", "Pay rent")
	_, err := env.do(t, "edit-task", map[string]string{"id": "t1", "title": " "})
	wantError(t, err, 422, "invalid", "title must not be blank")
	_, err = env.do(t, "edit-task", map[string]string{"id": "t1", "due": "next week"})
	wantError(t, err, 422, "invalid", "due must be a day, YYYY-MM-DD")
	if len(env.tick.updates) != 0 || env.tick.tasks["t1"].Title != "Pay rent" {
		t.Errorf("a refused edit changed the task: %+v, %d updates", env.tick.tasks["t1"], len(env.tick.updates))
	}
}

func TestCompleteMarksTheTaskDone(t *testing.T) {
	env := newServiceEnv(t)
	env.tick.add("t1", "p-work", "Write report")
	e, err := env.do(t, "complete-task", map[string]string{"id": "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if env.tick.tasks["t1"].Status != 2 {
		t.Errorf("status in TickTick = %d, want 2", env.tick.tasks["t1"].Status)
	}
	if got := taskIn(t, e); !got.Done || got.ID != "t1" {
		t.Errorf("answer = %+v, want a done t1", got)
	}
}

func TestReopenMakesADoneTaskOpenAgain(t *testing.T) {
	env := newServiceEnv(t)
	env.tick.add("t1", "p-work", "Write report")
	env.tick.finish("t1", now) // not seen in a list: the service finds it among the done tasks
	e, err := env.do(t, "reopen-task", map[string]string{"id": "t1"})
	if err != nil {
		t.Fatal(err)
	}
	tk := env.tick.tasks["t1"]
	if tk.Status != 0 || tk.CompletedTime != "" {
		t.Errorf("task in TickTick = %+v, want status 0 and no completion time", tk)
	}
	if got := taskIn(t, e); got.Done || got.DoneDay != "" || got.List != "p-work" {
		t.Errorf("answer = %+v, want an open task of p-work", got)
	}
	open, err := env.c.Get(t.Context(), "/open")
	if err != nil {
		t.Fatal(err)
	}
	if got := tasksOf(t, open); !slices.Equal(ids(got), []string{"t1"}) {
		t.Errorf("open = %v, want t1", ids(got))
	}
}

func TestDeleteRemovesTheTaskAndAnswersItAsItWas(t *testing.T) {
	env := newServiceEnv(t)
	env.tick.add("t1", "p-work", "Write report")
	e, err := env.do(t, "delete-task", map[string]string{"id": "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, there := env.tick.tasks["t1"]; there {
		t.Error("t1 is still in TickTick")
	}
	if got, want := taskIn(t, e), (task{ID: "t1", Title: "Write report", List: "p-work"}); got != want {
		t.Errorf("answer = %+v, want %+v", got, want)
	}
	_, err = env.do(t, "delete-task", map[string]string{"id": "t1"})
	wantError(t, err, 404, "not_found", "no such task")
}

func TestATickTickFaultIsBadGatewayWithoutItsBody(t *testing.T) {
	env := newServiceEnv(t)
	env.tick.unauthorized = true
	_, err := env.c.Get(t.Context(), "/open")
	wantError(t, err, 502, "upstream", "the task manager refused the login (401): run the login of the task service again")
	if !env.login {
		t.Error("a 401 in a handler must tell Serve to stop")
	}
	_, err = env.do(t, "add-task", map[string]string{"title": "x"})
	wantError(t, err, 502, "upstream", "the task manager refused the login (401): run the login of the task service again")
}

func TestSyncPollsAndAnswersTheCount(t *testing.T) {
	env := newServiceEnv(t)
	env.tick.add("t1", "p-home", "Pay rent")
	env.tick.finish("t1", now.Add(-time.Hour))
	for _, want := range []int{1, 0} {
		e, err := env.do(t, "sync", nil)
		if err != nil {
			t.Fatal(err)
		}
		var p struct {
			Written int `json:"written"`
		}
		reread(t, e, &p)
		if p.Written != want {
			t.Errorf("written = %d, want %d", p.Written, want)
		}
	}
	if len(env.life.got()) != 1 {
		t.Errorf("captures = %+v, want 1", env.life.got())
	}
}

func TestSyncAnswersAAminteFaultWithoutItsWordsOrItsCause(t *testing.T) {
	env := newServiceEnv(t)
	env.tick.add("t1", "p-home", "Pay rent")
	env.tick.finish("t1", now.Add(-time.Hour))
	env.life.refuse = 422
	_, err := env.do(t, "sync", nil)
	var ce *client.Error
	if !errors.As(err, &ce) || ce.Status != 502 {
		t.Fatalf("err = %v, want 502", err)
	}
	var p struct {
		Message string `json:"message"`
	}
	reread(t, ce.Entity, &p)
	if p.Message != "the poll failed" {
		t.Errorf("message = %q, want only that the poll failed", p.Message)
	}
}

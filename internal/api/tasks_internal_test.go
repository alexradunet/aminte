package api

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"aminte/internal/core"
	"aminte/internal/db/dbtest"
	"aminte/siren"
)

func titlesOf(tasks []svcTask) []string {
	var out []string
	for _, t := range tasks {
		out = append(out, t.Title)
	}
	return out
}

// A task with no due day goes last, tasks with one day keep the order of the service, and nothing else counts.
func TestSortOpenByDueDayOnly(t *testing.T) {
	tasks := []svcTask{
		{Title: "no day a"}, {Title: "late b", Due: "2031-02-09"}, {Title: "early", Due: "2031-01-30"},
		{Title: "no day b"}, {Title: "late a", Due: "2031-02-09"},
	}
	sortOpen(tasks)
	want := []string{"early", "late b", "late a", "no day a", "no day b"}
	if got := titlesOf(tasks); !slices.Equal(got, want) {
		t.Errorf("sorted = %v, want %v", got, want)
	}
}

// Done tasks are newest first by the instant. The instant is done_at, else the start of the done day.
func TestSortDoneNewestFirst(t *testing.T) {
	tasks := []svcTask{
		{Title: "old", DoneAt: "2031-02-01T23:00:00Z"},
		{Title: "day only", DoneDay: "2031-02-02"},
		{Title: "zone", DoneAt: "2031-02-03T01:00:00+05:00"}, // 2031-02-02T20:00Z
		{Title: "unknown"},
		{Title: "new", DoneAt: "2031-02-03T08:00:00Z"},
	}
	sortDone(tasks)
	want := []string{"new", "zone", "day only", "old", "unknown"}
	if got := titlesOf(tasks); !slices.Equal(got, want) {
		t.Errorf("sorted = %v, want %v", got, want)
	}
}

// The windows are counted in days of the clock they get. Month ends and a leap day are the edges.
func TestTheDayWindows(t *testing.T) {
	zone := time.FixedZone("test", 2*60*60)
	for _, c := range []struct {
		now                        time.Time
		today, weekLast, doneSince string
	}{
		{time.Date(2031, 2, 3, 12, 0, 0, 0, zone), "2031-02-03", "2031-02-09", "2031-01-04"},
		{time.Date(2031, 2, 25, 23, 59, 0, 0, zone), "2031-02-25", "2031-03-03", "2031-01-26"},
		{time.Date(2032, 2, 25, 0, 0, 0, 0, zone), "2032-02-25", "2032-03-02", "2032-01-26"}, // 2032 has a 29 February
		{time.Date(2031, 12, 30, 1, 0, 0, 0, zone), "2031-12-30", "2032-01-05", "2031-11-30"},
	} {
		got := daysOf(c.now)
		if got.Today != c.today || got.WeekLast != c.weekLast || got.DoneSince != c.doneSince {
			t.Errorf("daysOf(%v) = %+v, want %s, %s, %s", c.now, got, c.today, c.weekLast, c.doneSince)
		}
	}
}

// dueBetween takes both ends and no task without a day. overdue counts the days before today.
func TestDueBetweenAndOverdue(t *testing.T) {
	tasks := []svcTask{
		{Title: "before", Due: "2031-02-02"}, {Title: "today", Due: "2031-02-03"}, {Title: "last", Due: "2031-02-09"},
		{Title: "after", Due: "2031-02-10"}, {Title: "none"},
	}
	for _, c := range []struct {
		first, last string
		want        []string
	}{
		{"", "2031-02-03", []string{"before", "today"}},
		{"2031-02-03", "2031-02-09", []string{"today", "last"}},
		{"2031-02-04", "2031-02-08", nil},
	} {
		if got := titlesOf(dueBetween(tasks, c.first, c.last)); !slices.Equal(got, c.want) {
			t.Errorf("dueBetween(%q, %q) = %v, want %v", c.first, c.last, got, c.want)
		}
	}
	if got := overdue(tasks, "2031-02-03"); got != 1 {
		t.Errorf("overdue = %d, want 1", got)
	}
}

// theSixTaskActions are the names of the six actions of Tasks, and the hrefs of the six writes of the protocol of a task
// service (extensions/README.md, "A task service"), written out by hand: the expected answers do not come from
// taskCatalog.
var theSixTaskActions = []string{"add-task", "edit-task", "complete-task", "reopen-task", "delete-task", "sync-tasks"}
var theSixTaskWrites = []string{"/tasks", "/tasks/{id}", "/tasks/{id}/complete", "/tasks/{id}/reopen", "/tasks/{id}/delete", "/sync"}

// actionsOfWrites is the catalog of a task service: the six writes of the protocol.
func actionsOfWrites() []siren.Action {
	var out []siren.Action
	for i, href := range theSixTaskWrites {
		out = append(out, siren.Action{Name: theSixTaskActions[i], Method: "POST", Href: href})
	}
	return out
}

// taskCatalog is the one table of the six actions: each row has the name of its action and the href of its write at the
// service, and the table has no other row. A service that answers the six hrefs is registered (the tests below).
func TestTheTaskCatalogHoldsTheSixWritesOfTheProtocol(t *testing.T) {
	if len(taskCatalog) != len(theSixTaskActions) {
		t.Fatalf("taskCatalog has %d rows, want %d", len(taskCatalog), len(theSixTaskActions))
	}
	for i, name := range theSixTaskActions {
		at := slices.IndexFunc(taskCatalog, func(x spec) bool { return x.Name == name })
		if at < 0 {
			t.Errorf("taskCatalog has no row %s", name)
			continue
		}
		if got := taskCatalog[at].Service; got != theSixTaskWrites[i] {
			t.Errorf("%s: the href at the service is %q, want %q", name, got, theSixTaskWrites[i])
		}
	}
}

// The registry holds one task service. It refuses a second, and a catalog that lacks a write; Remove frees the role;
// an app cannot take the name of a task action; the service is not an app.
func TestTheRegistryHoldsOneTaskService(t *testing.T) {
	a := NewApps()
	a.Expect("svc")
	lists := []taskList{{ID: "l1", Name: "Home"}}

	short := actionsOfWrites()[:5] // no sync
	if err := a.setTasks("svc", "http://127.0.0.1:1", short, lists); err == nil || !strings.Contains(err.Error(), "no action POST /sync") {
		t.Errorf("a catalog with no sync: got %v, want the missing write named", err)
	}
	if _, ok := a.taskService(); ok {
		t.Error("a refused catalog made a task service")
	}
	if err := a.setTasks("svc", "http://127.0.0.1:1", actionsOfWrites(), lists); err != nil {
		t.Fatal(err)
	}
	if _, known := a.find("svc"); known {
		t.Error("the task service is an app too")
	}
	if names := a.names(); len(names) != 0 {
		t.Errorf("Names = %v, want none: a task service is not an app", names)
	}
	err := a.setTasks("other", "http://127.0.0.1:2", actionsOfWrites(), lists)
	if err == nil || err.Error() != "a task service runs already: svc" {
		t.Errorf("a second service: got %v", err)
	}
	if svc, _ := a.taskService(); svc.name != "svc" {
		t.Errorf("the service is %q after a refusal, want svc", svc.name)
	}
	if specs := a.specs(); len(specs) != len(taskCatalog) {
		t.Errorf("specs = %d rows, want the %d task actions", len(specs), len(taskCatalog))
	}
	if err := a.set("add", "http://127.0.0.1:3", "Add", []siren.Action{{Name: "task", Method: "POST", Href: "/x"}}); err == nil {
		t.Error("an app took the name add-task")
	}
	a.Remove("svc")
	if _, ok := a.taskService(); ok {
		t.Error("Remove left the task service")
	}
	if specs := a.specs(); len(specs) != 0 {
		t.Errorf("specs = %d rows after Remove, want none", len(specs))
	}
	if err := a.setTasks("other", "http://127.0.0.1:2", actionsOfWrites(), lists); err != nil {
		t.Errorf("the role is free after Remove: %v", err)
	}
}

// The field list of add-task has the names of the lists as its options, and the options of the static row stay empty.
func TestAddTaskOffersTheNamesOfTheLists(t *testing.T) {
	svc := taskService{lists: []taskList{{ID: "a", Name: "Home"}, {ID: "b", Name: "Work"}}}
	if got := fieldOptions(svc.spec("add-task"), "list"); !slices.Equal(got, []string{"Home", "Work"}) {
		t.Errorf("options = %v, want [Home Work]", got)
	}
	if got := fieldOptions(taskCatalog[0], "list"); got != nil {
		t.Errorf("the static row got the options %v", got)
	}
}

func fieldOptions(s spec, name string) []string {
	for _, f := range s.Fields {
		if f.Name == name {
			return f.Options
		}
	}
	return nil
}

// checkValues gives the id of the list for its name, takes an empty list as the default list, and refuses the rest.
func TestCheckValuesMapsTheListAndRefusesWhatTheProtocolCannotTake(t *testing.T) {
	svc := taskService{lists: []taskList{{ID: "a", Name: "Home"}}}
	v := map[string]string{"title": "x", "list": "Home", "due": "2031-02-03"}
	if err := svc.checkValues(v); err != nil || v["list"] != "a" {
		t.Errorf("a good set: err %v, list %q, want the id a", err, v["list"])
	}
	v = map[string]string{"title": "x", "list": ""}
	if err := svc.checkValues(v); err != nil {
		t.Fatal(err)
	}
	if _, has := v["list"]; has {
		t.Error("an empty list was sent: the default list has no id")
	}
	for _, bad := range []map[string]string{{"title": " "}, {"due": "soon"}, {"list": "Nowhere"}, {"due": "2031-02-30"}} {
		if err := svc.checkValues(bad); err == nil {
			t.Errorf("%v: no error", bad)
		}
	}
	if err := svc.checkValues(map[string]string{"due": "", "notes": ""}); err != nil {
		t.Errorf("an empty due and notes clear a field: %v", err)
	}
}

func listsEntity(lists ...map[string]any) *siren.Entity {
	return &siren.Entity{Class: []string{"lists"}, Properties: map[string]any{"lists": lists}}
}

// Two lists with one id are refused. A later list with a name that is taken gets the suffix " (2)", " (3)" ...
// so the owner sees, and add-task takes, one name for one list.
func TestListsWithTheSameNameGetUniqueNames(t *testing.T) {
	got, err := taskListsOf(listsEntity(
		map[string]any{"id": "a", "name": "Work"}, map[string]any{"id": "b", "name": "Home"},
		map[string]any{"id": "c", "name": "Work"}, map[string]any{"id": "d", "name": "Work"},
		map[string]any{"id": "e", "name": "Work (2)"}))
	if err != nil {
		t.Fatal(err)
	}
	want := []taskList{{"a", "Work"}, {"b", "Home"}, {"c", "Work (2)"}, {"d", "Work (3)"}, {"e", "Work (2) (2)"}}
	if !slices.Equal(got, want) {
		t.Errorf("lists = %v, want %v", got, want)
	}
	if _, err := taskListsOf(listsEntity(map[string]any{"id": "a", "name": "One"}, map[string]any{"id": "a", "name": "Two"})); err == nil {
		t.Error("a duplicate id was taken")
	}
	svc := taskService{lists: got}
	v := map[string]string{"list": "Work (2)"}
	if err := svc.checkValues(v); err != nil || v["list"] != "c" {
		t.Errorf("the second Work: err %v, list %q, want the id c", err, v["list"])
	}
}

// tasksOver is the API over a fresh file with a task service that answers 500 to every request and counts them. A test
// that sees the count stay at zero knows that the server refused the request before it called the service.
func tasksOver(t *testing.T) (h http.Handler, apps *Apps, requests *atomic.Int32) {
	t.Helper()
	requests = new(atomic.Int32)
	stand := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "the service must not be called", http.StatusInternalServerError)
	}))
	t.Cleanup(stand.Close)
	apps = NewApps()
	apps.Expect("svc")
	lists := []taskList{{ID: "home", Name: "Home"}, {ID: "work", Name: "Work"}}
	if err := apps.setTasks("svc", stand.URL, actionsOfWrites(), lists); err != nil {
		t.Fatal(err)
	}
	return NewWith(&core.Store{DB: dbtest.New(t)}, apps), apps, requests
}

func send(h http.Handler, method, target, body string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// The server refuses a write that the protocol cannot take, or that the action does not list, or that the writer may
// not do, and it does not call the service (API.md, "Tasks").
func TestARefusedTaskWriteDoesNotCallTheService(t *testing.T) {
	h, _, requests := tasksOver(t)
	for _, c := range []struct {
		name, target, body, source string
		status                     int
		code                       string
	}{
		{"a blank title", "/tasks", "title=+++", "", 422, siren.CodeInvalid},
		{"a blank title in an edit", "/tasks/items/t1", "title=+", "", 422, siren.CodeInvalid},
		{"a bad due day", "/tasks", "title=x&due=soon", "", 422, siren.CodeInvalid},
		{"a day that does not exist", "/tasks/items/t1", "due=2031-02-30", "", 422, siren.CodeInvalid},
		{"an unknown list", "/tasks", "title=x&list=Nowhere", "", 422, siren.CodeInvalid},
		{"a field the action does not list", "/tasks", "title=x&bogus=1", "", 422, siren.CodeInvalid},
		{"a field of another action", "/tasks/items/t1/complete", "title=x", "", 422, siren.CodeInvalid},
		{"delete as an agent", "/tasks/items/t1/delete", "", "agent:x", 403, siren.CodeForbidden},
	} {
		header := map[string]string{}
		if c.source != "" {
			header[siren.SourceHeader] = c.source
		}
		rec := send(h, "POST", c.target, c.body, header)
		if rec.Code != c.status || !strings.Contains(rec.Body.String(), `"`+c.code+`"`) {
			t.Errorf("%s: %d %s, want %d %s", c.name, rec.Code, rec.Body, c.status, c.code)
		}
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("the service got %d requests, want none", n)
	}
	// The stand-in is reachable: a good write does call it. Without this the count above proves nothing.
	if rec := send(h, "POST", "/tasks", "title=x", nil); rec.Code == 200 || requests.Load() != 1 {
		t.Errorf("a good write: %d, %d requests, want one request to the service", rec.Code, requests.Load())
	}
}

// When the task service ends, the link tasks leaves the root, the six actions leave GET /actions and every route is 503.
func TestWhenTheTaskServiceEndsTasksIsGone(t *testing.T) {
	h, apps, _ := tasksOver(t)
	hasLink := func() bool {
		return strings.Contains(send(h, "GET", "/", "", nil).Body.String(), `"href": "/tasks"`)
	}
	hasActions := func() bool { return strings.Contains(send(h, "GET", "/actions", "", nil).Body.String(), `"add-task"`) }
	if !hasLink() || !hasActions() {
		t.Fatalf("while the service runs: link %v, actions %v, want both", hasLink(), hasActions())
	}
	apps.Remove("svc")
	if hasLink() {
		t.Error("the root still links to /tasks")
	}
	body := send(h, "GET", "/actions", "", nil).Body.String()
	for _, name := range theSixTaskActions {
		if strings.Contains(body, `"`+name+`"`) {
			t.Errorf("GET /actions still has %s", name)
		}
	}
	rec := send(h, "GET", "/tasks", "", nil)
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), `"`+siren.CodeUnavailable+`"`) {
		t.Errorf("GET /tasks = %d %s, want 503 unavailable", rec.Code, rec.Body)
	}
}

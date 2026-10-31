package web_test

// The pages of Tasks, as web draws them over the real core and a small task service (lifelogtest.ConnectTasks). The core
// answers Siren JSON: the rows with their round buttons, the due chips, the side column and the quick-add. These tests
// hold the HTML of that path from end to end, and the write of a round button through web.

import (
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// watchedTaskService is a task service that a test can watch and break. It keeps the paths of the complete requests that
// it got, and while failing is true it answers every read, except the catalog, with a 500 that is not an entity.
type watchedTaskService struct {
	next    http.Handler
	failing atomic.Bool
	mu      sync.Mutex
	got     []string
}

func newWatchedTaskService() *watchedTaskService {
	return &watchedTaskService{next: taskServiceHandler()}
}

func (s *watchedTaskService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/complete"):
		s.mu.Lock()
		s.got = append(s.got, r.URL.Path)
		s.mu.Unlock()
	case s.failing.Load() && r.Method == http.MethodGet && r.URL.Path != "/actions":
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	s.next.ServeHTTP(w, r)
}

// completed are the paths of the complete requests that the service got, in order.
func (s *watchedTaskService) completed() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.got...)
}

// wantTaskRows fails the test unless the page draws the two tasks of the service as rows in this order. Each row has its
// link, its round Complete form and its due chip. The forms post to the paths of the tasks and return to back.
func wantTaskRows(t *testing.T, what, page, back string) {
	t.Helper()
	for _, row := range []struct{ id, title, due string }{{"t1", "Fix tap", "2031-05-12"}, {"t2", "Call mom", "2031-05-14"}} {
		form := `<form method="POST" action="/tasks/items/` + row.id + `/complete" class="round">`
		wantInPage(t, what, page, form, `<a href="/tasks/items/`+row.id+`">`+row.title+`</a>`, `<span class="chip">due: `+row.due+`</span>`)
		// The form is the round button: it names the task to the screen reader, and it returns to the page that it is on.
		round := regexp.MustCompile(`(?s)` + regexp.QuoteMeta(form) + `.*?</form>`).FindString(page)
		wantInPage(t, what+", the form of "+row.title, round, `aria-label="Complete"`, `<input type="hidden" name="back" value="`+back+`">`)
		if n := strings.Count(page, `action="/tasks/items/`+row.id+`/complete"`); n != 1 {
			t.Errorf("%s draws the Complete form of %s %d times, want once", what, row.title, n)
		}
	}
	fix, call := strings.Index(page, "Fix tap"), strings.Index(page, "Call mom")
	if fix < 0 || call < 0 || fix > call {
		t.Errorf("%s draws Call mom (at %d) before Fix tap (at %d): the overdue task comes first", what, call, fix)
	}
	if quick, first := strings.Index(page, `class="quick-add"`), strings.Index(page, `<li class="row">`); quick < 0 || first < 0 || quick > first {
		t.Errorf("%s: the quick-add (at %d) is not before the first row (at %d)", what, quick, first)
	}
	wantNotInPage(t, what, page, "/with/")
	if strings.Contains(strings.ToLower(page), "taskservice") {
		t.Errorf("%s names the service", what)
	}
}

// TestThePagesOfTasksDrawTheRowsTheQuickAddAndTheSideColumn: Today and one list are pages of the shell. The quick-add
// posts to /tasks. A row is a link with its due chip and the round Complete form of its task. The side column links to
// the lists. The page names neither an app address nor the service.
func TestThePagesOfTasksDrawTheRowsTheQuickAddAndTheSideColumn(t *testing.T) {
	l := emptyLife(t)
	l.core.ConnectTasks(t, "taskservice", newWatchedTaskService())

	today := htmlPageAt(t, l.web, "/tasks", http.StatusOK)
	wantInPage(t, "/tasks", today,
		`<form method="POST" action="/tasks" class="quick-add" data-action="add-task">`,
		`<nav class="side" aria-label="Links">`, `href="/tasks/lists/home"`, `href="/tasks/lists/work"`, `href="/tasks/week"`)
	wantTaskRows(t, "/tasks", today, "/tasks")

	list := htmlPageAt(t, l.web, "/tasks/lists/home", http.StatusOK)
	wantInPage(t, "/tasks/lists/home", list,
		`<form method="POST" action="/tasks" class="quick-add" data-action="add-task">`, `<option selected>Home</option>`,
		`<nav class="side" aria-label="Links">`, `href="/tasks/lists/work"`, `href="/tasks/lists/home/done"`)
	wantTaskRows(t, "/tasks/lists/home", list, "/tasks/lists/home")
}

// TestTheRoundFormOfATaskRowPostsThroughWebAndReturnsToThePage: the browser posts the form of a row. Web sends the write to
// the core, the core sends it to the task service, and the browser is redirected (303) to the page that it was on, with the
// receipt of the toast. The service got the write once.
func TestTheRoundFormOfATaskRowPostsThroughWebAndReturnsToThePage(t *testing.T) {
	service := newWatchedTaskService()
	l := emptyLife(t)
	l.core.ConnectTasks(t, "taskservice", service)

	page := htmlPageAt(t, l.web, "/tasks/lists/home", http.StatusOK)
	form := regexp.MustCompile(`(?s)<form method="POST" action="/tasks/items/t2/complete" class="round">.*?</form>`).FindString(page)
	if form == "" {
		t.Fatalf("/tasks/lists/home has no round Complete form of the task t2:\n%.1500s", page)
	}
	action, sent := sentByTheBrowser(t, form)
	rec := postForm(l.web, action, sent)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s: %d, want 303:\n%.500s", action, rec.Code, rec.Body.String())
	}
	if got := returnedPathOf(t, rec.Header().Get("Location")); got != "/tasks/lists/home" {
		t.Errorf("the form of the row returned to %q, want /tasks/lists/home", got)
	}
	if got := service.completed(); len(got) != 1 || got[0] != "/tasks/t2/complete" {
		t.Errorf("the service got the complete requests %q, want one: /tasks/t2/complete", got)
	}
	browserPageAt(t, l.web, rec.Header().Get("Location")) // the page that the browser lands on is a whole page
}

// TestAFailedReadOfTheTaskServiceIsAnErrorPageThatNamesNoService: when the service answers what is not an entity, the
// browser gets the page of an error in the shell, with the code upstream and the status 502. The page shows no task, and
// it names neither the service nor its address.
func TestAFailedReadOfTheTaskServiceIsAnErrorPageThatNamesNoService(t *testing.T) {
	service := newWatchedTaskService()
	l := emptyLife(t)
	l.core.ConnectTasks(t, "taskservice", service)
	service.failing.Store(true)

	for _, path := range []string{"/tasks", "/tasks/lists/home"} {
		page := htmlPageAt(t, l.web, path, http.StatusBadGateway)
		wantInPage(t, path, page, `<span class="kind">error</span>`, "code: upstream", "status: 502")
		wantNotInPage(t, path, page, "Fix tap", "Call mom", "/with/", "127.0.0.1")
		if strings.Contains(strings.ToLower(page), "taskservice") {
			t.Errorf("%s: the page of the error names the service", path)
		}
	}
}

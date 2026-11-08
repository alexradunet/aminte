package connector

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"aminte-ticktick/ticktick"
	"aminte/app"
	"aminte/siren"
)

const dayFmt = "2006-01-02"

// task is a task as the task protocol gives it (extensions/README.md, "A task service"). The tags are the names on the
// wire.
type task struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	List    string `json:"list"` // the id of the list, "" for the Inbox
	Notes   string `json:"notes,omitempty"`
	Due     string `json:"due,omitempty"`
	Done    bool   `json:"done"`
	DoneDay string `json:"done_day,omitempty"`
	DoneAt  string `json:"done_at,omitempty"`
}

// service holds what the handlers share. The mutex guards project only.
type service struct {
	p     *Poller
	lists []ticktick.Project

	mu      sync.Mutex
	project map[string]string // task id -> project id, learned from the lists and from the tasks made here
}

func idField() siren.Field {
	return siren.Field{Name: "id", Type: "text", Title: "Task", Required: true, In: "path"}
}

// actions are the six writes of the protocol. The server calls the href and not the name. They carry no class: the
// server draws /tasks from its own catalog of the protocol, and that catalog holds the class of each write.
func actions() []siren.Action {
	byID := func(name, href, title string) siren.Action {
		return siren.Action{Name: name, Method: "POST", Href: href, Title: title, Fields: []siren.Field{idField()}}
	}
	return []siren.Action{
		{Name: "add-task", Method: "POST", Href: "/tasks", Title: "Add a task",
			Description: "Make a task. list is the id of a list; without it the task goes to the Inbox. due is a day, YYYY-MM-DD.",
			Fields: []siren.Field{
				{Name: "title", Type: "text", Title: "Title", Required: true},
				{Name: "list", Type: "text", Title: "List"},
				{Name: "due", Type: "date", Title: "Due day"},
				{Name: "notes", Type: "textarea", Title: "Notes", Rows: 4},
			}},
		{Name: "edit-task", Method: "POST", Href: "/tasks/{id}", Title: "Edit a task",
			Description: "Change the fields that the request names. A field that is not sent stays. An empty due or notes clears it.",
			Fields: []siren.Field{idField(),
				{Name: "title", Type: "text", Title: "Title"},
				{Name: "due", Type: "date", Title: "Due day"},
				{Name: "notes", Type: "textarea", Title: "Notes", Rows: 4},
			}},
		byID("complete-task", "/tasks/{id}/complete", "Complete a task"),
		byID("reopen-task", "/tasks/{id}/reopen", "Reopen a task"),
		byID("delete-task", "/tasks/{id}/delete", "Delete a task"),
		{Name: "sync", Method: "POST", Href: "/sync", Title: "Sync now",
			Description: "Read the done tasks and write each as an entry of its day, once. The service does this every poll interval too."},
	}
}

// Service is the handler of the task service. It serves the task protocol and no page: aminte draws the pages.
// lists are the lists that TickTick reported at the start.
func Service(p *Poller, lists []ticktick.Project) http.Handler {
	s := &service{p: p, lists: lists, project: map[string]string{}}
	inner := app.CatalogOf("tasks", "Tasks", s.home, actions(), map[string]func(*http.Request) (*siren.Entity, error){
		"add-task": s.add, "edit-task": s.edit, "complete-task": s.complete, "reopen-task": s.reopen,
		"delete-task": s.remove, "sync": s.sync,
	})
	// app.CatalogOf routes the actions and the home. The reads of the protocol are routes beside it.
	mux := http.NewServeMux()
	mux.Handle("/", inner)
	for pattern, f := range map[string]func(*http.Request) (*siren.Entity, error){
		"GET /lists": s.showLists, "GET /open": s.open, "GET /done": s.done, "GET /tasks/{id}": s.show,
	} {
		mux.HandleFunc(pattern, app.Entity(f))
	}
	return mux
}

// upstream turns an error of TickTick into the error of the service. TickTick's body is not in it, and neither is its
// name: the app never shows which task manager runs. A nil error stays nil.
func (s *service) upstream(err error) error {
	err = s.p.check(err)
	var se *ticktick.StatusError
	switch {
	case errors.Is(err, ErrLoginAgain):
		return &app.Error{Status: http.StatusBadGateway, Code: siren.CodeUpstream, Msg: "the task manager refused the login (401): run the login of the task service again"}
	case errors.As(err, &se):
		return &app.Error{Status: http.StatusBadGateway, Code: siren.CodeUpstream, Msg: fmt.Sprintf("the task manager answered %d", se.Status)}
	}
	return err
}

func invalid(msg string) error {
	return &app.Error{Status: http.StatusUnprocessableEntity, Code: siren.CodeInvalid, Msg: msg}
}

func noTask() error {
	return &app.Error{Status: http.StatusNotFound, Code: siren.CodeNotFound, Msg: "no such task"}
}

func noList() error {
	return &app.Error{Status: http.StatusNotFound, Code: siren.CodeNotFound, Msg: "no such list"}
}

func isStatus(err error, status int) bool {
	var se *ticktick.StatusError
	return errors.As(err, &se) && se.Status == status
}

// writeError is the error of a call that changes a task: a 404 of TickTick is no such task.
func (s *service) writeError(err error) error {
	if isStatus(err, http.StatusNotFound) {
		return noTask()
	}
	return s.upstream(err)
}

func (s *service) remember(projectID string, tasks ...ticktick.Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range tasks {
		s.project[t.ID] = projectID
	}
}

func (s *service) forget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.project, id)
}

func (s *service) hasList(id string) bool {
	return slices.ContainsFunc(s.lists, func(l ticktick.Project) bool { return l.ID == id })
}

// taskOf maps a task of TickTick to the task of the protocol. A task in the Inbox, or in a list that TickTick did not
// report, has the list "".
func (s *service) taskOf(t ticktick.Task) task {
	out := task{ID: t.ID, Title: t.Title, Notes: t.Content, Done: t.Status == 2}
	if s.hasList(t.ProjectID) {
		out.List = t.ProjectID
	}
	if due, ok := t.Due(); ok {
		out.Due = due.Format(dayFmt)
	}
	if day, at, ok := doneDay(t); ok {
		out.DoneDay, out.DoneAt = day, at.Format(time.RFC3339)
	}
	return out
}

func taskEntity(t task) *siren.Entity {
	return &siren.Entity{Class: []string{"task"}, Title: t.Title, Properties: t,
		Links: []siren.Link{{Rel: []string{"self"}, Href: "/tasks/" + t.ID, Title: t.Title}}}
}

// find reads a task by its id. TickTick wants the list too: the id is looked up where this service learned it, else in
// the open tasks of the lists, else in the done tasks. A task of the Inbox that this service did not make or see in
// this run is not found: TickTick does not list the Inbox.
func (s *service) find(ctx context.Context, id string) (ticktick.Task, error) {
	s.mu.Lock()
	projectID, known := s.project[id]
	s.mu.Unlock()
	if known {
		t, err := s.p.TT.Task(ctx, projectID, id)
		if err != nil {
			return t, s.writeError(err)
		}
		if t.ProjectID == "" {
			t.ProjectID = projectID
		}
		return t, nil
	}
	for _, l := range s.lists {
		tasks, err := s.openTasks(ctx, l)
		if err != nil {
			return ticktick.Task{}, err
		}
		for _, t := range tasks {
			if t.ID == id {
				if t.ProjectID == "" {
					t.ProjectID = l.ID
				}
				return t, nil
			}
		}
	}
	// A done task has left its list. TickTick lists it as completed.
	done, err := s.p.TT.Completed(ctx)
	if errors.Is(err, ticktick.ErrNoCompletedEndpoint) {
		return ticktick.Task{}, noTask()
	}
	if err != nil {
		return ticktick.Task{}, s.upstream(err)
	}
	for _, t := range done {
		if t.ID == id {
			if t.ProjectID != "" {
				s.remember(t.ProjectID, t)
			}
			return t, nil
		}
	}
	return ticktick.Task{}, noTask()
}

func (s *service) show(r *http.Request) (*siren.Entity, error) {
	t, err := s.find(r.Context(), r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	return taskEntity(s.taskOf(t)), nil
}

func (s *service) home(*http.Request) (*siren.Entity, error) {
	return &siren.Entity{Class: []string{"taskservice"}, Title: "Tasks"}, nil
}

func (s *service) showLists(*http.Request) (*siren.Entity, error) {
	type list struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	out := make([]list, 0, len(s.lists))
	for _, l := range s.lists {
		out = append(out, list{l.ID, l.Name})
	}
	return &siren.Entity{Class: []string{"lists"}, Properties: map[string]any{"lists": out}}, nil
}

// listOf is the list id of the query ?list=ID, "" when the query names none. An unknown list is 404.
func (s *service) listOf(r *http.Request) (string, error) {
	id := r.URL.Query().Get("list")
	if id != "" && !s.hasList(id) {
		return "", noList()
	}
	return id, nil
}

func tasksEntity(ts []task) *siren.Entity {
	if ts == nil {
		ts = []task{}
	}
	return &siren.Entity{Class: []string{"tasks"}, Properties: map[string]any{"tasks": ts}}
}

// openTasks reads the open tasks of a list and remembers where they are.
func (s *service) openTasks(ctx context.Context, l ticktick.Project) ([]ticktick.Task, error) {
	tasks, err := s.p.TT.ProjectData(ctx, l.ID)
	if err != nil {
		return nil, s.upstream(err)
	}
	s.remember(l.ID, tasks...)
	return tasks, nil
}

// open is the open tasks of one list, or of every list, list by list. TickTick does not list the Inbox.
func (s *service) open(r *http.Request) (*siren.Entity, error) {
	id, err := s.listOf(r)
	if err != nil {
		return nil, err
	}
	var out []task
	for _, l := range s.lists {
		if id != "" && l.ID != id {
			continue
		}
		tasks, err := s.openTasks(r.Context(), l)
		if err != nil {
			return nil, err
		}
		for _, t := range tasks {
			if t.ProjectID == "" {
				t.ProjectID = l.ID
			}
			out = append(out, s.taskOf(t))
		}
	}
	return tasksEntity(out), nil
}

// done is the tasks done on the day since or later. A server without /task/completed gives none.
func (s *service) done(r *http.Request) (*siren.Entity, error) {
	since := r.URL.Query().Get("since")
	if _, err := time.Parse(dayFmt, since); err != nil {
		return nil, invalid("since must be a day, YYYY-MM-DD")
	}
	id, err := s.listOf(r)
	if err != nil {
		return nil, err
	}
	all, err := s.p.TT.Completed(r.Context())
	if errors.Is(err, ticktick.ErrNoCompletedEndpoint) {
		return tasksEntity(nil), nil
	}
	if err != nil {
		return nil, s.upstream(err)
	}
	var out []task
	for _, t := range all {
		if t.ProjectID != "" {
			s.remember(t.ProjectID, t)
		}
		got := s.taskOf(t)
		if got.DoneDay != "" && got.DoneDay >= since && (id == "" || got.List == id) {
			out = append(out, got)
		}
	}
	return tasksEntity(out), nil
}

// zoneName is the name of the zone for a due day. It is the zone of the task when the name loads. Else it is the local
// zone: TZ (without a leading colon) when it loads, else the target of the link /etc/localtime after "zoneinfo/" when
// it loads, else "". The name "Local" is no zone name: TickTick would not know it. With "", the offset in dueDate
// carries the instant.
func zoneName(task, tz, link string) string {
	if loads(task) {
		return task
	}
	if tz = strings.TrimPrefix(tz, ":"); loads(tz) {
		return tz
	}
	if _, name, ok := strings.Cut(link, "zoneinfo/"); ok && loads(name) {
		return name
	}
	return ""
}

func loads(name string) bool {
	if name == "" || name == "Local" {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}

// zoneFor is zoneName with the inputs of this machine, and the zone itself (the local zone when there is no name).
func zoneFor(task string) (string, *time.Location) {
	link, _ := os.Readlink("/etc/localtime")
	name := zoneName(task, os.Getenv("TZ"), link)
	if name == "" {
		return "", time.Local
	}
	loc, err := time.LoadLocation(name)
	if err != nil { // zoneName has loaded it
		return "", time.Local
	}
	return name, loc
}

// dueFields are the fields of TickTick for a due day: one instant for dueDate and startDate, all day. A day that is ""
// clears them. taskZone is the zone of the task, "" for a new task. With no zone name, timeZone is not a field.
func dueFields(day, taskZone string) (fields map[string]any, err error) {
	if day == "" {
		return map[string]any{"dueDate": "", "startDate": "", "isAllDay": false}, nil
	}
	zone, loc := zoneFor(taskZone)
	d, err := time.ParseInLocation(dayFmt, day, loc)
	if err != nil {
		return nil, invalid("due must be a day, YYYY-MM-DD")
	}
	at := d.Format(ticktick.TimeLayout)
	fields = map[string]any{"dueDate": at, "startDate": at, "isAllDay": true}
	if zone != "" {
		fields["timeZone"] = zone
	}
	return fields, nil
}

func (s *service) add(r *http.Request) (*siren.Entity, error) {
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		return nil, invalid("title is required")
	}
	t := ticktick.Task{Title: title, Content: r.FormValue("notes"), ProjectID: r.FormValue("list")}
	if t.ProjectID != "" && !s.hasList(t.ProjectID) {
		return nil, invalid("no such list")
	}
	if due := r.FormValue("due"); due != "" {
		f, err := dueFields(due, "")
		if err != nil {
			return nil, err
		}
		t.DueDate, t.StartDate, t.IsAllDay = f["dueDate"].(string), f["startDate"].(string), true
		t.TimeZone, _ = f["timeZone"].(string)
	}
	made, err := s.p.TT.Create(r.Context(), t)
	if err != nil {
		return nil, s.upstream(err)
	}
	if made.ProjectID == "" {
		made.ProjectID = t.ProjectID
	}
	s.remember(made.ProjectID, made)
	return taskEntity(s.taskOf(made)), nil
}

// edit changes the fields that the request names. A field that is not sent stays as it is. An empty due or notes
// clears it.
func (s *service) edit(r *http.Request) (*siren.Entity, error) {
	if err := r.ParseForm(); err != nil {
		return nil, invalid("the form cannot be read")
	}
	t, err := s.find(r.Context(), r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	change := map[string]any{}
	if v, ok := r.PostForm["title"]; ok {
		if strings.TrimSpace(v[0]) == "" {
			return nil, invalid("title must not be blank")
		}
		change["title"] = v[0]
	}
	if v, ok := r.PostForm["notes"]; ok {
		change["content"] = v[0]
	}
	// A browser form sends every field. The day the task has already keeps its due as it is, with its time.
	if v, ok := r.PostForm["due"]; ok && v[0] != s.taskOf(t).Due {
		f, err := dueFields(v[0], t.TimeZone)
		if err != nil {
			return nil, err
		}
		maps.Copy(change, f)
	}
	if len(change) == 0 {
		return taskEntity(s.taskOf(t)), nil
	}
	return s.update(r.Context(), t, change)
}

// update sends the change to TickTick, which gets the whole task back (ticktick.Client.Update), and answers the task.
func (s *service) update(ctx context.Context, t ticktick.Task, change map[string]any) (*siren.Entity, error) {
	made, err := s.p.TT.Update(ctx, t.ProjectID, t.ID, change)
	if err != nil {
		return nil, s.writeError(err)
	}
	if made.ProjectID == "" {
		made.ProjectID = t.ProjectID
	}
	return taskEntity(s.taskOf(made)), nil
}

func (s *service) complete(r *http.Request) (*siren.Entity, error) {
	t, err := s.find(r.Context(), r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	if err := s.p.TT.Complete(r.Context(), t.ProjectID, t.ID); err != nil {
		return nil, s.writeError(err)
	}
	t.Status = 2
	return taskEntity(s.taskOf(t)), nil
}

// reopen is Update with status 0.
func (s *service) reopen(r *http.Request) (*siren.Entity, error) {
	t, err := s.find(r.Context(), r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	return s.update(r.Context(), t, map[string]any{"status": 0, "completedTime": nil})
}

// remove deletes the task in TickTick and answers it as it was.
func (s *service) remove(r *http.Request) (*siren.Entity, error) {
	t, err := s.find(r.Context(), r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	if err := s.p.TT.Delete(r.Context(), t.ProjectID, t.ID); err != nil {
		return nil, s.writeError(err)
	}
	s.forget(t.ID)
	return taskEntity(s.taskOf(t)), nil
}

// sync polls now. It waits when a poll is in progress (Poller.Poll).
func (s *service) sync(r *http.Request) (*siren.Entity, error) {
	n, err := s.p.Poll(r.Context())
	if err != nil {
		var se *ticktick.StatusError
		if errors.Is(err, ErrLoginAgain) || errors.As(err, &se) {
			return nil, s.upstream(err)
		}
		log.Printf("poll failed: %v", err) // the cause may name the task manager or hold a word of the owner: the log only
		return nil, &app.Error{Status: http.StatusBadGateway, Code: siren.CodeUpstream, Msg: "the poll failed"}
	}
	return &siren.Entity{Class: []string{"sync"}, Title: "Sync", Properties: map[string]any{"written": n}}, nil
}

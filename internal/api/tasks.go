package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"lifelog/client"
	"lifelog/internal/core"
	"lifelog/siren"
)

// The task service (docs/plans/158-one-task-service-behind-tasks.md) is the one child of `serve --with` whose catalog
// has the class tasks. It answers the small protocol of extensions/README.md, "A task service": the lists, the open
// and the done tasks, and six writes. This file builds the pages of Tasks from those answers and forwards the six
// actions to the service. The server names no service and no provider: the protocol is the whole interface, and the
// file keeps no task (docs/decisions/D23-no-tasks.md).

// taskList is a list of the task service: its id for the protocol, its name for the owner.
type taskList struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// taskListsOf reads the lists from the answer of GET /lists of the task service.
func taskListsOf(e *siren.Entity) ([]taskList, error) {
	var p struct {
		Lists []taskList `json:"lists"`
	}
	if err := propertiesOf(e, &p); err != nil {
		return nil, fmt.Errorf("the lists: %w", err)
	}
	ids, names := map[string]bool{}, map[string]bool{}
	for i, l := range p.Lists {
		if l.ID == "" || l.Name == "" {
			return nil, fmt.Errorf("the lists: a list has no id or no name")
		}
		if ids[l.ID] {
			return nil, fmt.Errorf("the lists: the id %q is in two lists", l.ID)
		}
		ids[l.ID] = true
		// add-task names a list by its name, so each list needs its own name.
		name := l.Name
		for n := 2; names[name]; n++ {
			name = fmt.Sprintf("%s (%d)", l.Name, n)
		}
		names[name] = true
		p.Lists[i].Name = name
	}
	return p.Lists, nil
}

// propertiesOf reads the properties of an entity into v, as the JSON they came from.
func propertiesOf(e *siren.Entity, v any) error {
	b, err := json.Marshal(e.Properties)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// svcTask is a task as the protocol gives it. Due and DoneDay are days in the zone of the task. DoneAt is an RFC 3339
// instant. List is the id of the list, and "" for the default list.
type svcTask struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	List    string `json:"list"`
	Notes   string `json:"notes"`
	Due     string `json:"due"`
	Done    bool   `json:"done"`
	DoneDay string `json:"done_day"`
	DoneAt  string `json:"done_at"`
}

// taskCatalog are the six actions of Tasks, as rows of the merged catalog. They are in GET /actions while a task
// service runs (taskService.specs). Path is the route of this server, and Service is the href of the same write at the
// service (extensions/README.md, "A task service"): setTasks refuses a service that lacks one. The options of the
// field list are the names of the lists, which only the service knows, so the row has none here.
var taskCatalog = []spec{
	{Name: "add-task", Title: "Add a task", Class: []string{"create"}, Method: "POST", Path: "/tasks", Service: "/tasks",
		Description: "Make a task in the task service. list is the name of a list (the default list when absent). due is a day, YYYY-MM-DD.",
		Hint:        "Make a task.",
		Fields:      []siren.Field{req("title", "text", "Title"), opt("list", "text", "List"), opt("due", "date", "Due day"), area("notes", "Notes", 4)}},
	{Name: "edit-task", Title: "Edit a task", Class: []string{"edit"}, Method: "POST", Path: "/tasks/items/{id}", Service: "/tasks/{id}",
		Description: "Change the title, the notes or the due day of a task. A field that is not sent stays as it is. An empty due or notes clears it.",
		Hint:        "Change the title, the notes or the due day of this task. Leave the notes or the due day empty to remove it.",
		Fields: []siren.Field{path("id", "text", "Task"), opt("title", "text", "Title"), area("notes", "Notes", 4),
			opt("due", "date", "Due day")}},
	{Name: "complete-task", Title: "Complete a task", Class: []string{"toggle"}, Method: "POST", Path: "/tasks/items/{id}/complete", Service: "/tasks/{id}/complete",
		Description: "Mark a task as done.",
		Fields:      []siren.Field{path("id", "text", "Task")}},
	{Name: "reopen-task", Title: "Reopen a task", Class: []string{"toggle"}, Method: "POST", Path: "/tasks/items/{id}/reopen", Service: "/tasks/{id}/reopen",
		Description: "Mark a done task as open again.",
		Fields:      []siren.Field{path("id", "text", "Task")}},
	{Name: "delete-task", Title: "Delete a task", Method: "POST", Path: "/tasks/items/{id}/delete", Service: "/tasks/{id}/delete", Owner: true, Danger: true,
		Description: "Delete a task in the task service. The service cannot give it back.",
		Fields:      []siren.Field{path("id", "text", "Task")}},
	{Name: "sync-tasks", Title: "Sync the done tasks", Method: "POST", Path: "/tasks/sync", Service: "/sync",
		Description: "Ask the task service to write the tasks that are done as entries of their days, once each."},
}

// taskService is the task service that runs: its name (for the registry and the log; a client never sees it), the client of its loopback
// address and the lists it reported when it registered. A new list needs a restart of the service.
type taskService struct {
	name  string
	c     *client.Client
	lists []taskList
}

// spec is the row of the action with that name, as the service offers it. The names are the six of taskCatalog. The
// field list of add-task gets the names of the lists as its options. That row has a copy of its fields, so the row of
// taskCatalog does not change. The other rows are the rows of taskCatalog, and a page that draws many tasks copies
// nothing for them.
func (s taskService) spec(name string) spec {
	row := taskCatalog[slices.IndexFunc(taskCatalog, func(x spec) bool { return x.Name == name })]
	if i := slices.IndexFunc(row.Fields, func(f siren.Field) bool { return f.Name == "list" }); i >= 0 {
		row.Fields = slices.Clone(row.Fields)
		row.Fields[i].Options = listNames(s.lists)
	}
	return row
}

// specs are the six actions, each as spec gives it.
func (s taskService) specs() []spec {
	out := make([]spec, len(taskCatalog))
	for i, row := range taskCatalog {
		out[i] = s.spec(row.Name)
	}
	return out
}

// action is the action as a page draws it: path fields filled from params, other fields with the values.
func (s taskService) action(name string, params map[string]string, values map[string]any) siren.Action {
	return filled(s.spec(name), params, values)
}

func (s taskService) listName(id string) string {
	for _, l := range s.lists {
		if l.ID == id {
			return l.Name
		}
	}
	return ""
}

func (s taskService) hasList(id string) bool { return s.listName(id) != "" }

func taskHref(id string) string     { return "/tasks/items/" + url.PathEscape(id) }
func taskListHref(id string) string { return "/tasks/lists/" + url.PathEscape(id) }

// links are the links of every page: self, then the side column of the generic view (Today, Next 7 days, one link for
// each list, Done), then the links that only this page has. counts are the open tasks of each list by list id. A page
// that did not read every list passes nil, and the lists show no count.
func (s taskService) links(href, title string, counts map[string]int, extra ...siren.Link) []siren.Link {
	links := []siren.Link{
		{Rel: []string{"self"}, Href: href, Title: title},
		{Rel: []string{"nav"}, Href: "/tasks", Title: "Today"},
		{Rel: []string{"nav"}, Href: "/tasks/week", Title: "Next 7 days"},
	}
	for _, l := range s.lists {
		name := l.Name
		if n, ok := counts[l.ID]; ok {
			name = fmt.Sprintf("%s · %d", name, n)
		}
		links = append(links, siren.Link{Rel: []string{"nav"}, Class: []string{"list"}, Href: taskListHref(l.ID), Title: name})
	}
	links = append(links, siren.Link{Rel: []string{"done"}, Href: "/tasks/done", Title: "Done"})
	return append(links, extra...)
}

// row is a task as a row of a page. The chips are its due day, its done day and, when the rows of many lists are
// mixed (withList), its list. An open task has the round button Complete, a done task the round button Reopen.
func (s taskService) row(t svcTask, withList bool) siren.Link {
	row := siren.Link{Rel: []string{"item"}, Class: []string{"task"}, Href: taskHref(t.ID), Title: t.Title}
	props := map[string]any{}
	if t.Due != "" {
		props["due"] = t.Due
	}
	if name := s.listName(t.List); withList && name != "" {
		props["list"] = name
	}
	if t.Done && t.DoneDay != "" {
		props["done"] = t.DoneDay
	}
	if len(props) > 0 {
		row.Properties = props
	}
	next, word := "complete-task", "Complete"
	if t.Done {
		next, word = "reopen-task", "Reopen"
	}
	button := s.action(next, map[string]string{"id": t.ID}, nil)
	button.Title = word // the accessible name of the round button: the row says which task
	row.Actions = []siren.Action{button}
	return row
}

func (s taskService) rows(tasks []svcTask, withList bool) []siren.Link {
	var rows []siren.Link
	for _, t := range tasks {
		rows = append(rows, s.row(t, withList))
	}
	return rows
}

// page is one task as a page: its properties, the side column, and the forms Edit (filled with the current values) and
// Delete. Complete and Reopen are the round buttons of the rows only (docs/plans/131-the-pareto-pass.md: one way).
func (s taskService) page(t svcTask) *siren.Entity {
	props := map[string]any{"id": t.ID, "title": t.Title, "done": t.Done}
	if name := s.listName(t.List); name != "" {
		props["list"] = name
	}
	for k, v := range map[string]string{"notes": t.Notes, "due": t.Due, "done_day": t.DoneDay} {
		if v != "" {
			props[k] = v
		}
	}
	links := s.links(taskHref(t.ID), t.Title, nil)
	for i := range links { // the link of the list of the task is also its rel list; the default list has no page
		if s.hasList(t.List) && links[i].Href == taskListHref(t.List) {
			links[i].Rel = append(links[i].Rel, "list")
		}
	}
	id := map[string]string{"id": t.ID}
	return &siren.Entity{Class: []string{"task"}, Title: t.Title, Properties: props, Links: links,
		Actions: []siren.Action{
			s.action("edit-task", id, map[string]any{"title": t.Title, "notes": t.Notes, "due": t.Due}),
			s.action("delete-task", id, nil)}}
}

// get reads href of the service in the name of the writer of r.
func (s taskService) get(ctx context.Context, r *http.Request, href string) (*siren.Entity, error) {
	e, err := s.c.As(source(r)).Get(ctx, href)
	if err != nil {
		return nil, forwardError(r, "the task service", err)
	}
	return e, nil
}

// tasks reads the tasks that href of the service lists (properties.tasks).
func (s taskService) tasks(ctx context.Context, r *http.Request, href string) ([]svcTask, error) {
	e, err := s.get(ctx, r, href)
	if err != nil {
		return nil, err
	}
	var p struct {
		Tasks []svcTask `json:"tasks"`
	}
	if err := propertiesOf(e, &p); err != nil {
		return nil, forwardError(r, "the task service", err)
	}
	return p.Tasks, nil
}

// open reads the open tasks of a list, or of every list when listID is "".
func (s taskService) open(ctx context.Context, r *http.Request, listID string) ([]svcTask, error) {
	href := "/open"
	if listID != "" {
		href += "?list=" + url.QueryEscape(listID)
	}
	return s.tasks(ctx, r, href)
}

// done reads the tasks done on the day since or later, newest first. listID limits them to one list; "" is all.
func (s taskService) done(ctx context.Context, r *http.Request, since, listID string) ([]svcTask, error) {
	href := "/done?since=" + url.QueryEscape(since)
	if listID != "" {
		href += "&list=" + url.QueryEscape(listID)
	}
	tasks, err := s.tasks(ctx, r, href)
	for i := range tasks {
		tasks[i].Done = true // the list says so, and the row draws Reopen by it
	}
	sortDone(tasks)
	return tasks, err
}

// post sends a write to the service in the name of the writer of r.
func (s taskService) post(ctx context.Context, r *http.Request, href string, values map[string]string) (*siren.Entity, error) {
	e, err := s.c.As(source(r)).Do(ctx, siren.Action{Method: "POST", Href: href}, values)
	if err != nil {
		return nil, forwardError(r, "the task service", err)
	}
	return e, nil
}

// doneWindow is how many days back the Done pages look.
const doneWindow = 30

// taskDays are the days that the pages need, in the zone of the owner.
type taskDays struct {
	Today     string // the day of now
	WeekLast  string // today + 6: the last day of Next 7 days
	DoneSince string // today - doneWindow: the first day of Done
}

// daysOf gives the days of now. The caller gives the clock of the store, so the zone is the owner's and not the
// zone of the service.
func daysOf(now time.Time) taskDays {
	return taskDays{Today: now.Format(time.DateOnly), WeekLast: now.AddDate(0, 0, 6).Format(time.DateOnly),
		DoneSince: now.AddDate(0, 0, -doneWindow).Format(time.DateOnly)}
}

// dueBetween are the tasks with a due day from first to last, both included. first "" has no lower bound. A task with
// no due day is never in the answer.
func dueBetween(tasks []svcTask, first, last string) []svcTask {
	var out []svcTask
	for _, t := range tasks {
		if t.Due != "" && t.Due >= first && t.Due <= last {
			out = append(out, t)
		}
	}
	return out
}

// overdue counts the tasks whose due day is before today.
func overdue(tasks []svcTask, today string) int {
	n := 0
	for _, t := range tasks {
		if t.Due != "" && t.Due < today {
			n++
		}
	}
	return n
}

// sortOpen sorts open tasks by due day, and a task with no due day goes last. Tasks that tie keep the order of the
// service. There is no other key: the protocol has no priority.
func sortOpen(tasks []svcTask) {
	slices.SortStableFunc(tasks, func(a, b svcTask) int {
		switch {
		case a.Due == b.Due:
			return 0
		case a.Due == "":
			return 1
		case b.Due == "":
			return -1
		}
		return strings.Compare(a.Due, b.Due)
	})
}

// sortDone sorts done tasks, the newest first. The instant is done_at; a task with no valid instant counts from the
// start of its done day, and a task with neither goes last. Tasks that tie keep the order of the service.
func sortDone(tasks []svcTask) {
	at := func(t svcTask) time.Time {
		if x, err := time.Parse(time.RFC3339, t.DoneAt); err == nil {
			return x
		}
		x, _ := time.Parse(time.DateOnly, t.DoneDay) // the zero time when the day is not valid
		return x
	}
	slices.SortStableFunc(tasks, func(a, b svcTask) int { return at(b).Compare(at(a)) })
}

// noTaskService is the answer of every route of Tasks while no task service runs.
func noTaskService() error {
	return &core.Error{Status: http.StatusServiceUnavailable, Code: siren.CodeUnavailable, Msg: "no task service runs"}
}

// mountTasks gives m the routes of Tasks: the pages, and the six actions. The routes are always there; each answers
// 503 while no task service runs. They go through serve, so an error is an error entity.
func (h *server) mountTasks(m *http.ServeMux) {
	m.HandleFunc("GET /tasks", h.serve(h.tasksToday))
	m.HandleFunc("GET /tasks/week", h.serve(h.tasksWeek))
	m.HandleFunc("GET /tasks/lists/{id}", h.serve(h.tasksList))
	m.HandleFunc("GET /tasks/lists/{id}/done", h.serve(h.tasksListDone))
	m.HandleFunc("GET /tasks/done", h.serve(h.tasksDone))
	m.HandleFunc("GET /tasks/items/{id}", h.serve(h.tasksItem))
	for _, s := range taskCatalog {
		m.HandleFunc(s.Method+" "+s.Path, h.serve(h.taskWrite(s.Name)))
	}
}

// withTasks runs f with the task service, or answers 503. The calls of f to the service share one deadline of
// forwardTimeout.
func (h *server) withTasks(r *http.Request, f func(ctx context.Context, svc taskService) (*siren.Entity, error)) (*siren.Entity, error) {
	svc, ok := h.with.taskService()
	if !ok {
		return nil, noTaskService()
	}
	ctx, cancel := context.WithTimeout(r.Context(), forwardTimeout)
	defer cancel()
	return f(ctx, svc)
}

// tasksToday is Today: the open tasks due today or before, across the lists, with the form add-task (due today).
func (h *server) tasksToday(r *http.Request) (*siren.Entity, error) {
	return h.withTasks(r, func(ctx context.Context, svc taskService) (*siren.Entity, error) {
		days := daysOf(h.s.Local())
		all, err := svc.open(ctx, r, "")
		if err != nil {
			return nil, err
		}
		counts := map[string]int{}
		for _, t := range all {
			counts[t.List]++
		}
		due := dueBetween(all, "", days.Today)
		sortOpen(due)
		return &siren.Entity{Class: []string{"tasks"}, Title: "Today",
			Properties: map[string]any{"open": len(all), "overdue": overdue(due, days.Today)}, Entities: svc.rows(due, true),
			Actions: []siren.Action{svc.action("add-task", nil, map[string]any{"due": days.Today}), svc.action("sync-tasks", nil, nil)},
			Links:   svc.links("/tasks", "Today", counts)}, nil
	})
}

// tasksWeek is Next 7 days: the open tasks due from today to six days later, across the lists.
func (h *server) tasksWeek(r *http.Request) (*siren.Entity, error) {
	return h.withTasks(r, func(ctx context.Context, svc taskService) (*siren.Entity, error) {
		days := daysOf(h.s.Local())
		all, err := svc.open(ctx, r, "")
		if err != nil {
			return nil, err
		}
		week := dueBetween(all, days.Today, days.WeekLast)
		sortOpen(week)
		return &siren.Entity{Class: []string{"week"}, Title: "Next 7 days",
			Properties: map[string]any{"open": len(week)}, Entities: svc.rows(week, true),
			Actions: []siren.Action{svc.action("add-task", nil, nil)},
			Links:   svc.links("/tasks/week", "Next 7 days", nil)}, nil
	})
}

// listOf is the list of the request, or 404.
func (s taskService) listOf(r *http.Request) (taskList, error) {
	i := slices.IndexFunc(s.lists, func(l taskList) bool { return l.ID == r.PathValue("id") })
	if i < 0 {
		return taskList{}, core.NotFound("no such list %s", r.PathValue("id"))
	}
	return s.lists[i], nil
}

// tasksList is the open tasks of one list, with add-task and the list filled in.
func (h *server) tasksList(r *http.Request) (*siren.Entity, error) {
	return h.withTasks(r, func(ctx context.Context, svc taskService) (*siren.Entity, error) {
		l, err := svc.listOf(r)
		if err != nil {
			return nil, err
		}
		tasks, err := svc.open(ctx, r, l.ID)
		if err != nil {
			return nil, err
		}
		today := daysOf(h.s.Local()).Today
		late := overdue(tasks, today)
		sortOpen(tasks)
		done := siren.Link{Rel: []string{"done"}, Href: taskListHref(l.ID) + "/done", Title: "Done in this list"}
		return &siren.Entity{Class: []string{"list"}, Title: l.Name,
			Properties: map[string]any{"open": len(tasks), "overdue": late}, Entities: svc.rows(tasks, false),
			Actions: []siren.Action{svc.action("add-task", nil, map[string]any{"list": l.Name})},
			Links:   svc.links(taskListHref(l.ID), l.Name, nil, done)}, nil
	})
}

// tasksDone is the tasks done in the last doneWindow days, newest first, across the lists.
func (h *server) tasksDone(r *http.Request) (*siren.Entity, error) {
	return h.withTasks(r, func(ctx context.Context, svc taskService) (*siren.Entity, error) {
		return h.donePage(ctx, r, svc, "", "/tasks/done", "Done")
	})
}

// tasksListDone is the same for one list. The href and the title come from the list.
func (h *server) tasksListDone(r *http.Request) (*siren.Entity, error) {
	return h.withTasks(r, func(ctx context.Context, svc taskService) (*siren.Entity, error) {
		l, err := svc.listOf(r)
		if err != nil {
			return nil, err
		}
		return h.donePage(ctx, r, svc, l.ID, taskListHref(l.ID)+"/done", "Done · "+l.Name)
	})
}

// donePage is a page of done tasks. listID limits it to one list; "" is every list, and then each row names its list.
func (h *server) donePage(ctx context.Context, r *http.Request, svc taskService, listID, href, title string) (*siren.Entity, error) {
	tasks, err := svc.done(ctx, r, daysOf(h.s.Local()).DoneSince, listID)
	if err != nil {
		return nil, err
	}
	return &siren.Entity{Class: []string{"done"}, Title: title, Properties: map[string]any{"count": len(tasks)},
		Entities: svc.rows(tasks, listID == ""), Links: svc.links(href, title, nil)}, nil
}

// tasksItem is one task.
func (h *server) tasksItem(r *http.Request) (*siren.Entity, error) {
	return h.withTasks(r, func(ctx context.Context, svc taskService) (*siren.Entity, error) {
		e, err := svc.get(ctx, r, "/tasks/"+url.PathEscape(r.PathValue("id")))
		if err != nil {
			return nil, err
		}
		return svc.pageOf(r, e)
	})
}

// pageOf is the page of the task that the service answered.
func (s taskService) pageOf(r *http.Request, e *siren.Entity) (*siren.Entity, error) {
	var t svcTask
	if err := propertiesOf(e, &t); err != nil || t.ID == "" {
		return nil, forwardError(r, "the task service", fmt.Errorf("the answer is not a task: %v", err))
	}
	return s.page(t), nil
}

// taskWrite is the handler of the action with that name. It admits the request against the row of the action as every
// action of the catalog is admitted, then sends the fields that the request names to the service: a field that is not
// sent stays as it is, which is what edit-task needs.
func (h *server) taskWrite(name string) func(*http.Request) (*siren.Entity, error) {
	return func(r *http.Request) (*siren.Entity, error) {
		return h.withTasks(r, func(ctx context.Context, svc taskService) (*siren.Entity, error) {
			row := svc.spec(name)
			a, err := admit(r, row)
			if err != nil {
				return nil, err
			}
			values := a.sent()
			if err := svc.checkValues(values); err != nil {
				return nil, err
			}
			href := strings.ReplaceAll(row.Service, "{id}", url.PathEscape(r.PathValue("id")))
			switch name {
			case "sync-tasks":
				return svc.synced(ctx, r, href)
			case "delete-task":
				return svc.deleted(ctx, r, href)
			}
			e, err := svc.post(ctx, r, href, values)
			if err != nil {
				return nil, err
			}
			return svc.pageOf(r, e)
		})
	}
}

// checkValues refuses the values of a write that the protocol does not take, and gives the list its id. The protocol
// takes a title that is not blank, a due that is a day (or empty, which clears it) and a list that is an id.
func (s taskService) checkValues(values map[string]string) error {
	if t, ok := values["title"]; ok && strings.TrimSpace(t) == "" {
		return core.Invalid("title must not be blank")
	}
	if due := values["due"]; due != "" {
		if _, err := time.Parse(time.DateOnly, due); err != nil {
			return core.Invalid("due must be a day, YYYY-MM-DD")
		}
	}
	if name, ok := values["list"]; ok && name != "" {
		i := slices.IndexFunc(s.lists, func(l taskList) bool { return l.Name == name })
		if i < 0 { // fieldsOf checks the options, but a service with no list gives no options
			return core.Invalid("list must be one of %s", strings.Join(listNames(s.lists), ", "))
		}
		values["list"] = s.lists[i].ID
	} else if ok {
		delete(values, "list") // an empty list is the default list: the protocol takes no list for it
	}
	return nil
}

func listNames(lists []taskList) []string {
	names := make([]string, 0, len(lists))
	for _, l := range lists {
		names = append(names, l.Name)
	}
	return names
}

// synced is the answer of sync-tasks: how many Done entries the service wrote. Its self link is Today, where a UI
// returns.
func (s taskService) synced(ctx context.Context, r *http.Request, href string) (*siren.Entity, error) {
	e, err := s.post(ctx, r, href, nil)
	if err != nil {
		return nil, err
	}
	var p struct {
		Written int `json:"written"`
	}
	if err := propertiesOf(e, &p); err != nil {
		return nil, forwardError(r, "the task service", err)
	}
	return &siren.Entity{Class: []string{"tasks"}, Title: "Today", Properties: map[string]any{"written": p.Written},
		Links: []siren.Link{{Rel: []string{"self"}, Href: "/tasks", Title: "Today"}}}, nil
}

// deleted is the answer of delete-task. The task has no page now, so the self link is Today and a UI returns
// there; the properties are the task as it was.
func (s taskService) deleted(ctx context.Context, r *http.Request, href string) (*siren.Entity, error) {
	e, err := s.post(ctx, r, href, nil)
	if err != nil {
		return nil, err
	}
	var t svcTask
	if err := propertiesOf(e, &t); err != nil {
		return nil, forwardError(r, "the task service", err)
	}
	return &siren.Entity{Class: []string{"task"}, Title: t.Title, Properties: map[string]any{"id": t.ID, "title": t.Title, "deleted": true},
		Links: []siren.Link{{Rel: []string{"self"}, Href: "/tasks", Title: "Today"}}}, nil
}

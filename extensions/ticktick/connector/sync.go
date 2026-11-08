package connector

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"aminte-ticktick/ticktick"
	"aminte/client"
	"aminte/siren"
)

const (
	// firstWindow is how far back the first poll looks. A first sync does not import a lifetime: the owner's log
	// starts where the connector starts.
	firstWindow = 30 * 24 * time.Hour
	// overlap is how far the cursor stays behind the newest done task. A resend writes nothing (import_key), so the
	// overlap costs one request and covers a task that TickTick shows late.
	overlap = 24 * time.Hour
)

// entry is one capture: a done task as a line of its day.
type entry struct {
	id, day, text, key string
	at                 time.Time
}

// doneDay is the day of the completion of a task, YYYY-MM-DD, and the instant. The day is the day of completedTime in
// the zone of the task. It is false when the task has no completion time. The task service and the entries share this
// one rule.
func doneDay(t ticktick.Task) (string, time.Time, bool) {
	at, ok := t.Done()
	if !ok {
		return "", time.Time{}, false
	}
	return at.Format(dayFmt), at, true
}

// entryOf maps a done task to its capture. The day is the day of completedTime in the zone of the task. The key is the
// id and the completedTime as TickTick gives it, so a repeat task done twice is two entries and a resend is one.
// names holds the lists that TickTick reported: a task in the Inbox, or in a list that is not there, has no list name.
func entryOf(t ticktick.Task, names map[string]string) (entry, bool) {
	day, at, ok := doneDay(t)
	if !ok {
		return entry{}, false
	}
	text := "Done: " + t.Title
	if name, known := names[t.ProjectID]; known && !strings.HasPrefix(t.ProjectID, "inbox") {
		text += " · " + name
	}
	return entry{id: t.ID, day: day, text: text, key: t.ID + ":" + t.CompletedTime, at: at}, true
}

// Sync does one poll. It asks TickTick for the tasks done since the cursor and writes each as a capture on its day.
// It returns the number of entries that were new. It changes st only when it succeeds, so a failed poll is asked
// again from the same cursor. lf must write as siren.ImportPrefix+"ticktick".
func Sync(ctx context.Context, tt *ticktick.Client, lf *client.Client, st *State, now time.Time) (int, error) {
	actions, err := lf.Catalog(ctx)
	if err != nil {
		return 0, fmt.Errorf("the catalog of aminte: %w", err)
	}
	var capture siren.Action
	for _, a := range actions {
		if a.Name == "capture" {
			capture = a
		}
	}
	if capture.Name == "" {
		return 0, errors.New("the catalog of aminte has no capture action")
	}
	lists, err := tt.Projects(ctx)
	if err != nil {
		return 0, err
	}
	names := map[string]string{}
	for _, l := range lists {
		names[l.ID] = l.Name
	}

	var done []ticktick.Task
	seen := st.Seen
	if !st.fetchOnly {
		since := st.Cursor
		if since.IsZero() {
			since = now.Add(-firstWindow)
		}
		done, err = tt.Completed(ctx)
		if err == nil { // the server lists every done task it has: the cursor is applied here
			done = slices.DeleteFunc(done, func(t ticktick.Task) bool {
				at, ok := t.Done()
				return !ok || at.Before(since)
			})
		}
		if errors.Is(err, ticktick.ErrNoCompletedEndpoint) {
			log.Print("the server has no /task/completed: finding done tasks by the ones that left their lists")
			st.fetchOnly, err = true, nil
		}
		if err != nil {
			return 0, err
		}
	}
	if st.fetchOnly {
		if done, seen, err = vanished(ctx, tt, lists, st.Seen); err != nil {
			return 0, err
		}
	}

	var entries []entry
	var newest time.Time
	for _, t := range done {
		e, ok := entryOf(t, names)
		if !ok {
			continue
		}
		entries = append(entries, e)
		if e.at.After(newest) {
			newest = e.at
		}
	}
	slices.SortStableFunc(entries, func(a, b entry) int { return a.at.Compare(b.at) })
	written := 0
	for _, e := range entries {
		already, err := write(ctx, lf, capture, e)
		if err != nil {
			return written, fmt.Errorf("task %s: %w", e.id, err)
		}
		if !already {
			written++
		}
	}

	st.Seen = seen
	if c := newest.Add(-overlap); c.After(st.Cursor) {
		st.Cursor = c
	}
	return written, nil
}

// write sends one capture. It reports whether aminte had the entry already. An error never holds the words of the
// entry or of aminte's answer.
func write(ctx context.Context, lf *client.Client, capture siren.Action, e entry) (already bool, err error) {
	res, err := lf.Do(ctx, capture, map[string]string{"day": e.day, "text": e.text, "import_key": e.key})
	var ce *client.Error
	if errors.As(err, &ce) {
		return false, fmt.Errorf("aminte answered %d %s", ce.Status, ce.Code())
	}
	if err != nil {
		return false, err
	}
	if m, ok := res.Result.(map[string]any); ok && m["already"] == true && slices.Contains(res.Class, "result") {
		return true, nil
	}
	return false, nil
}

// vanished is the way for a server that has no /task/completed. It reads the open tasks of every list. A task that was
// open at the last poll and is not now was done, deleted or moved: it is read by its id. Status 2 is a done task, a 404
// is a deleted one. seen is the open tasks now.
func vanished(ctx context.Context, tt *ticktick.Client, lists []ticktick.Project, before map[string]string) (done []ticktick.Task, seen map[string]string, err error) {
	seen = map[string]string{}
	for _, l := range lists {
		tasks, err := tt.ProjectData(ctx, l.ID)
		if err != nil {
			return nil, nil, err
		}
		for _, t := range tasks {
			seen[t.ID] = l.ID
		}
	}
	for id, project := range before {
		if _, open := seen[id]; open {
			continue
		}
		t, err := tt.Task(ctx, project, id)
		var se *ticktick.StatusError
		if errors.As(err, &se) && se.Status == http.StatusNotFound {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		if t.Status == 2 {
			if t.ProjectID == "" {
				t.ProjectID = project
			}
			done = append(done, t)
		}
	}
	return done, seen, nil
}

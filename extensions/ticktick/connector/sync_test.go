package connector

import (
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"aminte-ticktick/ticktick"
)

func TestEntryOfMapsATaskToItsDayTextAndKey(t *testing.T) {
	names := map[string]string{"p-home": "Home", "inbox1": "ignored: an Inbox has no suffix"}
	tests := []struct {
		name string
		task ticktick.Task
		want entry
		ok   bool
	}{
		{"a list adds its name after a middle dot",
			ticktick.Task{ID: "t1", ProjectID: "p-home", Title: "Pay rent", CompletedTime: "2030-03-01T10:00:00.000+0000", TimeZone: "UTC"},
			entry{day: "2030-03-01", text: "Done: Pay rent · Home", key: "t1:2030-03-01T10:00:00.000+0000"}, true},
		{"an id that starts with inbox is the Inbox",
			ticktick.Task{ID: "t2", ProjectID: "inbox1", Title: "Milk", CompletedTime: "2030-03-01T10:00:00.000+0000", TimeZone: "UTC"},
			entry{day: "2030-03-01", text: "Done: Milk", key: "t2:2030-03-01T10:00:00.000+0000"}, true},
		{"a list that TickTick did not report is the Inbox",
			ticktick.Task{ID: "t3", ProjectID: "p-gone", Title: "Call", CompletedTime: "2030-03-01T10:00:00.000+0000", TimeZone: "UTC"},
			entry{day: "2030-03-01", text: "Done: Call", key: "t3:2030-03-01T10:00:00.000+0000"}, true},
		{"23:30 UTC is already the next day in Tokyo",
			ticktick.Task{ID: "t4", ProjectID: "p-home", Title: "Late", CompletedTime: "2030-03-01T23:30:00.000+0000", TimeZone: "Asia/Tokyo"},
			entry{day: "2030-03-02", text: "Done: Late · Home", key: "t4:2030-03-01T23:30:00.000+0000"}, true},
		{"03:00 UTC is still the day before in Los Angeles",
			ticktick.Task{ID: "t5", ProjectID: "p-home", Title: "Early", CompletedTime: "2030-03-02T03:00:00.000+0000", TimeZone: "America/Los_Angeles"},
			entry{day: "2030-03-01", text: "Done: Early · Home", key: "t5:2030-03-02T03:00:00.000+0000"}, true},
		{"a task with no completedTime is not an entry", ticktick.Task{ID: "t6", Title: "Open"}, entry{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := entryOf(tc.task, names)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if got.day != tc.want.day || got.text != tc.want.text || got.key != tc.want.key {
				t.Errorf("entry = %q %q %q, want %q %q %q", got.day, got.text, got.key, tc.want.day, tc.want.text, tc.want.key)
			}
		})
	}
}

func TestEntryOfWithoutAZoneUsesTheLocalZone(t *testing.T) {
	task := ticktick.Task{ID: "t", Title: "X", CompletedTime: "2030-03-01T12:00:00.000+0000"}
	got, _ := entryOf(task, nil)
	want := time.Date(2030, 3, 1, 12, 0, 0, 0, time.UTC).In(time.Local).Format("2006-01-02")
	if got.day != want {
		t.Errorf("day = %s, want %s", got.day, want)
	}
}

var now = time.Date(2030, 3, 5, 12, 0, 0, 0, time.UTC)

func TestSyncWritesADoneTaskOnceAndMovesTheCursor(t *testing.T) {
	tick, tt := newFakeTick(t)
	life, ll := newFakeLife(t)
	tick.add("t1", "p-home", "Pay rent")
	tick.add("t2", "p-work", "Open task")
	done := now.Add(-3 * time.Hour)
	tick.finish("t1", done)
	st := &State{Seen: map[string]string{}}

	n, err := Sync(t.Context(), tt, ll, st, now)
	if err != nil || n != 1 {
		t.Fatalf("Sync = %d, %v, want 1, nil", n, err)
	}
	want := capture{Day: "2030-03-05", Text: "Done: Pay rent · Home", Key: "t1:" + done.Format(ticktick.TimeLayout)}
	if got := life.got(); len(got) != 1 || got[0] != want {
		t.Errorf("captures = %+v, want [%+v]", got, want)
	}
	if life.source != "import:ticktick" {
		t.Errorf("writer = %q, want import:ticktick", life.source)
	}
	if wantCursor := done.Add(-24 * time.Hour); !st.Cursor.Equal(wantCursor) {
		t.Errorf("cursor = %v, want %v (the newest completion minus one day)", st.Cursor, wantCursor)
	}

	n, err = Sync(t.Context(), tt, ll, st, now.Add(time.Minute))
	if err != nil || n != 0 {
		t.Errorf("the second Sync = %d, %v, want 0, nil", n, err)
	}
	if len(life.got()) != 1 {
		t.Errorf("a resend wrote again: %+v", life.got())
	}
}

func TestTheFirstSyncKeepsTheLastThirtyDaysAndSendsNoFilter(t *testing.T) {
	tick, tt := newFakeTick(t)
	life, ll := newFakeLife(t)
	tick.add("old", "p-home", "Old")
	tick.finish("old", now.Add(-31*24*time.Hour))
	tick.add("new", "p-home", "New")
	tick.finish("new", now.Add(-29*24*time.Hour))
	n, err := Sync(t.Context(), tt, ll, &State{Seen: map[string]string{}}, now)
	if err != nil || n != 1 {
		t.Fatalf("Sync = %d, %v, want 1, nil (the task done 29 days ago, not the one done 31 days ago)", n, err)
	}
	if got := life.got(); len(got) != 1 || !strings.Contains(got[0].Text, "New") {
		t.Errorf("written = %+v, want the one task New", got)
	}
	// The live server answers nothing to a date filter: the body carries none, and the cursor is applied here.
	if strings.TrimSpace(tick.completedBody) != "{}" {
		t.Errorf("body = %s, want {}", tick.completedBody)
	}
}

func TestTheCursorNeverMovesBackAndTheNextAskStartsAtIt(t *testing.T) {
	tick, tt := newFakeTick(t)
	_, ll := newFakeLife(t)
	tick.add("t1", "p-home", "A")
	tick.finish("t1", now.Add(-2*time.Hour))
	st := &State{Seen: map[string]string{}}
	if _, err := Sync(t.Context(), tt, ll, st, now); err != nil {
		t.Fatal(err)
	}
	cursor := st.Cursor
	// An older completion that TickTick shows late does not move the cursor back.
	tick.add("t2", "p-home", "B")
	tick.finish("t2", now.Add(-20*time.Hour))
	if _, err := Sync(t.Context(), tt, ll, st, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !st.Cursor.Equal(cursor) {
		t.Errorf("cursor = %v, want %v", st.Cursor, cursor)
	}
	// A completion before the cursor is not written again or at all: the cursor is the filter.
	tick.add("t3", "p-home", "C")
	tick.finish("t3", cursor.Add(-time.Hour))
	n, err := Sync(t.Context(), tt, ll, st, now.Add(2*time.Minute))
	if err != nil || n != 0 {
		t.Errorf("a completion before the cursor: Sync = %d, %v, want 0, nil", n, err)
	}
}

func TestSyncSwitchesToVanishAndFetchForTheRestOfTheRun(t *testing.T) {
	tick, tt := newFakeTick(t)
	life, ll := newFakeLife(t)
	tick.noCompleted = true
	tick.add("t1", "p-home", "Pay rent")
	tick.add("t2", "p-work", "Will be deleted")
	tick.add("t3", "p-work", "Stays open")
	st := &State{Seen: map[string]string{}}

	if n, err := Sync(t.Context(), tt, ll, st, now); err != nil || n != 0 {
		t.Fatalf("first Sync = %d, %v, want 0, nil", n, err)
	}
	if !st.fetchOnly {
		t.Fatal("a 404 of /task/completed must switch the run to vanish-and-fetch")
	}
	if len(st.Seen) != 3 || st.Seen["t1"] != "p-home" {
		t.Errorf("Seen = %v, want the three open tasks with their lists", st.Seen)
	}

	done := now.Add(-time.Minute)
	tick.finish("t1", done)
	delete(tick.tasks, "t2")
	n, err := Sync(t.Context(), tt, ll, st, now)
	if err != nil || n != 1 {
		t.Fatalf("second Sync = %d, %v, want 1, nil", n, err)
	}
	want := capture{Day: "2030-03-05", Text: "Done: Pay rent · Home", Key: "t1:" + done.Format(ticktick.TimeLayout)}
	if got := life.got(); len(got) != 1 || got[0] != want {
		t.Errorf("captures = %+v, want [%+v]", got, want)
	}
	if len(st.Seen) != 1 || st.Seen["t3"] != "p-work" {
		t.Errorf("Seen = %v, want t3 only", st.Seen)
	}
	if n := tick.callsTo("POST /task/completed"); n != 1 {
		t.Errorf("POST /task/completed was called %d times, want 1: the switch lasts for the run", n)
	}
	if n, err := Sync(t.Context(), tt, ll, st, now); err != nil || n != 0 {
		t.Errorf("third Sync = %d, %v, want 0, nil", n, err)
	}
}

func TestAFailedWriteIsAnErrorWithTheIdAndLeavesTheStateAlone(t *testing.T) {
	tick, tt := newFakeTick(t)
	life, ll := newFakeLife(t)
	tick.add("t1", "p-home", "Private words of the owner")
	tick.finish("t1", now.Add(-time.Hour))
	life.refuse = 422
	st := &State{Seen: map[string]string{}}
	n, err := Sync(t.Context(), tt, ll, st, now)
	if err == nil || n != 0 {
		t.Fatalf("Sync = %d, %v, want an error", n, err)
	}
	if !strings.Contains(err.Error(), "t1") || strings.Contains(err.Error(), "words") {
		t.Errorf("err = %q, want the task id and no words", err)
	}
	if !st.Cursor.IsZero() {
		t.Errorf("cursor = %v, want it unmoved", st.Cursor)
	}
	life.refuse = 0
	if n, err := Sync(t.Context(), tt, ll, st, now); err != nil || n != 1 {
		t.Errorf("Sync after the fault = %d, %v, want 1, nil", n, err)
	}
}

func TestAPollSavesTheStateAndTheNextPollerReadsIt(t *testing.T) {
	tick, tt := newFakeTick(t)
	_, ll := newFakeLife(t)
	dir := t.TempDir()
	tick.add("t1", "p-home", "A")
	tick.finish("t1", now.Add(-time.Hour))
	p, err := NewPoller(tt, ll, dir)
	if err != nil {
		t.Fatal(err)
	}
	p.Now = func() time.Time { return now }
	if n, err := p.Poll(t.Context()); err != nil || n != 1 {
		t.Fatalf("Poll = %d, %v, want 1, nil", n, err)
	}
	again, err := NewPoller(tt, ll, dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := now.Add(-time.Hour).Add(-24 * time.Hour); !again.st.Cursor.Equal(want) {
		t.Errorf("cursor after a restart = %v, want %v", again.st.Cursor, want)
	}
}

func TestA401EndsThePollAndTellsServe(t *testing.T) {
	tick, tt := newFakeTick(t)
	_, ll := newFakeLife(t)
	tick.unauthorized = true
	p, err := NewPoller(tt, ll, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	called := false
	p.onLogin = func() { called = true }
	if _, err := p.Poll(t.Context()); !errors.Is(err, ErrLoginAgain) {
		t.Errorf("err = %v, want ErrLoginAgain", err)
	}
	if !called {
		t.Error("onLogin was not called")
	}
	if got := ErrLoginAgain.Error(); got != "login again: aminte ticktick login" {
		t.Errorf("the line = %q", got)
	}
}

func TestTwoPollsNeverRunAtOnce(t *testing.T) {
	tick, tt := newFakeTick(t)
	_, ll := newFakeLife(t)
	tick.slow, tick.entered = make(chan struct{}), make(chan struct{}, 2)
	p, err := NewPoller(tt, ll, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	go func() { _, err := p.Poll(t.Context()); results <- err }()
	<-tick.entered // the first poll is inside TickTick and holds the lock

	started := make(chan struct{})
	go func() { close(started); _, err := p.Poll(t.Context()); results <- err }()
	<-started
	for range 100 { // let the second poll run as far as it can: it must stop at the lock
		runtime.Gosched()
	}
	close(tick.slow)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if tick.maxInFlight != 1 {
		t.Errorf("%d requests to TickTick were in flight at once, want 1", tick.maxInFlight)
	}
	if n := tick.callsTo("GET /project"); n != 2 {
		t.Errorf("GET /project was called %d times, want 2 (one for each poll)", n)
	}
}

package view

import (
	"testing"
	"time"
)

// Tasks is in the rail only while a task service runs, right after Browse, and it is current on /tasks and below.
func TestTheRailHasTasksOnlyWhileATaskServiceRuns(t *testing.T) {
	hrefs := func(v *view) (out []string) {
		for _, it := range v.Nav() {
			if it.Group == "day" {
				out = append(out, it.Href)
			}
		}
		return out
	}
	without := hrefs(&view{Path: "/browse", Shell: &Shell{Today: "2031-02-03", Zone: time.UTC}})
	for _, h := range without {
		if h == "/tasks" {
			t.Errorf("the rail has Tasks with no task service: %v", without)
		}
	}
	with := hrefs(&view{Path: "/browse", Shell: &Shell{Today: "2031-02-03", Zone: time.UTC, Tasks: true}})
	at := -1
	for i, h := range with {
		if h == "/browse" {
			at = i
		}
	}
	if at < 0 || at+1 >= len(with) || with[at+1] != "/tasks" {
		t.Errorf("Tasks is not right after Browse: %v", with)
	}
	if len(with) != len(without)+1 {
		t.Errorf("the rail has %d day links with the service and %d without, want one more", len(with), len(without))
	}
	for path, want := range map[string]string{"/tasks": "/tasks", "/tasks/week": "/tasks", "/tasks/items/t1": "/tasks", "/browse": "/browse"} {
		var got string
		for _, it := range (&view{Path: path, Shell: &Shell{Tasks: true}}).Nav() {
			if it.Current {
				got = it.Href
			}
		}
		if got != want {
			t.Errorf("%s marks %q current, want %q", path, got, want)
		}
	}
}

// The pages of an app and of the task service are drawn by the shape of the entity, and no other page is.
func TestDrawnByShape(t *testing.T) {
	for path, want := range map[string]bool{
		"/with/app": true, "/with/app/x": true, "/tasks": true, "/tasks/week": true, "/tasks/items/1": true,
		"/": false, "/taskset": false, "/tasksx/1": false, "/days/today": false, "/withx": false,
	} {
		if got := drawnByShape(path); got != want {
			t.Errorf("drawnByShape(%q) = %v, want %v", path, got, want)
		}
	}
}

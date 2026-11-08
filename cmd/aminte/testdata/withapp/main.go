// Withapp is the connected app of the tests of cmd/aminte: a counter of items. It shows what a connector author
// writes: a home, some actions and a handler for each, served with aminte/app on the address that `aminte serve
// --with` gives in AMINTE_LISTEN. It names no third party.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"time"

	"aminte/app"
	"aminte/siren"
)

var (
	mu    sync.Mutex
	items []string // the text of item n is items[n-1]
	done  int      // how many times a done-item ran
)

var actions = []siren.Action{
	{Name: "add-item", Class: []string{"create"}, Method: "POST", Href: "/items", Title: "Add an item", Description: "Add an item with a text.",
		Fields: []siren.Field{{Name: "text", Type: "text", Title: "Text", Required: true}}},
	{Name: "look", Method: "GET", Href: "/items/{id}", Title: "Look at an item", Description: "Read one item.",
		Fields: []siren.Field{{Name: "id", Type: "number", Title: "Item", Required: true, In: "path"}}},
	{Name: "done-item", Class: []string{"toggle"}, Method: "POST", Href: "/items/{id}/done", Title: "Done", Description: "Mark an item done.",
		Fields: []siren.Field{{Name: "id", Type: "number", Title: "Item", Required: true, In: "path"}}},
	{Name: "reset", Method: "POST", Href: "/reset", Title: "Reset", Description: "Remove all items.", Owner: true},
	{Name: "stop", Method: "POST", Href: "/stop", Title: "Stop", Description: "End the app with status 0."},
	{Name: "crash", Method: "POST", Href: "/crash", Title: "Crash", Description: "End the app with status 1."},
}

func home(*http.Request) (*siren.Entity, error) {
	mu.Lock()
	defer mu.Unlock()
	e := &siren.Entity{Class: []string{"testapp"}, Title: "Test app", Properties: map[string]any{"count": len(items), "done": done}, Actions: actions}
	e.Links = []siren.Link{{Rel: []string{"collection"}, Href: "/", Title: "Items"}}
	if n := len(items); n > 0 { // the last item is a row: it has a chip and a round button
		id := strconv.Itoa(n)
		e.Entities = []siren.Link{{Rel: []string{"item"}, Href: "/items/" + id, Title: items[n-1], Properties: map[string]any{"n": n},
			Actions: []siren.Action{{Name: "done-item", Class: []string{"toggle"}, Method: "POST", Href: "/items/" + id + "/done", Title: "Done"}}}}
	}
	return e, nil
}

func addItem(r *http.Request) (*siren.Entity, error) {
	text := r.FormValue("text")
	if text == "" {
		return nil, &app.Error{Status: http.StatusUnprocessableEntity, Code: "invalid", Msg: "text is required"}
	}
	mu.Lock()
	items = append(items, text)
	n := len(items)
	mu.Unlock()
	return item(n, text), nil
}

func look(r *http.Request) (*siren.Entity, error) {
	n, err := strconv.Atoi(r.PathValue("id"))
	mu.Lock()
	defer mu.Unlock()
	if err != nil || n < 1 || n > len(items) {
		return nil, &app.Error{Status: http.StatusNotFound, Code: "not_found", Msg: "no such item"}
	}
	return item(n, items[n-1]), nil
}

func item(n int, text string) *siren.Entity {
	return &siren.Entity{Class: []string{"item"}, Title: text, Properties: map[string]any{"id": n, "text": text},
		Links: []siren.Link{{Rel: []string{"self"}, Href: "/items/" + strconv.Itoa(n), Title: text}}}
}

func doneItem(r *http.Request) (*siren.Entity, error) {
	mu.Lock()
	n, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || n < 1 || n > len(items) {
		mu.Unlock()
		return nil, &app.Error{Status: http.StatusNotFound, Code: "not_found", Msg: "no such item"}
	}
	done++
	mu.Unlock()
	return home(r)
}

func reset(r *http.Request) (*siren.Entity, error) {
	mu.Lock()
	items = nil
	mu.Unlock()
	return home(r)
}

// endAfterAnswer ends the process a moment after the handler returned, so that the answer is written first.
func endAfterAnswer(status int) func(*http.Request) (*siren.Entity, error) {
	return func(r *http.Request) (*siren.Entity, error) {
		go func() {
			time.Sleep(100 * time.Millisecond)
			os.Exit(status)
		}()
		return home(r)
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	h := app.Catalog("Test app", home, actions, map[string]func(*http.Request) (*siren.Entity, error){
		"add-item": addItem, "look": look, "done-item": doneItem, "reset": reset, "stop": endAfterAnswer(0), "crash": endAfterAnswer(1),
	})
	if err := app.Listen(ctx, h); err != nil {
		fmt.Fprintln(os.Stderr, "withapp:", err)
		os.Exit(1)
	}
}

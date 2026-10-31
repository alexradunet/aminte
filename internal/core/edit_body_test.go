package core

import (
	"errors"
	"testing"
)

// TestEditBodyEditsTheBodyThePageHasNow: edit gets the body as the page has it and its result is the new body, in one
// write that syncs the wikilinks. The error of edit ends the write and reaches the caller as it is. A page that is
// missing is 404, and a deleted page or a page of another type is 409; none is written.
func TestEditBodyEditsTheBodyThePageHasNow(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Notes", "one", "")
	if err != nil {
		t.Fatal(err)
	}
	got, sync, err := s.EditBody(ctx, "agent:x", "Notes", func(body string) (string, error) { return body + " two [[Sam]]", nil })
	if err != nil || got != id {
		t.Fatalf("EditBody: %d %v, want the id %d", got, err, id)
	}
	if p, _ := s.PageByID(ctx, id); p.Body != "one two [[Sam]]" || len(sync.Created) != 1 {
		t.Errorf("the page after the edit: %q, sync %+v, want the new body and the page Sam made", p.Body, sync)
	}
	refusal := errors.New("not this body")
	if _, _, err := s.EditBody(ctx, "cli", "Notes", func(string) (string, error) { return "lost", refusal }); !errors.Is(err, refusal) {
		t.Errorf("the error of edit: %v, want it as it is", err)
	}
	if p, _ := s.PageByID(ctx, id); p.Body != "one two [[Sam]]" {
		t.Errorf("a refused edit wrote: %q", p.Body)
	}
	if _, _, err := s.EditBody(ctx, "cli", "No such page", func(b string) (string, error) { return b, nil }); status(err) != 404 {
		t.Errorf("a missing page: %v, want 404", err)
	}
	sam, _ := s.PageID(ctx, "Sam")
	if err := s.Promote(ctx, "cli", sam, "Sam Example", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.EditBody(ctx, "cli", "Sam", func(b string) (string, error) { return b + "x", nil }); status(err) != 409 {
		t.Errorf("a person: %v, want 409", err)
	}
	if err := s.Tombstone(ctx, "cli", id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.EditBody(ctx, "cli", "Notes", func(b string) (string, error) { return b + "x", nil }); status(err) != 409 {
		t.Errorf("a deleted page: %v, want 409", err)
	}
	if p, _ := s.PageByID(ctx, id); p.DeletedAt == "" || p.Body != "one two [[Sam]]" {
		t.Errorf("the deleted page after the refused edit: deleted %q, body %q", p.DeletedAt, p.Body)
	}
}

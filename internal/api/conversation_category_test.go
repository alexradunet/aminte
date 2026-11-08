package api_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"aminte/client"
	"aminte/internal/api"
	"aminte/internal/api/apitest"
	"aminte/internal/core"
	"aminte/internal/db/dbtest"
	"aminte/siren"
)

// The category of the conversation pages is the plain page titled Conversations. Every first say of a day files the
// page of the day in it, and the schema refuses a part-of link to a page that is not a plain page. So a page of
// another type under that title would make every first say of a day fail with 422. These tests hold the rule from the
// outside: no write, of any writer, makes the page of that title a page of another type. A plain page stays allowed.

const (
	category = "Conversations"
	nextDay  = "2031-05-15"
)

// categoryStore is a store whose clock the test moves: it stands at 09:07 UTC on convDay, and the zone is UTC.
func categoryStore(t *testing.T) (http.Handler, *core.Store, *time.Time) {
	t.Helper()
	at := time.Date(2031, 5, 14, 9, 7, 0, 0, time.UTC)
	s := &core.Store{DB: dbtest.New(t), Clock: func() time.Time { return at }, Zone: time.UTC}
	return api.NewWith(s, nil), s, &at
}

// categoryWrites are the writes that give a page the type of a person, a metric or a file, by the title Conversations.
// A write that has no page to work on (promote) is nil when id is 0.
func categoryWrites(id int64) []struct {
	name, action string
	values       map[string]string
} {
	writes := []struct {
		name, action string
		values       map[string]string
	}{
		{"create-person", "create-person", map[string]string{"title": category}},
		{"create-person in another case", "create-person", map[string]string{"title": strings.ToUpper(category)}},
		{"register-metric", "register-metric", map[string]string{"name": category, "unit": "kg"}},
		{"add-file", "add-file", map[string]string{"title": category, "sha256": strings.Repeat("ab", 32), "mime": "text/plain"}},
	}
	if id != 0 {
		writes = append(writes, struct {
			name, action string
			values       map[string]string
		}{"promote", "promote", map[string]string{"id": fmt.Sprint(id)}})
	}
	return writes
}

// filedIn is the title of the category that the page titled title is part of; "" when it is part of none.
func filedIn(t *testing.T, s *core.Store, title string) string {
	t.Helper()
	id, err := s.PageID(t.Context(), title)
	if err != nil || id == 0 {
		t.Fatalf("the page %q: %d %v", title, id, err)
	}
	p, err := s.PageByID(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range p.Out {
		if l.Kind == "part-of" {
			return l.Title
		}
	}
	return ""
}

// TestNoWriteTurnsTheCategoryOfConversationsIntoAnotherType: create-person, register-metric, add-file and promote with
// the title Conversations (in any case) are 409 for every writer, whether the page does not exist yet, exists as a plain
// page, or already holds the conversation pages. The refusal writes nothing, and the first say of the next day still
// files its page in a plain page titled Conversations.
func TestNoWriteTurnsTheCategoryOfConversationsIntoAnotherType(t *testing.T) {
	for _, scenario := range []string{"no page", "plain page", "plain page with a conversation filed in it"} {
		for _, source := range writers {
			t.Run(scenario+"/"+source, func(t *testing.T) {
				h, s, at := categoryStore(t)
				owner := client.InProcess(h, "cli")
				var id int64
				switch scenario {
				case "plain page":
					if _, err := run(t, h, "cli", "create-page", map[string]string{"title": category, "body": "talks"}); err != nil {
						t.Fatal(err)
					}
				case "plain page with a conversation filed in it":
					if _, err := say(t, owner, convDay, "Hello"); err != nil {
						t.Fatal(err)
					}
				}
				id, _ = s.PageID(t.Context(), category)
				for _, w := range categoryWrites(id) {
					_, err := run(t, h, source, w.action, w.values)
					want := http.StatusConflict
					if source != "cli" && w.action == "register-metric" { // an owner action: an agent and an import are refused first
						want = http.StatusForbidden
					}
					if got := apitest.Status(err); got != want {
						t.Errorf("%s as %s: %v, want %d", w.name, source, err, want)
					}
				}
				if got, _ := s.PageID(t.Context(), category); got != id {
					t.Errorf("the page titled %s after the refusals: %d, want %d (0 is none)", category, got, id)
				}
				if id != 0 {
					p, err := s.PageByID(t.Context(), id)
					if err != nil || p.Type != "page" || p.Title != category {
						t.Errorf("the category after the refusals: %+v %v, want a plain page titled %s", p, err, category)
					}
				}
				*at = at.Add(24 * time.Hour)
				if _, err := say(t, owner, nextDay, "Next day"); err != nil {
					t.Fatalf("the first say of the next day after the refusals: %v", err)
				}
				if got := filedIn(t, s, "Conversation "+nextDay); got != category {
					t.Errorf("the conversation of the next day is filed in %q, want %s", got, category)
				}
				if got, _ := s.PageID(t.Context(), category); got == 0 {
					t.Fatalf("no page titled %s after the say", category)
				} else if p, err := s.PageByID(t.Context(), got); err != nil || p.Type != "page" {
					t.Errorf("the category after the say: %+v %v, want a plain page", p, err)
				}
			})
		}
	}
}

// TestAPlainPageTitledConversationsIsAllowed: the title is for a plain page. create-page (also as the category of
// another create-page), a wikilink that names the title and rename of a plain page to the title make it, for every
// writer, and say files the conversation page in it.
func TestAPlainPageTitledConversationsIsAllowed(t *testing.T) {
	for _, source := range writers {
		for _, how := range []string{"create-page", "the category of create-page", "a wikilink", "rename"} {
			t.Run(source+"/"+how, func(t *testing.T) {
				h, s, _ := categoryStore(t)
				var err error
				switch how {
				case "create-page":
					_, err = run(t, h, source, "create-page", map[string]string{"title": category, "body": "talks"})
				case "the category of create-page":
					_, err = run(t, h, source, "create-page", map[string]string{"title": "Sleep notes", "body": "x", "category": category})
				case "a wikilink":
					_, err = run(t, h, source, "create-page", map[string]string{"title": "Sleep notes", "body": "see [[" + category + "]]"})
				case "rename":
					var e *siren.Entity
					if e, err = run(t, h, source, "create-page", map[string]string{"title": "Talks", "body": "x"}); err == nil {
						_, err = run(t, h, source, "rename", map[string]string{"id": fmtID(e), "title": category})
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				id, _ := s.PageID(t.Context(), category)
				if p, err := s.PageByID(t.Context(), id); err != nil || p.Type != "page" {
					t.Fatalf("the page titled %s: %+v %v, want a plain page", category, p, err)
				}
				if _, err := say(t, client.InProcess(h, "cli"), convDay, "Hello"); err != nil {
					t.Fatal(err)
				}
				if got := filedIn(t, s, "Conversation "+convDay); got != category {
					t.Errorf("the conversation is filed in %q, want %s", got, category)
				}
			})
		}
	}
}

// TestRenameDoesNotGiveTheTitleOfTheCategoryToAPageThatIsNotPlain: a person is not renamed to Conversations (409, for
// every writer). The page keeps its title, nothing holds the title, and the first say makes the category.
func TestRenameDoesNotGiveTheTitleOfTheCategoryToAPageThatIsNotPlain(t *testing.T) {
	for _, source := range writers {
		t.Run(source, func(t *testing.T) {
			h, s, _ := categoryStore(t)
			ana, err := run(t, h, "cli", "create-person", map[string]string{"title": "Ana"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := run(t, h, source, "rename", map[string]string{"id": fmtID(ana), "title": category}); apitest.Status(err) != http.StatusConflict {
				t.Errorf("rename of a person to %s as %s: %v, want 409", category, source, err)
			}
			if id, _ := s.PageID(t.Context(), category); id != 0 {
				t.Errorf("a refused rename gave the title to the page %d", id)
			}
			if _, err := say(t, client.InProcess(h, "cli"), convDay, "Hello"); err != nil {
				t.Fatalf("say after the refused rename: %v", err)
			}
			if got := filedIn(t, s, "Conversation "+convDay); got != category {
				t.Errorf("the conversation is filed in %q, want %s", got, category)
			}
		})
	}
}

// TestTheCategoryOfConversationsRenamedAwayStaysPlain: the plain page that holds the title may be renamed. It keeps
// the title as a name it had, so say still files in it, and the page stays a plain page: promote, create-person with
// its new title and a rename of a person to the old title are 409, and the 409 offers no promotion.
func TestTheCategoryOfConversationsRenamedAwayStaysPlain(t *testing.T) {
	for _, source := range writers {
		t.Run(source, func(t *testing.T) {
			h, s, _ := categoryStore(t)
			cat, err := run(t, h, "cli", "create-page", map[string]string{"title": category, "body": "talks"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := run(t, h, source, "rename", map[string]string{"id": fmtID(cat), "title": "Talks"}); err != nil {
				t.Fatalf("rename of the plain category as %s: %v", source, err)
			}
			ana, err := run(t, h, "cli", "create-person", map[string]string{"title": "Ana"})
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range []struct {
				name, action string
				values       map[string]string
			}{
				{"promote", "promote", map[string]string{"id": fmtID(cat)}},
				{"create-person with the new title", "create-person", map[string]string{"title": "Talks"}},
				{"create-person with the old title", "create-person", map[string]string{"title": category}},
				{"rename of a person to the old title", "rename", map[string]string{"id": fmtID(ana), "title": category}},
			} {
				_, err := run(t, h, source, w.action, w.values)
				if got := apitest.Status(err); got != http.StatusConflict {
					t.Errorf("%s as %s: %v, want 409", w.name, source, err)
				}
				var ce *client.Error
				if errors.As(err, &ce) && ce.Entity != nil && ce.Entity.LinkByRel("promote") != nil {
					t.Errorf("%s as %s: the 409 offers a promotion that is always refused", w.name, source)
				}
			}
			if p, err := s.PageByID(t.Context(), mustID(t, cat)); err != nil || p.Type != "page" || p.Title != "Talks" {
				t.Errorf("the renamed category after the refusals: %+v %v, want a plain page titled Talks", p, err)
			}
			if _, err := say(t, client.InProcess(h, "cli"), convDay, "Hello"); err != nil {
				t.Fatalf("say after the rename of the category: %v", err)
			}
			if got := filedIn(t, s, "Conversation "+convDay); got != "Talks" {
				t.Errorf("the conversation is filed in %q, want Talks, the page that held the title", got)
			}
		})
	}
}

// TestTheCategoryOfConversationsOffersNoPromotion: create-person on the plain page does not offer to make it a person,
// and the page offers neither promote nor the link that makes it a metric. rename stays. A plain page of another
// title keeps its offers.
func TestTheCategoryOfConversationsOffersNoPromotion(t *testing.T) {
	h, s, _ := categoryStore(t)
	cat, err := run(t, h, "cli", "create-page", map[string]string{"title": category, "body": "talks"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = run(t, h, "cli", "create-person", map[string]string{"title": category})
	var ce *client.Error
	if !errors.As(err, &ce) || ce.Status != http.StatusConflict || ce.Code() != siren.CodeExists {
		t.Fatalf("create-person with the title of the category: %v, want 409 %s", err, siren.CodeExists)
	}
	if l := ce.Entity.LinkByRel("existing"); l == nil || l.Href != apitest.Href(cat, "self") {
		t.Errorf("the link existing is %+v, want the category", l)
	}
	if l := ce.Entity.LinkByRel("promote"); l != nil {
		t.Errorf("the 409 offers a promotion that is always refused: %+v", l)
	}
	c := client.InProcess(h, "cli")
	page := apitest.Must(t)(c.Get(t.Context(), siren.PageHref(mustID(t, cat))))
	for _, a := range page.Actions {
		if a.Name == "promote" {
			t.Errorf("the category offers promote, which is always refused")
		}
	}
	if got := apitest.Href(page, "make-metric"); got != "" {
		t.Errorf("the category offers to make a metric of it: %q", got)
	}
	apitest.Action(t, page, "rename")

	other := apitest.Must(t)(run(t, h, "cli", "create-page", map[string]string{"title": "Sleep notes", "body": "x"}))
	otherPage := apitest.Must(t)(c.Get(t.Context(), siren.PageHref(mustID(t, other))))
	apitest.Action(t, otherPage, "promote")
	if apitest.Href(otherPage, "make-metric") == "" {
		t.Errorf("a plain page of another title: %v, want the link to make a metric", otherPage.Links)
	}
	if id, _ := s.PageID(t.Context(), category); id != mustID(t, cat) {
		t.Errorf("the page titled %s is %d, want %d", category, id, mustID(t, cat))
	}
}

// mustID is the id of the page that an answer carries.
func mustID(t *testing.T, e *siren.Entity) int64 {
	t.Helper()
	var id int64
	if _, err := fmt.Sscan(fmtID(e), &id); err != nil {
		t.Fatal(err)
	}
	return id
}

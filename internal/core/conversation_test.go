package core

import (
	"strings"
	"testing"
)

// TestConversationDayIsReadFromTheKeyOfTheTitle: the vectors are written by hand. A title names the conversation page
// of a day by its key, which ignores case, so one predicate serves the write that refuses the title, the page view and
// the link to the day's conversation.
func TestConversationDayIsReadFromTheKeyOfTheTitle(t *testing.T) {
	for title, want := range map[string]string{
		"Conversation 2031-05-14": "2031-05-14", "conversation 2031-05-14": "2031-05-14", "CONVERSATION 2031-05-14": "2031-05-14",
		"Conversation 2031-05-14 09.07.00": "", "Conversation 2031-13-14": "", "Conversation 2031-02-30": "",
		"Conversation": "", "Conversation notes": "", "Notes 2031-05-14": "", "2031-05-14": "", "": "",
	} {
		if got := ConversationDay(title); got != want {
			t.Errorf("ConversationDay(%q) = %q, want %q", title, got, want)
		}
	}
	if got, want := ConversationTitle("2031-05-14"), "Conversation 2031-05-14"; got != want {
		t.Errorf("ConversationTitle = %q, want %q", got, want)
	}
}

// TestAConversationTitleIsKeptForSay: no write makes a page under the title of a conversation, gives a page that
// title, takes the title from a page, or changes the type of the page that holds it. Each refusal is a 409 that names
// the title, writes nothing, and holds in every spelling of the key. Say (AppendParagraph) makes the page, and a
// wikilink makes an empty page: they are the two exceptions.
func TestAConversationTitleIsKeptForSay(t *testing.T) {
	s := fresh(t)
	notes, _, err := s.CreatePage(ctx, "cli", "Notes", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Conversation 2031-05-15", "conversation 2031-05-16"} {
		for name, write := range map[string]func() error{
			"CreatePage":         func() error { _, _, err := s.CreatePage(ctx, "cli", title, "x", ""); return err },
			"the category of it": func() error { _, _, err := s.CreatePage(ctx, "cli", "Notes 2", "x", title); return err },
			"FilePages":          func() error { _, err := s.FilePages(ctx, "cli", []int64{notes}, title, "", false); return err },
			"CreatePerson":       func() error { _, err := s.CreatePerson(ctx, "cli", title, "", "", ""); return err },
			"CreateMetric":       func() error { _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: title, Unit: "kg"}); return err },
			"AddFile": func() error {
				_, err := s.AddFile(ctx, "cli", FileIn{Title: title, SHA256: strings.Repeat("ab", 32), MIME: "text/plain"})
				return err
			},
			"Rename to the title":  func() error { _, err := s.Rename(ctx, "cli", notes, title); return err },
			"Rename, spelled less": func() error { _, err := s.Rename(ctx, "cli", notes, strings.ToUpper(title)); return err },
		} {
			err := write()
			if status(err) != 409 || !strings.Contains(err.Error(), "conversation title") {
				t.Errorf("%s with %q: %v, want 409 that names the conversation title", name, title, err)
			}
			if id, err := s.PageID(ctx, title); err != nil || id != 0 {
				t.Errorf("%s with %q made the page %d %v", name, title, id, err)
			}
		}
	}
	if p, err := s.PageByID(ctx, notes); err != nil || p.Title != "Notes" || p.Type != "page" {
		t.Errorf("Notes after the refusals: %+v %v, want it as it was", p, err)
	}

	// The page that say made keeps its type and its title.
	const title = "Conversation 2031-05-14"
	id, _, err := s.AppendParagraph(ctx, "cli", title, "2031-05-14", "Conversations", "[[2031-05-14]]", func(b string) string { return b + "\n\n**Owner** (09:00): Hello" })
	if err != nil {
		t.Fatal(err)
	}
	for name, write := range map[string]func() error{
		"Promote":         func() error { return s.Promote(ctx, "cli", id, "", "", "") },
		"Rename it":       func() error { _, err := s.Rename(ctx, "cli", id, "Talk"); return err },
		"Rename its case": func() error { _, err := s.Rename(ctx, "cli", id, "conversation 2031-05-14"); return err },
		"CreateMetric":    func() error { _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: title, Unit: "kg"}); return err },
		"AddFile": func() error {
			_, err := s.AddFile(ctx, "cli", FileIn{Title: title, SHA256: strings.Repeat("cd", 32), MIME: "text/plain"})
			return err
		},
	} {
		if err := write(); status(err) != 409 {
			t.Errorf("%s on the conversation page: %v, want 409", name, err)
		}
	}
	p, err := s.PageByID(ctx, id)
	if err != nil || p.Type != "page" || p.Title != title || p.Person != nil || p.Metric != nil || p.File != nil {
		t.Errorf("the conversation page after the refusals: %+v %v, want a plain page with its title", p, err)
	}
	// A create-person that meets the page offers no promote: promote would be refused.
	if _, err := s.CreatePerson(ctx, "cli", title, "", "", ""); err == nil {
		t.Error("create-person with the title of a conversation page that exists: no error")
	} else if ex, ok := err.(*ExistsError); ok && ex.Plain {
		t.Errorf("the title is taken by a conversation page, and the error offers promote: %+v", ex)
	}
}

// TestAWikilinkMakesAnEmptyConversationPageAndSayAdoptsIt: the save contract of wikilinks makes the target, also when
// its title is a conversation title. The page has no words, so the first AppendParagraph gives it the first line, its
// day and its category, as it does for a page that it makes itself. A page that has words keeps them: the paragraph is
// added after them, and the first line, the day and the category are not touched.
func TestAWikilinkMakesAnEmptyConversationPageAndSayAdoptsIt(t *testing.T) {
	s := fresh(t)
	if _, _, err := s.CreatePage(ctx, "agent:x", "Notes", "see [[Conversation 2031-05-14]] and [[conversation 2031-05-15]]", ""); err != nil {
		t.Fatal(err)
	}
	id, _ := s.PageID(ctx, "Conversation 2031-05-14")
	if p, err := s.PageByID(ctx, id); err != nil || p.Type != "page" || p.Body != "" || p.Day != "" {
		t.Fatalf("the stub: %+v %v, want an empty plain page with no day", p, err)
	}
	add := func(b string) string { return b + "\n\n**Owner** (09:00): Hello" }
	got, _, err := s.AppendParagraph(ctx, "cli", "Conversation 2031-05-14", "2031-05-14", "Conversations", "[[2031-05-14]]", add)
	if err != nil || got != id {
		t.Fatalf("AppendParagraph on the stub: %d %v, want the stub %d", got, err, id)
	}
	p, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if want := "[[2031-05-14]]\n\n**Owner** (09:00): Hello"; p.Body != want || p.Day != "2031-05-14" || titles(p.Out, "part-of") != "Conversations" {
		t.Errorf("the adopted page: body %q, day %q, filed in %q; want %q, 2031-05-14, Conversations", p.Body, p.Day, titles(p.Out, "part-of"), want)
	}
	// A page with words keeps its first line, its day and its category.
	if _, _, err := s.AppendParagraph(ctx, "cli", "Conversation 2031-05-14", "2031-05-14", "Conversations", "[[2031-05-14]]", add); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.PageByID(ctx, id); p.Body != "[[2031-05-14]]\n\n**Owner** (09:00): Hello\n\n**Owner** (09:00): Hello" {
		t.Errorf("the second paragraph: %q", p.Body)
	}
}

// TestOnlyTheOwnerWritesTheBodyOfAConversationPage: a writer that is not the owner's (an agent, an import, a name that
// is no writer name) is refused (403) in every write of a body of a conversation page that is not a turn: the save of a
// body and the plain write of a body, which set-cover and every other caller uses. Nothing is written. The two writes
// of turns, AppendParagraph (say) and EditBody (reply), pass for any writer. The owner's save passes, and so does the
// save of any writer on a page that is no conversation.
func TestOnlyTheOwnerWritesTheBodyOfAConversationPage(t *testing.T) {
	s := fresh(t)
	const title = "Conversation 2031-05-14"
	turn := func(b string) string { return b + "\n\n**Owner** (09:00): Hello" }
	id, _, err := s.AppendParagraph(ctx, "cli", title, "2031-05-14", "Conversations", "[[2031-05-14]]", turn)
	if err != nil {
		t.Fatal(err)
	}
	notes, _, err := s.CreatePage(ctx, "cli", "Notes", "plain", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "Stub", "see [[Conversation 2031-05-15]]", ""); err != nil {
		t.Fatal(err)
	}
	stubID, _ := s.PageID(ctx, "Conversation 2031-05-15")
	if stubID == 0 {
		t.Fatal("the wikilink made no stub")
	}
	page := func(id int64) *Page {
		p, err := s.PageByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	for _, source := range []string{"agent:mcp", "import:x.y", "Agent:mcp", ""} {
		for _, target := range []struct {
			name string
			id   int64
		}{{"the page of a conversation", id}, {"the empty page that a wikilink made", stubID}} {
			before := page(target.id)
			if _, err := s.SaveBody(ctx, source, target.id, "forged", before.Version); status(err) != 403 || !strings.Contains(err.Error(), "reply") {
				t.Errorf("SaveBody of %s as %q: %v, want 403 that names reply", target.name, source, err)
			}
			err := s.Do(ctx, source, func(tx *Tx) error { _, err := tx.SetBody(target.id, "forged"); return err })
			if status(err) != 403 || !strings.Contains(err.Error(), "reply") {
				t.Errorf("SetBody of %s as %q: %v, want 403 that names reply", target.name, source, err)
			}
			if got := page(target.id); got.Body != before.Body {
				t.Errorf("a refusal wrote the body of %s as %q: %q, want %q", target.name, source, got.Body, before.Body)
			}
		}
		if _, err := s.SaveBody(ctx, source, notes, "plain "+source, page(notes).Version); err != nil {
			t.Errorf("SaveBody of a plain page as %q: %v, want it allowed", source, err)
		}
	}
	// The two writes of turns pass for an agent.
	if _, _, err := s.EditBody(ctx, "agent:aminte", title, func(b string) (string, error) { return b + "\n\n**aminte** (09:01): Hi", nil }); err != nil {
		t.Errorf("EditBody (reply) as agent:aminte: %v, want it allowed", err)
	}
	if _, _, err := s.AppendParagraph(ctx, "agent:mcp", title, "2031-05-14", "Conversations", "[[2031-05-14]]", turn); err != nil {
		t.Errorf("AppendParagraph (say) as agent:mcp: %v, want it allowed", err)
	}
	// The owner saves the body, and the first AppendParagraph still adopts the empty page that a wikilink made.
	p := page(id)
	if _, err := s.SaveBody(ctx, "cli", id, p.Body+"\n\nan edit", p.Version); err != nil {
		t.Errorf("SaveBody of the conversation as the owner: %v, want it allowed", err)
	}
	if _, _, err := s.AppendParagraph(ctx, "agent:mcp", "Conversation 2031-05-15", "2031-05-15", "Conversations", "[[2031-05-15]]", turn); err != nil {
		t.Fatal(err)
	}
	if p := page(stubID); p.Day != "2031-05-15" || !strings.HasPrefix(p.Body, "[[2031-05-15]]") {
		t.Errorf("the stub after the first turn: body %q, day %q, want it adopted", p.Body, p.Day)
	}
}

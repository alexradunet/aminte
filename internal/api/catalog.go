// Package api serves life.db as hypermedia: Siren-style JSON for every client, and bytes (a picture, a snapshot). It
// draws no page: a UI is a client, and the browser is the extension lifelog-web. A client needs no rules of its own: an
// action is offered only where it is legal, with its fields (and their current values) filled in. The Siren types and
// the addresses of the resources are in package siren, which the clients import too. This file holds the catalog of the
// actions.
package api

import (
	"net/url"
	"slices"
	"strings"

	"lifelog/internal/core"
	"lifelog/siren"
)

const formType = "application/x-www-form-urlencoded"

// spec is one action of the catalog: a templated href ("/pages/{id}/body") and its fields, path fields included.
// An Owner action is the owner's alone: refused to an agent:* writer and never an MCP tool. A Danger action hides or
// retracts what is already there: a UI draws its button as a danger and any other client may warn the same way;
// no surface decides this by an action's name.
type spec struct {
	Name, Title, Description, Hint, Method, Path string
	Fields                                       []siren.Field
	Owner, Danger                                bool
	Class                                        []string // at most one of siren.ActionClasses: what the action does
}

func path(name, typ, title string) siren.Field {
	return siren.Field{Name: name, Type: typ, Title: title, Required: true, In: "path"}
}

func req(name, typ, title string) siren.Field {
	return siren.Field{Name: name, Type: typ, Title: title, Required: true}
}

func opt(name, typ, title string) siren.Field {
	return siren.Field{Name: name, Type: typ, Title: title}
}

// pageField is a text field whose value is a page's title; required says whether the action needs one.
func pageField(name, title string, required bool) siren.Field {
	return siren.Field{Name: name, Type: "text", Title: title, Required: required, Pages: true}
}

// area is a multi-line field that a UI draws rows lines tall.
func area(name, title string, rows int) siren.Field {
	return siren.Field{Name: name, Type: "textarea", Title: title, Rows: rows}
}

// flagField is a 0 or 1 choice: a UI draws a checkbox, and a program sends 1 for yes. Any other value is refused
// (options).
func flagField(name, title string) siren.Field {
	return siren.Field{Name: name, Type: "number", Title: title, Options: []string{"0", "1"}}
}

// areaReq is a multi-line field the action cannot do without.
func areaReq(name, title string, rows int) siren.Field {
	f := area(name, title, rows)
	f.Required = true
	return f
}

// placeheld gives a field the words that a client may show while it is empty (siren.Field.Placeholder).
func placeheld(f siren.Field, example string) siren.Field {
	f.Placeholder = example
	return f
}

// catalog is every action the API has. mount gives each one its route, GET /actions serves it, and the CLI and the MCP
// server are built from it.
var catalog = []spec{
	{Name: "capture", Title: "Add to the day", Class: []string{"create"},
		Description: "Append an entry to the journal page of a local day (created on the first capture), with an optional mood 1-5. The entry begins with its time, (HH:MM) in the server's zone, and names the relay when an agent or an import wrote it: (HH:MM, via NAME). [[Title]] and #tag in the text link pages, creating them if new. import_key makes a re-send a no-op: make one key for one capture and send it again unchanged on a retry; the answer is then Already added, with the day.",
		Hint:        "Add a note to the journal of a day. A mood from 1 to 5 is optional. A name in [[double brackets]] or a #tag links a page, and makes it when it is new.",
		Method:      "POST", Path: "/days/{day}/capture",
		Fields: []siren.Field{path("day", "date", "Local day"), placeheld(area("text", "Entry (CommonMark)", 4), "What happened? Who was there?"),
			{Name: "mood", Type: "number", Title: "Mood", Options: []string{"1", "2", "3", "4", "5"}}, opt("import_key", "text", "Key")},
	},
	{Name: "say", Title: "Chat",
		Description: "Write to the agent: text is added to the conversation page of a local day (the page Conversation YYYY-MM-DD, made on the first turn of the day, filed in the category Conversations) as the paragraph **Owner** (HH:MM): text, with the time in the server's zone. say is the owner's action: an agent or an import is refused (403). via names the relay, the program that carries the owner's words; the paragraph is then **Owner** (HH:MM, via NAME): text. The core calls no model: an agent that watches the page (an extension) answers on the same page with reply. A line of the text that reads as a turn marker is escaped, and a code fence that the text does not close is closed, so the page stays readable as turns. The answer is the conversation of the day, with the turns, and the result of a save of a body.",
		Hint:        "Write to the agent. It answers here when it runs.",
		Method:      "POST", Path: "/days/{day}/say", Owner: true,
		Fields: []siren.Field{path("day", "date", "Local day"), areaReq("text", "To the agent", 3),
			opt("via", "hidden", "Relay: the name of the program that carries the owner's words")},
	},
	{Name: "reply", Title: "Reply",
		Description: "Write the agent's turn on the conversation page of a local day (the page Conversation YYYY-MM-DD): text is added as the paragraph **lifelog** (HH:MM): text, with the time in the server's zone, right after the owner's turn that it answers. turn is the key of that owner turn: the key of a turn of the conversation (turns[].key). A key names a turn by what it is and not by its place, so an edit that removes an earlier turn moves no reply. A turn that the owner said meanwhile stays after the reply and waits for its own. tools is the tool lines of the turn, one for each line, each starting with - ` (a dash, a space and a backtick); they are written right before the paragraph. The write is one transaction. A turn that has a reply already is 409, a key that no owner turn of the page has now is 409 (the owner edited the turn or removed it: read the conversation again), and a day with no conversation is 404. reply is not the owner's: an agent writes its turn with it, because an agent or an import is refused when it saves the body of a conversation page (403). A line of the text that reads as a turn marker is escaped, and a code fence that the text does not close is closed, so the page stays readable as turns. The answer is the conversation of the day.",
		Hint:        "Write the agent's answer to a turn of the owner on the conversation of a day.",
		Method:      "POST", Path: "/days/{day}/reply",
		Fields: []siren.Field{path("day", "date", "Local day"), req("turn", "text", "The key of the owner's turn that it answers (turns[].key of the conversation)"), areaReq("text", "The reply", 4),
			opt("tools", "text", "Tool lines, one for each line (new lines between them), each starting with - `")},
	},
	{Name: "create-page", Title: "New page", Class: []string{"create"},
		Description: "Create a page with a unique title (a valid file name) and a CommonMark body. category files it in that category page (D26) in the same write, making the category when it is new.",
		Hint:        "Make a page with a title that no other page has. A category is optional: the page is filed in it.",
		Method:      "POST", Path: "/pages",
		Fields: []siren.Field{req("title", "text", "Title"), area("body", "Body", 12), pageField("category", "Category (title)", false)},
	},
	{Name: "save-body", Title: "Save", Class: []string{"edit"},
		Description: "Replace a page's whole body. version must be the page's current version (from the last read); wikilinks are re-synced. An agent or an import may not save a conversation page (403): the agent writes its turn with reply.",
		Method:      "POST", Path: "/pages/{id}/body",
		Fields: []siren.Field{path("id", "number", "Page id"), areaReq("body", "Body", 18), req("version", "hidden", "Version")},
	},
	{Name: "create-person", Title: "New person", Class: []string{"create"},
		Description: "Create a person: title is the permanent handle that [[wikilinks]] use (e.g. 'Sam (barber)'), name the full name.",
		Hint:        "Make a person. The title is the short name that links use. The name is the full name.",
		Method:      "POST", Path: "/people",
		Fields: []siren.Field{placeheld(req("title", "text", "Name in [[links]]"), "Sam (barber)"), placeheld(opt("name", "text", "Full name"), "Samuel Ortiz"), opt("birth_day", "date", "Born"), opt("death_day", "date", "Died")},
	},
	{Name: "add-file", Title: "New file", Class: []string{"create"},
		Description: "Keep a file the owner picked (a recording, a PDF, a scan, a photo) as a page: title is its permanent handle (by default the original's file name; required with a sha256), and ![[title]] in a page shows it; body is its text (a transcript, the text of a PDF, a caption). Send the original, which is read and never stored, or its sha256 and mime. A picture is made from the original when it is a JPEG, PNG or GIF; for any other (HEIC, a video's frame) send preview, a JPEG, PNG or GIF. The file's own day is day, else the EXIF date of a JPEG, read from the original (the program reads none from a HEIC: send day). The page of its own day shows a file that has a picture. A photo with no day of its own gets no day and goes in no day page; any other file without one gets the day it is kept. The same original again finds its page and writes nothing but a missing picture and its embed in the page of its own day.",
		Hint:        "Keep a file as a page. The file is read, and its text and a small picture are kept. The original stays where it is.",
		Method:      "POST", Path: "/files",
		Fields: []siren.Field{opt("original", "file", "The file (its text and a small picture are kept; the original stays where it is)"), opt("title", "text", "Title (default: the original's file name)"), opt("sha256", "text", "SHA-256 of the original, when it is not sent"),
			opt("mime", "text", "Type, e.g. audio/mp4"), opt("preview", "file", "Picture, when the original is not a JPEG, PNG or GIF"), area("body", "Text: a transcript, a caption", 6), opt("day", "date", "Its day (default: a JPEG's EXIF date, else none; any other file: today)"),
			flagField("dry_run", "Dry run: write nothing, say what keeping it would do")},
	},
	{Name: "set-cover", Title: "Set cover photo", Class: []string{"edit"},
		Description: "Make a photo this page's cover: the first picture its body embeds, drawn beside the page, in the lists and in the calendar. Its embed goes first in the body, or trades places with the cover's when the body embeds it already. Send a new photo as original (kept as a file with no day, titled with the page's title and today, a day page's with its day alone, unless title names it), or name a kept picture by title (GET /pages/{id}/cover lists them). A file page takes none: its own picture is its cover.",
		Hint:        "Choose the photo that shows beside this page. Add a new photo, or pick one that is already kept.",
		Method:      "POST", Path: "/pages/{id}/cover",
		Fields: []siren.Field{path("id", "number", "Page id"), {Name: "original", Type: "file", Title: "A new photo (read, never stored)", Accept: "image/*"},
			opt("title", "text", "A kept picture's title; with a new photo, its title"), opt("preview", "file", "A JPEG, PNG or GIF of the photo, when it is not one")},
	},
	{Name: "promote", Title: "Make it a person", Class: []string{"edit"},
		Description: "Make a plain page (e.g. one a [[wikilink]] created) a person; its id and links stay. name, birth_day and death_day are optional: the name is the page's title when left out.",
		Hint:        "Make this page a person. Its text and links stay.",
		Method:      "POST", Path: "/pages/{id}/promote",
		Fields: []siren.Field{path("id", "number", "Page id"), opt("name", "text", "Full name (a person)"),
			opt("birth_day", "date", "Born (a person)"), opt("death_day", "date", "Died (a person)")},
	},
	{Name: "edit-person", Title: "Edit name and dates", Class: []string{"edit"},
		Description: "Change the full name and the dates of a person (the handle, the page's title, changes with rename). A field left out stays as it is, and an edit with no field is 422. An empty birth_day or death_day removes that date; an empty name is 422. A day is YYYY-MM-DD, or the word today. A bad day, or a death before the birth, is 422. A page that is not a person is 409.",
		Hint:        "Change the name and the dates of this person. Leave a date empty to remove it. Rename changes the title.",
		Method:      "POST", Path: "/pages/{id}/person",
		Fields: []siren.Field{path("id", "number", "Page id"), placeheld(opt("name", "text", "Full name"), "Samuel Ortiz"), opt("birth_day", "date", "Born"), opt("death_day", "date", "Died")},
	},
	{Name: "edit-metric", Title: "Ask for it every day", Class: []string{"edit"},
		Description: "Set daily of a metric: 1 asks for a reading of it on every day, 0 stops asking. Its readings stay. A page that is not a live metric is 409.",
		Hint:        "Tick it to see a box for this metric on every day page. Untick it to stop. Its readings stay.",
		Method:      "POST", Path: "/pages/{id}/metric",
		Fields: []siren.Field{path("id", "number", "Page id"), flagField("daily", "Ask for it every day")},
	},
	{Name: "revive", Title: "Restore", Class: []string{"toggle"},
		Description: "Undo a tombstone.",
		Method:      "POST", Path: "/pages/{id}/revive",
		Fields: []siren.Field{path("id", "number", "Page id")},
	},
	{Name: "tombstone-pages", Title: "Delete",
		Description: "Tombstone one page or several (files among them) in one write: ids are page ids, separated by spaces or commas. Nothing is ever removed. An id no page has refuses the whole action; revive undoes each.",
		Hint:        "Delete pages. Nothing is removed for good: Restore brings a page back.",
		Method:      "POST", Path: "/pages/tombstone",
		Fields: []siren.Field{req("ids", "text", "Page ids, separated by spaces or commas")},
		Danger: true,
	},
	{Name: "file-pages", Title: "Put in a category",
		Description: "File several pages (metrics among them) in the category page titled `category`, in one write (a part-of link from each), or with `from` take them out of the category page titled `from` (the link from each is removed; nothing else changes): ids are page ids, separated by spaces or commas. One of category or from is required, never both. A missing category is made as an empty page. With move 1, every other part-of link of those pages is removed first (with category only). An id no page has refuses the whole action.",
		Hint:        "Put the checked pages in a category, or take them out of one. A category that does not exist yet is made.",
		Method:      "POST", Path: "/pages/file",
		Fields: []siren.Field{req("ids", "text", "Page ids, separated by spaces or commas"), placeheld(pageField("category", "Category (title)", false), "Bloodwork, Sport, Financial"), pageField("from", "Out of the category (title)", false),
			flagField("move", "Move (remove the other categories)")},
	},
	{Name: "record", Title: "Add a reading", Class: []string{"create"},
		Description: "Record a reading of a registered metric for a local day. import_key makes a re-send a no-op. A view offers every live metric by name.",
		Hint:        "Add a reading of a metric for a day: a number in the unit of the metric. For a habit, 1 is done and 0 is not done.",
		Method:      "POST", Path: "/measurements",
		Fields: []siren.Field{placeheld(req("metric", "text", "Metric"), "Pick a metric"), req("day", "date", "Day"), req("value", "number", "Value"),
			opt("taken_at", "text", "Time taken, in UTC (2026-06-09T21:14:03.482Z)"), opt("tz", "text", "Time zone (Europe/Bucharest)"), opt("import_key", "text", "Key")},
	},
	{Name: "correct", Title: "Correct", Class: []string{"edit"},
		Description: "Correct a reading with a new value (a reading is corrected once; correct the correction after that). To remove a reading, retract it.",
		Hint:        "Give this reading a new value. A reading is corrected once. To remove a reading, retract it.",
		Method:      "POST", Path: "/measurements/{id}/correct",
		Fields: []siren.Field{path("id", "number", "Measurement id"), req("value", "number", "Value")},
	},
	{Name: "retract", Title: "Retract",
		Description: "Retract a reading that should never have existed.",
		Hint:        "Mark a reading that should never have existed.",
		Method:      "POST", Path: "/measurements/{id}/retract",
		Fields: []siren.Field{path("id", "number", "Measurement id")},
		Danger: true,
	},
	{Name: "rename", Title: "Rename", Class: []string{"edit"},
		Description: "Select a new preferred reference name without changing this identity, body or links. Previous names remain aliases. Another identity's name and journal dates are refused.",
		Hint:        "Choose a new title for this page. Its text and links stay, and the old title stays as another name for it.",
		Method:      "POST", Path: "/pages/{id}/rename",
		Fields: []siren.Field{path("id", "number", "Page id"), req("title", "text", "New title")},
	},
	{Name: "register-metric", Title: "New metric", Class: []string{"create"},
		Description: "Make a new metric: a thing you measure again and again, such as Weight, Sleep or Mood. Each reading is a number. The unit never changes: kg or h; a range such as 1-5 for a score; empty for a count, or for a habit that is done or not done. The note is the text of its page. A plain page with this name becomes the metric and keeps its text. A metric that exists is returned as it is when the unit is the same, or when it is a scale and the unit is empty; sent with a daily that differs from its mark, it is refused (409): edit-metric changes the mark. daily 1 asks for a reading of the metric on every day.",
		Hint:        "A thing you measure again and again, such as Weight or Mood. The unit never changes: kg, h, a range such as 1-5 for a score, or empty for a count or a habit. A plain page with this name becomes the metric and keeps its text. Tick Ask for it every day to get a box for it on every day page.",
		Method:      "POST", Path: "/metrics",
		Fields: []siren.Field{placeheld(req("name", "text", "Name"), "Weight, Mood, Walk"), placeheld(opt("unit", "text", "Unit"), "kg, or 1-5 for a score, or empty for done or not done"), placeheld(opt("note", "text", "Note"), "What it measures, and how"), flagField("daily", "Ask for it every day")},
		Owner:  true,
	},
	{Name: "settings", Title: "Settings", Class: []string{"edit"},
		Description: "The owner's settings of the application, kept in a file beside life.db (not in the database): model_url is the base URL of a server that speaks the chat-completions protocol and ends in /v1, model the model id, model_key a bearer credential for it, owner_number the digits of the owner's phone number with the country code (the one number that the agent on the phone answers; other text is 422). A field left out stays as it is, and an empty field clears that setting. GET /actions fills no value; GET /settings, the owner's read, fills them all. The answer is the settings.",
		Hint:        "Set the model that the agent uses. The key is kept in a file beside life.db, not in the database.",
		Method:      "POST", Path: "/settings",
		Fields: []siren.Field{opt("model_url", "text", "Model server, the base URL ending in /v1"), opt("model", "text", "Model id"), opt("model_key", "password", "Model key, when the server needs one"),
			opt("owner_number", "text", "Your phone number: digits with the country code; the agent on the phone answers this number alone")},
		Owner: true,
	},
	{Name: "integrity-check", Title: "Integrity",
		Description: "Run the four integrity checks of the database (docs/contract/integrity-checks.md).",
		Method:      "GET", Path: "/integrity",
		Fields: nil,
		Owner:  true,
	},
	{Name: "search", Title: "Search",
		Description: "Full-text search over page titles and bodies (FTS5 syntax). The pages whose title starts with the words are listed first and are not hits. order is best (the best match first, the default) or newest (the newest day first: a day page by its title, any other page by the day it was made). kind is day, page (a metric is one), person or file: only the hits of that kind; with none, every kind. counts are the hits of each kind.",
		Method:      "GET", Path: "/search",
		Fields: []siren.Field{opt("q", "search", "Search"),
			{Name: "order", Type: "text", Title: "Order", Options: core.SearchOrders()},
			{Name: "kind", Type: "text", Title: "Kind", Options: core.SearchKinds()},
			opt("limit", "number", "Rows on a page (1-500, default 50)"), opt("offset", "number", "Rows skipped")},
	},
	{Name: "browse", Title: "Browse",
		Description: "List the things of one kind, with the category tree. kind is page, person, media, document or metric (default page), or all, which needs in. in is the title of a category: only what is filed directly in it (default kind all). unfiled 1 lists only what is in no category. ghosts 1 lists the ghost pages (kind page only). media is image, video or audio (kind media only). limit and offset select the page of rows. A combination with no meaning is refused.",
		Method:      "GET", Path: "/browse",
		Fields: []siren.Field{{Name: "kind", Type: "text", Title: "Kind", Options: browseKinds},
			pageField("in", "In the category (title)", false),
			flagField("unfiled", "Only what is in no category"),
			flagField("ghosts", "Only the ghost pages"),
			flagField("deleted", "Only the deleted pages"),
			{Name: "media", Type: "text", Title: "Media kind", Options: core.MediaKinds()},
			opt("limit", "number", "Rows on a page (1-500, default 200)"), opt("offset", "number", "Rows skipped")},
	},
	{Name: "find", Title: "Find page",
		Description: "Open the page with this title (any case or normalisation).",
		Method:      "GET", Path: "/pages",
		Fields: []siren.Field{pageField("title", "Title", true)},
	},
	{Name: "open-day", Title: "Go to a day",
		Description: "Open the day view of a local day: its journal, the pages of that day and its readings.",
		Method:      "GET", Path: "/days/go",
		Fields: []siren.Field{opt("day", "date", "Day (default today)")},
	},
	{Name: "random-page", Title: "At random",
		Description: "Open a live thing of one kind picked at random, a way to rediscover it: kind is page (the default), person, media, document, metric, all, or day (the day view of a live day page up to today). all is never a day page, and no kind is a ghost page. 404 when the kind has nothing.",
		Method:      "GET", Path: "/pages/random",
		Fields: []siren.Field{{Name: "kind", Type: "text", Title: "Kind", Options: append(slices.Clone(browseKinds), "day")}},
	},
	{Name: "snapshot", Title: "Take a snapshot", Class: []string{"create"},
		Description: "The owner's dated copy of life.db (docs/cookbook/take-a-snapshot.md) into the server's snapshot folder, never overwriting, then its restore check. The answer names the file and the check.",
		Method:      "POST", Path: "/snapshots",
		Fields: nil,
		Owner:  true,
	},
	{Name: "rebuild-search", Title: "Rebuild search",
		Description: "The owner's repair of the full-text index: rebuild it from the pages (docs/contract/integrity-checks.md). Run it when integrity-check says the index is not ok. It reads every page and holds the write lock while it runs. The answer says how many documents the new index holds.",
		Method:      "POST", Path: "/search/rebuild",
		Fields: nil,
		Owner:  true,
	},
	{Name: "resync-links", Title: "Resync links",
		Description: "The owner's repair of the links of the bodies: every live page whose [[wikilinks]] differ from its body gets the links of its body, as a save of the same body would give them (docs/contract/integrity-checks.md). A page whose links are right is not written. A page that a body names and no page has is made, and a deleted one is revived, as a save does. The answer lists the pages rewritten.",
		Method:      "POST", Path: "/links/resync",
		Fields: nil,
		Owner:  true,
	},
}

func specOf(name string) spec {
	s, ok := lookup(name)
	if !ok {
		panic("no action " + name)
	}
	return s
}

// lookup is the catalog action with the name, for a name that comes from a request.
func lookup(name string) (spec, bool) {
	for _, s := range catalog {
		if s.Name == name {
			return s, true
		}
	}
	return spec{}, false
}

// base is a spec's action before its fields: what action and templated share.
func (s spec) base() siren.Action {
	a := siren.Action{Name: s.Name, Title: s.Title, Description: s.Description, Hint: s.Hint, Method: s.Method, Href: s.Path, Owner: s.Owner, Danger: s.Danger, Class: s.Class}
	if s.Method == "POST" {
		a.Type = formType
	}
	return a
}

// action instantiates a catalog action: path fields fill the href (escaped for a path) and drop out, values prefill
// the rest.
func action(name string, params map[string]string, values map[string]any) siren.Action {
	return filled(specOf(name), params, values)
}

// filled is action for a row that the caller holds, such as the row of an action of a connected app.
func filled(s spec, params map[string]string, values map[string]any) siren.Action {
	a := s.base()
	for _, f := range s.Fields {
		if f.In == "path" {
			a.Href = strings.ReplaceAll(a.Href, "{"+f.Name+"}", url.PathEscape(params[f.Name]))
			continue
		}
		if v, ok := values[f.Name]; ok {
			f.Value = v
		}
		a.Fields = append(a.Fields, f)
	}
	return a
}

// templated is a catalog action as /actions shows it: the href keeps its {placeholders}. The fields are a copy, so a
// caller that changes them leaves the catalog as it is.
func templated(s spec) siren.Action {
	a := s.base()
	a.Fields = slices.Clone(s.Fields)
	return a
}

func link(rel, href, title string) siren.Link {
	return siren.Link{Rel: []string{rel}, Href: href, Title: title}
}

func selfOf(e *siren.Entity) string {
	if l := e.LinkByRel("self"); l != nil {
		return l.Href
	}
	return ""
}

func withOptions(a siren.Action, field string, opts []string) siren.Action {
	if f := a.Field(field); f != nil {
		f.Options = opts
	}
	return a
}

func withResult(e *siren.Entity, err error) func(any) (*siren.Entity, error) {
	return func(res any) (*siren.Entity, error) {
		if e != nil {
			e.Result = res
		}
		return e, err
	}
}

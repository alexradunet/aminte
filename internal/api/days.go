package api

// The days: the root (today at a glance), the list of days, a day and its view, and capture.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"lifelog/internal/core"
	"lifelog/siren"
	"lifelog/text"
)

// recentDays is how many of the newest day pages the root shows.
const recentDays = 5

// recentDay is a day page as the root lists it: the day, the first line of its body, its mood and the address of the
// picture of its cover. Picture hides the id of the embedded entry, which has no JSON name.
type recentDay struct {
	core.DayEntry
	Mood    []float64 `json:"mood,omitempty"`
	Picture string    `json:"picture,omitempty"`
}

// recentDaysUpTo are the newest day pages up to today, the newest first, with their moods and the address of their
// pictures.
func (h *server) recentDaysUpTo(ctx context.Context, today string) ([]recentDay, error) {
	entries, err := h.s.DayEntries(ctx, "0000-01-01", today, true, recentDays)
	if err != nil {
		return nil, err
	}
	return h.withMoods(ctx, entries)
}

// withMoods are the entries as the root lists them, in the same order, each with its mood and the address of its
// picture.
func (h *server) withMoods(ctx context.Context, entries []core.DayEntry) ([]recentDay, error) {
	out := []recentDay{}
	if len(entries) == 0 {
		return out, nil
	}
	lo, hi := entries[0].Day, entries[0].Day
	for _, d := range entries {
		lo, hi = min(lo, d.Day), max(hi, d.Day)
	}
	moods, err := h.s.Moods(ctx, lo, hi)
	if err != nil {
		return nil, err
	}
	for _, d := range entries {
		out = append(out, recentDay{DayEntry: d, Mood: moods[d.Day], Picture: pictureHref(d.Picture)})
	}
	return out, nil
}

// earlierYears is how many earlier years a day looks back to for "On this day".
const earlierYears = 5

// sameDayEarlier are the day pages of day in the years before it, the nearest year first: year minus 1, then 2, and so
// on while a page exists, at most earlierYears of them. A year in which the date does not exist (29 February) is
// passed over, and does not end the list.
func (h *server) sameDayEarlier(ctx context.Context, day string) ([]recentDay, error) {
	t, ok := text.ParseDay(day)
	if !ok {
		return nil, nil
	}
	var entries []core.DayEntry
	for back := 1; back <= earlierYears && t.Year()-back >= 0; back++ {
		d := fmt.Sprintf("%04d-%02d-%02d", t.Year()-back, int(t.Month()), t.Day())
		if !text.IsDay(d) {
			continue
		}
		found, err := h.s.DayEntries(ctx, d, d, false, 1)
		if err != nil {
			return nil, err
		}
		if len(found) == 0 {
			break
		}
		entries = append(entries, found[0])
	}
	if len(entries) == 0 {
		return nil, nil
	}
	return h.withMoods(ctx, entries)
}

// The root is the entry point of the API: the newest day pages, and every action that starts something. It names the clock (zone, utc_offset), the day of today and its conversation as sub-entities that ?embed=today,
// conversation reads in the same answer, the connected apps that run (the links app) and the task service (the link
// tasks): what a client needs to draw its frame once (API.md, root).
func (h *server) root(r *http.Request) (*siren.Entity, error) {
	ctx := r.Context()
	now := h.s.Local() // one read of the clock, so today and the zone agree
	today := now.Format(time.DateOnly)
	recent, err := h.recentDaysUpTo(ctx, today)
	if err != nil {
		return nil, err
	}
	ms, err := h.s.Metrics(ctx)
	if err != nil {
		return nil, err
	}
	record := recordAction(ms, "", today)
	actions := []siren.Action{
		action("capture", map[string]string{"day": today}, nil), action("search", nil, nil), action("find", nil, nil), action("open-day", nil, nil),
		action("create-page", nil, nil), action("create-person", nil, nil), action("add-file", nil, nil),
		record, action("register-metric", nil, nil), action("snapshot", nil, nil)}
	_, offset := now.Zone()
	e := &siren.Entity{
		Class: []string{"root"}, Title: "Lifelog",
		Properties: map[string]any{"today": today, "zone": now.Location().String(), "utc_offset": offset, "recent": recent},
		Entities:   []siren.Link{link("today", siren.DayHref(today), "Today"), link("conversation", conversationHref(today), "Chat")},
		Links: []siren.Link{link("self", "/", "Lifelog"), link("today", siren.DayHref(today), "Today"), link("days", "/days", "Days"),
			link("browse", siren.BrowseHref(nil), "Browse"), link("people", siren.BrowseKindHref("person"), "People"),
			link("media", siren.BrowseKindHref("media"), "Media"), link("files", siren.BrowseKindHref("document"), "Documents"),
			link("metrics", "/metrics", "Metrics"), link("ghosts", siren.GhostsHref(), "Ghost pages"), link("actions", "/actions", "All actions"),
			link("calendar", siren.MonthHref(now), "Calendar"),
			link("year", siren.YearHref(now.Year()), "This year")},
		Actions: actions,
	}
	e.Links = append(e.Links, h.with.links()...)
	if _, ok := h.with.taskService(); ok {
		e.Links = append(e.Links, link("tasks", "/tasks", "Tasks"))
	}
	return e, nil
}

func (h *server) today(*http.Request) (*siren.Entity, error) {
	return nil, redirect{siren.DayHref(h.s.Today())}
}

// dayNamed is the day that a path or a field names. The word "today" is today: the local day of the store's clock, the
// day that GET /days/today redirects to. Every field of the catalog whose type is date is read through it, so the word
// has one meaning in the API (TestEveryDayFieldReadsTheWordToday). Any other value comes back as it is: the schema's
// CHECK refuses a write of a value that is not a day, and open-day checks its own.
func (h *server) dayNamed(day string) string {
	if day == "today" {
		return h.s.Today()
	}
	return day
}

// neighbour is the page next to this one in time: the day its address stands for (the first day of a month or of
// a week), its address and its title.
type neighbour struct {
	day         time.Time
	href, title string
}

// neighbourLinks are the links prev and next of a page of the calendar. A link leads only to a page that answers: a
// neighbour whose day is not a day (before 0000-01-01, after 9999-12-31) has no link.
func neighbourLinks(prev, next neighbour) []siren.Link {
	var out []siren.Link
	if text.IsDay(prev.day.Format(time.DateOnly)) {
		out = append(out, link("prev", prev.href, prev.title))
	}
	if text.IsDay(next.day.Format(time.DateOnly)) {
		out = append(out, link("next", next.href, next.title))
	}
	return out
}

// The stream (docs/plans/130-the-explore-pass.md): /days is the days of the record as cards, the newest first. A page
// is offset days skipped, at most limit of them; next is the older days, prev the newer.

// streamLimit is the count of the cards of a page of the stream when the request names none.
const streamLimit = 30

// cardChip is a person or a birthday of a card, with the address of its page.
type cardChip struct {
	Title string `json:"title"`
	Href  string `json:"href"`
}

// cardPage is a page or a file of a card (Kind), with its address.
type cardPage struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Href  string `json:"href"`
}

// dayCard is a card of the stream as the API shows it (core.Card). A slim card, a day with no day page, has no page_id
// and no words.
type dayCard struct {
	Day       string     `json:"day"`
	PageID    int64      `json:"page_id,omitempty"`
	Words     string     `json:"words,omitempty"`
	Mood      []float64  `json:"mood,omitempty"`
	Picture   string     `json:"picture,omitempty"`
	People    []cardChip `json:"people,omitempty"`
	MoreChips int        `json:"more_chips,omitempty"`
	Birthdays []cardChip `json:"birthdays,omitempty"`
	Pages     []cardPage `json:"pages,omitempty"`
	MorePages int        `json:"more_pages,omitempty"`
	Readings  int        `json:"readings,omitempty"`
}

// cardOf is a card of the store as the API shows it.
func cardOf(c core.Card) dayCard {
	chip := func(it core.CardItem) cardChip { return cardChip{Title: it.Title, Href: siren.PageHref(it.ID)} }
	d := dayCard{Day: c.Day, PageID: c.PageID, Words: c.Words, Mood: c.Mood, Picture: pictureHref(c.Picture),
		MoreChips: c.MoreChips, MorePages: c.MorePages, Readings: c.Readings}
	for _, it := range c.Chips {
		d.People = append(d.People, chip(it))
	}
	for _, it := range c.Birthdays {
		d.Birthdays = append(d.Birthdays, chip(it))
	}
	for _, it := range c.Pages {
		d.Pages = append(d.Pages, cardPage{Kind: it.Kind, Title: it.Title, Href: siren.PageHref(it.ID)})
	}
	return d
}

// streamHref is the address of a page of the stream, with limit and offset when they are not the defaults.
func streamHref(limit, offset int) string {
	v := url.Values{}
	if limit != streamLimit {
		v.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		v.Set("offset", strconv.Itoa(offset))
	}
	if len(v) == 0 {
		return "/days"
	}
	return "/days?" + v.Encode()
}

func (h *server) days(r *http.Request) (*siren.Entity, error) {
	ctx := r.Context()
	limit, offset, err := pageOf(r, streamLimit)
	if err != nil {
		return nil, err
	}
	today := h.s.Today()
	cards, more, err := h.s.Stream(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	e := &siren.Entity{Class: []string{"days"}, Title: "Days",
		Links:   []siren.Link{link("self", streamHref(limit, offset), "Days"), link("index", "/", "Home")},
		Actions: []siren.Action{action("open-day", nil, nil), action("random-page", nil, map[string]any{"kind": "day"})}}
	if offset > 0 {
		e.Links = append(e.Links, link("prev", streamHref(limit, max(offset-limit, 0)), "Newer days"))
	}
	if more {
		e.Links = append(e.Links, link("next", streamHref(limit, offset+limit), "Older days"))
	}
	// The zoom is anchored on the top card's day, so Month and Year open the period the page shows.
	anchor := today
	if len(cards) > 0 {
		anchor = cards[0].Day
	}
	at, _ := text.ParseDay(anchor)
	e.Links = append(e.Links, zoomLinks(at)...)
	shown := []dayCard{}
	for _, c := range cards {
		shown = append(shown, cardOf(c))
		e.Entities = append(e.Entities, link("item", siren.DayHref(c.Day), c.Day))
	}
	e.Properties = map[string]any{"today": today, "limit": limit, "offset": offset, "cards": shown}
	return e, nil
}

// dayView is a day as the API shows it: the cookbook's day view, and beside it the day's mood and the day the
// server read as today
// (docs/plans/091-the-browser-for-daily-use.md).
type dayView struct {
	core.Day
	Mood  []float64 `json:"mood"`
	Today string    `json:"today"`
	// Recent are the newest day pages, on the day of today only: Home is the page of today.
	Recent []recentDay `json:"recent,omitempty"`
	// Earlier are the day pages of this day in the years before, the nearest year first: "On this day".
	Earlier []recentDay `json:"earlier,omitempty"`
	// Cover is the cover of the day page, which the journal draws above it; absent when it has none.
	Cover *core.Cover `json:"cover,omitempty"`
	// Units are the units of the metrics read on the day, by metric name; a metric with no unit is absent.
	Units map[string]string `json:"units,omitempty"`
	// Daily are the names of the daily metrics, in name order: the day asks for a reading of each.
	Daily []string `json:"daily"`
	// Streak, Written and Habits are the Today strip of the rail, on the day of today only, as Recent is: the strip is
	// about today. A client that draws the strip reads the day of today (the root names it). Streak is the days
	// written in a row that end today, or yesterday when today has no words yet (cookbook/streaks.md). Written says that
	// the day page of today has words. Habits are the daily metrics with no unit, by name, each with whether the
	// newest reading of today is not 0 (null when it has no reading). They are pointers and a list with omitempty so
	// that a streak of 0 and a day with no words are on the wire on today, and absent on any other day.
	Streak  *int         `json:"streak,omitempty"`
	Written *bool        `json:"written,omitempty"`
	Habits  []core.Habit `json:"habits,omitempty"`
}

func (h *server) day(r *http.Request) (*siren.Entity, error) {
	return h.dayEntity(r.Context(), r.PathValue("day"))
}

// dayEntity is the resource /days/{day} serves.
func (h *server) dayEntity(ctx context.Context, day string) (*siren.Entity, error) {
	d, err := h.s.Day(ctx, day)
	if err != nil {
		return nil, err
	}
	today := h.s.Today()
	moods, err := h.s.Moods(ctx, day, day)
	if err != nil {
		return nil, err
	}
	mood := moods[day]
	if mood == nil {
		mood = []float64{}
	}
	var recent []recentDay
	if day == today {
		if recent, err = h.recentDaysUpTo(ctx, today); err != nil {
			return nil, err
		}
	}
	earlier, err := h.sameDayEarlier(ctx, day)
	if err != nil {
		return nil, err
	}
	ms, err := h.s.Metrics(ctx) // the registry, read once: the options of record and the units of the readings
	if err != nil {
		return nil, err
	}
	record, daily := recordAction(ms, "", day), dailyOf(ms)
	units := unitsOf(ms, d.Readings, daily)
	props := dayView{Day: *d, Mood: mood, Today: today, Recent: recent, Earlier: earlier, Units: units, Daily: daily}
	if day == today {
		streak, written, err := h.s.Streak(ctx, day)
		if err != nil {
			return nil, err
		}
		habits, err := h.s.Habits(ctx, day)
		if err != nil {
			return nil, err
		}
		props.Streak, props.Written, props.Habits = &streak, &written, habits
	}
	t, _ := text.ParseDay(day)
	e := &siren.Entity{
		Class: []string{"day"}, Title: day,
		Properties: props,
		Links:      []siren.Link{link("self", siren.DayHref(day), day)},
		Actions:    []siren.Action{action("capture", map[string]string{"day": day}, nil), record},
	}
	// 0000-01-01 has no day before it, and 9999-12-31 none after it.
	before, after := t.AddDate(0, 0, -1), t.AddDate(0, 0, 1)
	e.Links = append(e.Links, neighbourLinks(
		neighbour{before, siren.DayHref(before.Format(time.DateOnly)), "Previous day"},
		neighbour{after, siren.DayHref(after.Format(time.DateOnly)), "Next day"})...)
	e.Links = append(e.Links, link("metrics", "/metrics", "Metrics"), link("index", "/", "Home"), link("month", siren.MonthHref(t), t.Format(layoutMonth)),
		link("week", weekHref(mondayOf(t)), weekName(mondayOf(t))))
	if d.PageID != 0 {
		e.Links = append(e.Links, link("page", siren.PageHref(d.PageID), "The day page"))
		p, err := h.s.PageByID(ctx, d.PageID)
		if err != nil {
			return nil, err
		}
		e.Actions = append(e.Actions, saveBodyAction(d.PageID, p.Body, p.Version))
		e.Links = append(e.Links, coverLinks(p)...)
		e.Actions = append(e.Actions, action("set-cover", idParam(d.PageID), nil))
		dv := e.Properties.(dayView)
		dv.Cover = p.Cover
		e.Properties = dv
	}
	for _, m := range d.Readings {
		e.Entities = append(e.Entities, link("reading", siren.MeasurementHref(m.ID), m.Metric+" "+siren.Num(m.Value)))
	}
	return e, nil
}

func (h *server) capture(r *http.Request, act spec, src string) (*siren.Entity, error) {
	v, err := fieldsOf(r, act)
	if err != nil {
		return nil, err
	}
	mood, err := number(v, "mood")
	if err != nil {
		return nil, err
	}
	day := h.dayNamed(r.PathValue("day")) // the share target of the manifest cannot know the day: it sends the word
	entry := words(v.Get("text"))
	if entry != "" { // the entry begins with its stamp, as a turn of say does; a mood alone writes no text
		entry = h.stamp(relayOf(src)) + " " + entry
	}
	_, sync, already, err := h.s.CaptureWithKey(r.Context(), src, v.Get("import_key"), day, entry, mood)
	if err != nil {
		return nil, err
	}
	if already { // a re-send: the first one stands (docs/decisions/D28-capture-keys.md), and the answer links its day
		if day, err = h.s.CapturedDay(r.Context(), src, v.Get("import_key")); err != nil {
			return nil, err
		}
		return &siren.Entity{Class: []string{"result"}, Title: "Already added",
			Properties: map[string]any{"message": "a capture with this import_key was added before; nothing changed"},
			Result:     map[string]any{"already": true},
			Links:      []siren.Link{link("self", siren.DayHref(day), day), link("day", siren.DayHref(day), day), link("index", "/", "Home")}}, nil
	}
	return withResult(h.dayEntity(r.Context(), day))(sync)
}

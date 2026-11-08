package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aminte/internal/api/apitest"
)

// The period pages (D30). Today is Wednesday 2031-05-14, in ISO week 2031-W20. A period page is a plain page with a key
// as its title, made with create-page. The view of a period (a week, a month, a year) carries its page when it has one.

func TestAWeekListsItsSevenDaysAndItsPage(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "2031-W20", "body": "Finish the report"}))

	week := apitest.Must(t)(c.Get(t.Context(), "/weeks/2031-W20"))
	if week.Class[0] != "week" || week.Title != "Week 20 of 2031" {
		t.Errorf("the week is %v titled %q, want a week titled Week 20 of 2031", week.Class, week.Title)
	}
	var days []string
	for _, l := range week.Entities {
		if l.HasRel("day") {
			days = append(days, l.Href)
		}
	}
	var want []string
	for d := 12; d <= 18; d++ { // Monday to Sunday: not the 11th, not the 19th
		want = append(want, fmt.Sprintf("/days/2031-05-%02d", d))
	}
	if strings.Join(days, " ") != strings.Join(want, " ") {
		t.Errorf("the week lists %v, want %v", days, want)
	}
	if props(week)["body"] != "Finish the report" || apitest.Href(week, "page") == "" {
		t.Errorf("the week carries the body %v and the page %q, want the page of the week", props(week)["body"], apitest.Href(week, "page"))
	}
	if page := week.LinkByRel("page"); page == nil || page.Title != "The week page" {
		t.Errorf("the link page of the week is %+v, want the title The week page", page)
	}
	if apitest.Href(week, "prev") != "/weeks/2031-W19" {
		t.Errorf("the link prev of the week is %q, want /weeks/2031-W19", apitest.Href(week, "prev"))
	}
	if hasAction(week, "create-page") {
		t.Errorf("a week with a page offers to make it: %s", names(week))
	}

	req := httptest.NewRequest("GET", "/weeks/today", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/weeks/2031-W20" {
		t.Errorf("/weeks/today: %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	for _, bad := range []string{"/weeks/2031-W53", "/weeks/2031-W00", "/weeks/2031-W5", "/weeks/x"} {
		if _, err := c.Get(t.Context(), bad); apitest.Status(err) != 422 {
			t.Errorf("GET %s: %v, want 422", bad, err)
		}
	}
}

// TestAMonthAndAYearShowTheirPages: a month and a year with no page offer create-page; with pages they carry them, and
// the year lists its four quarters with the page of each (0 for a quarter with no page, which create-page makes).
func TestAMonthAndAYearShowTheirPages(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	month := apitest.Must(t)(c.Get(t.Context(), "/months/2031-05"))
	if apitest.Href(month, "page") != "" || !hasAction(month, "create-page") {
		t.Errorf("a month with no page: link page %q, actions %s", apitest.Href(month, "page"), names(month))
	}
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "2031-05", "body": "Plan the month"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "2031", "body": "Run a marathon"}))
	month = apitest.Must(t)(c.Get(t.Context(), "/months/2031-05"))
	if props(month)["body"] != "Plan the month" || apitest.Href(month, "page") == "" || hasAction(month, "create-page") {
		t.Errorf("a month with a page: body %v, link page %q, actions %s", props(month)["body"], apitest.Href(month, "page"), names(month))
	}
	quarters := func() map[string]string {
		t.Helper()
		out := map[string]string{}
		list, _ := props(apitest.Must(t)(c.Get(t.Context(), "/years/2031")))["quarters"].([]any)
		for _, q := range list {
			m := q.(map[string]any)
			out[fmt.Sprint(m["key"])] = fmt.Sprint(m["page_id"])
		}
		return out
	}
	year := apitest.Must(t)(c.Get(t.Context(), "/years/2031"))
	if props(year)["body"] != "Run a marathon" || apitest.Href(year, "page") == "" {
		t.Errorf("a year with a page: body %v, link page %q", props(year)["body"], apitest.Href(year, "page"))
	}
	before := quarters()
	if len(before) != 4 || before["2031-Q4"] != "<nil>" {
		t.Fatalf("the quarters before the page of Q4: %v, want four with no page for 2031-Q4", before)
	}
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "2031-Q4", "body": "Ship it"}))
	if after := quarters(); after["2031-Q4"] == "<nil>" || after["2031-Q4"] == "" || after["2031-Q1"] != "<nil>" {
		t.Errorf("the quarters after the page of Q4: %v, want the page of 2031-Q4", after)
	}
}

// TestAPeriodPageNamesItsKind: the entity of a page titled with a key of a period says which period (props period), and
// carries its body.
func TestAPeriodPageNamesItsKind(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	for key, want := range map[string]string{"2031-W20": "week", "2031-05": "month", "2031": "year", "2031-Q4": "quarter"} {
		e := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": key, "body": "Aim " + key}))
		if props(e)["period"] != want || props(e)["body"] != "Aim "+key {
			t.Errorf("the page %s: period %v, body %v; want %s and Aim %s", key, props(e)["period"], props(e)["body"], want, key)
		}
	}
}

// TestCreatePageMakesAPeriodPageAndRefusesAPeriodThatDoesNotExist: a create-page of a week key makes the period page
// that the week view then carries. The file refuses a week that the year does not have, by the name of its constraint.
func TestCreatePageMakesAPeriodPageAndRefusesAPeriodThatDoesNotExist(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	week := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "2031-W43"}))
	if p := props(week); week.Title != "2031-W43" || p["period"] != "week" {
		t.Errorf("the week page: %q %v", week.Title, p["period"])
	}
	view := apitest.Must(t)(c.Get(t.Context(), "/weeks/2031-W43"))
	if apitest.Href(view, "page") == "" || hasAction(view, "create-page") {
		t.Errorf("the week view does not carry the page that create-page made: link page %q, actions %s", apitest.Href(view, "page"), names(view))
	}
	_, err := c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "2027-W53"})
	if apitest.Status(err) != http.StatusUnprocessableEntity || !strings.Contains(err.Error(), "entities_period_exists") {
		t.Errorf("create-page of 2027-W53: %v, want 422 naming entities_period_exists", err)
	}
}

// TestAPeriodViewWithoutAPageOffersCreatePage: a week, a month and a year with no page offer create-page with the key
// as the title. With a page they offer save-body and not create-page.
func TestAPeriodViewWithoutAPageOffersCreatePage(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	for path, key := range map[string]string{"/weeks/2031-W20": "2031-W20", "/months/2031-05": "2031-05", "/years/2031": "2031"} {
		e := apitest.Must(t)(c.Get(t.Context(), path))
		if names(e) != "create-page" {
			t.Errorf("%s offers %s, want create-page", path, names(e))
			continue
		}
		var title any
		for _, f := range apitest.Action(t, e, "create-page").Fields {
			if f.Name == "title" {
				title = f.Value
			}
		}
		if title != key {
			t.Errorf("%s: the title of create-page is %v, want %s", path, title, key)
		}
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": key}))
		if after := apitest.Must(t)(c.Get(t.Context(), path)); names(after) != "save-body" {
			t.Errorf("%s with a page offers %s, want save-body", path, names(after))
		}
	}
}

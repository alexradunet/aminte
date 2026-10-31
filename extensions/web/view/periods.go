package view

// The period pages in the browser (docs/decisions/D30-period-pages.md): the words for their kind and the links to their
// views, read from the entity's properties.

import (
	"fmt"
	"lifelog/siren"
	"strconv"
	"strings"
)

// periodWords is the words for the kind of a period page, "week page"; "" for a page that is not one.
func periodWords(period any) string {
	p, _ := period.(string)
	if p == "" {
		return ""
	}
	return strings.ToLower(p) + " page"
}

// PeriodViews are the links to the views of a period page: a week, a month or a year has one, and a quarter links its
// three months. Nil for a page that is not a period page.
func (v *view) PeriodViews() []siren.Link {
	props, _ := v.Props.(map[string]any)
	period, _ := props["period"].(string)
	switch period {
	case "week":
		return []siren.Link{{Href: "/weeks/" + v.Title, Title: "The week view"}}
	case "month":
		return []siren.Link{{Href: "/months/" + v.Title, Title: "The month view"}}
	case "year":
		return []siren.Link{{Href: "/years/" + v.Title, Title: "The year view"}}
	case "quarter":
		year, q, ok := strings.Cut(v.Title, "-Q")
		n, err := strconv.Atoi(q)
		if !ok || err != nil || n < 1 || n > 4 {
			return nil
		}
		var out []siren.Link
		for m := 3*n - 2; m <= 3*n; m++ {
			month := fmt.Sprintf("%s-%02d", year, m)
			out = append(out, siren.Link{Href: "/months/" + month, Title: monthName(month)})
		}
		return out
	}
	return nil
}

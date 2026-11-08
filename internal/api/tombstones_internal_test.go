package api

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"aminte/internal/core"
)

// TestIdsOf: the field ids of a bulk action. Each value is split on commas and white space, an id named twice counts
// once, and the order is the order of the first time. No id, a word that is not an id and more than 500 ids are each
// a 422 with its sentence.
func TestIdsOf(t *testing.T) {
	words := func(from, n int) string {
		var w []string
		for i := from; i < from+n; i++ {
			w = append(w, strconv.Itoa(i))
		}
		return strings.Join(w, " ")
	}
	for _, tc := range []struct {
		name   string
		values []string
		want   string // the ids, or the status and the sentence of the refusal
	}{
		{"one id", []string{"7"}, "[7]"},
		{"commas and spaces, an id two times", []string{"1, 2 1"}, "[1 2]"},
		{"one value a checked box", []string{"3", "1"}, "[3 1]"},
		{"values that share an id", []string{"1,2", "2 3"}, "[1 2 3]"},
		{"a tab, a newline and a no-break space", []string{"4\t5\n6\u00a07"}, "[4 5 6 7]"},
		{"the largest id", []string{"9223372036854775807"}, "[9223372036854775807]"},
		{"500 ids", []string{words(1, 500)}, fmt.Sprint(500, " ids")},
		{"501 words, one of them two times", []string{words(1, 500) + " 1"}, fmt.Sprint(500, " ids")},
		{"no value", nil, "422 missing field ids"},
		{"an empty value", []string{""}, "422 missing field ids"},
		{"only separators", []string{" , "}, "422 missing field ids"},
		{"a word", []string{"1 abc"}, `422 ids: "abc" is not an id`},
		{"zero", []string{"0"}, `422 ids: "0" is not an id`},
		{"a negative id", []string{"-1"}, `422 ids: "-1" is not an id`},
		{"a fraction", []string{"1.5"}, `422 ids: "1.5" is not an id`},
		{"past the largest id", []string{"9223372036854775808"}, `422 ids: "9223372036854775808" is not an id`},
		{"a semicolon", []string{"1;2"}, `422 ids: "1;2" is not an id`},
		{"501 ids", []string{words(1, 501)}, "422 ids: at most 500 in one action"},
	} {
		ids, err := idsOf(tc.values)
		got := fmt.Sprint(ids)
		if len(ids) > 10 {
			got = fmt.Sprint(len(ids), " ids")
		}
		var ce *core.Error
		if errors.As(err, &ce) {
			got = fmt.Sprint(ce.Status, " ", ce.Msg)
		} else if err != nil {
			got = err.Error()
		}
		if got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
}

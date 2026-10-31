package text

import (
	"strings"
	"testing"
)

func TestHasEmbedPostParseNFC(t *testing.T) {
	for _, n := range []int{100, 118, 119} {
		title := strings.Repeat("é", n) + ".jpg"
		body := "![[" + strings.Repeat("e\u0301", n) + ".jpg]]"
		keys, _, _ := Targets(body, "")
		want := len(title) <= 240
		if (len(keys) == 1) != want || HasEmbed(body, title) != want {
			t.Errorf("n=%d graph=%v embed=%v want=%v", n, keys, HasEmbed(body, title), want)
		}
	}
	for _, body := range []string{"`![[Cafe\u0301.jpg]]`", "![[Cafe\u0301.jpg]](u)", "![[Cafe\u0301.jpg]]\n\n[Cafe\u0301.jpg]: u"} {
		if HasEmbed(body, "Café.jpg") {
			t.Errorf("CommonMark precedence: %q", body)
		}
	}
	if !HasEmbed("\u1fef![[Cafe\u0301.jpg]]\u1fef", "Café.jpg") {
		t.Error("normalized syntax was parsed as code")
	}
}

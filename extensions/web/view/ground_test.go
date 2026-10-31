package view

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestTheGroundIsTheOwnersChoice: app.css draws each floor that differs from the boards (docs/plans/153-the-ground-in-
// settings.md): oak drops the tile, firelight and embers lay two fixed layers under the frame, embers with the picture
// of the sparks. The boards are the body's own rule, so a page with no choice has them. The layers exist only where the
// floor shows: on a screen wider than the frame, never in print. (That every animation asks for motion first is held
// by TestTheMotionsAreOffUnderReducedMotion.)
func TestTheGroundIsTheOwnersChoice(t *testing.T) {
	app := cssComments.ReplaceAllString(string(appCSS), "")
	rule := func(selector string) string {
		t.Helper()
		m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(selector) + `\{([^}]*)\}`).FindStringSubmatch(app)
		if m == nil {
			t.Fatalf("app.css has no rule %s", selector)
		}
		return m[1]
	}
	if r := rule("body[data-ground=oak]"); strings.Contains(r, "ground.png") || !strings.Contains(r, "background-image:var(--grain-ground)") {
		t.Errorf("oak keeps the tile or drops the grain: %q", r)
	}
	layers := rule("body:is([data-ground=firelight],[data-ground=embers])::before,body:is([data-ground=firelight],[data-ground=embers])::after")
	for _, want := range []string{"content:\"\"", "position:fixed", "inset:0", "z-index:-1", "pointer-events:none"} {
		if !strings.Contains(layers, want) {
			t.Errorf("the layers of the moving floors lack %s: %q", want, layers)
		}
	}
	if r := rule("body[data-ground=firelight]::before"); !strings.Contains(r, "radial-gradient") {
		t.Errorf("firelight draws no glow: %q", r)
	}
	if r := rule("body[data-ground=embers]::before,body[data-ground=embers]::after"); !strings.Contains(r, "url(/assets/embers.png") {
		t.Errorf("embers draws no sparks: %q", r)
	}
	// The layers go where the floor is not seen: under the frame's width, and in print.
	// The removing rules weigh what the drawing rule weighs (body, one attribute, one pseudo-element), so they win by
	// coming later; a lighter selector would lose, and the layers would stay, as a rendered check once showed.
	off := `body\[data-ground\]::before,body\[data-ground\]::after\{content:none\}`
	frame := regexp.MustCompile(`@media \(max-width:(\d+)px\)\{` + off + `\}`).FindStringSubmatch(app)
	if frame == nil {
		t.Fatal("app.css keeps the layers on a screen no wider than the frame")
	}
	base := cssComments.ReplaceAllString(string(baseCSS), "")
	px := func(token string) int {
		t.Helper()
		m := regexp.MustCompile(`--` + token + `:(\d+)px`).FindStringSubmatch(base)
		if m == nil {
			t.Fatalf("base.css has no --%s in px", token)
		}
		n, _ := strconv.Atoi(m[1])
		return n
	}
	if !strings.Contains(base, "--frame:calc(var(--rail-width) + var(--block) + 2 * var(--space-8))") {
		t.Fatal("base.css: --frame is not the sum this test adds up")
	}
	if got, want := frame[1], strconv.Itoa(px("rail-width")+px("block")+2*px("space-8")); got != want {
		t.Errorf("the layers stop at %s px, the frame is %s px", got, want)
	}
	if print := regexp.MustCompile(`@media print\{.*`).FindString(app); !regexp.MustCompile(off).MatchString(print) {
		t.Error("print draws the layers of the moving floors")
	}
}

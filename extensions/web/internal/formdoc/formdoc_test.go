package formdoc_test

import (
	"net/url"
	"reflect"
	"testing"

	"lifelog-web/internal/formdoc"
)

// TestAFormIsReadAsABrowserSendsIt: the default fields of a form are the ones a browser sends when the reader changes
// nothing. The page is written by hand, and the expected fields are worked out from the rules of HTML.
func TestAFormIsReadAsABrowserSendsIt(t *testing.T) {
	page := `<p>Not a form</p>
<form method="post" action="/days/2031-05-14/capture?a=1&amp;b=2" data-action="capture" enctype="multipart/form-data">
<input type="hidden" name="back" value="/days/2031-05-14">
<input type="hidden" name="import_key" value="K &amp; L">
<textarea name="text" rows="3">
first line &lt;b&gt;</textarea>
<select name="mood"><option></option><option>1</option><option selected>4</option></select>
<select name="ground"><option>boards</option><option value="oak">Oak</option></select>
<input type="checkbox" name="flag" value="1"><input type="checkbox" name="kept" checked>
<input type="radio" name="kind" value="a"><input type="radio" name="kind" value="b" checked>
<input type="file" name="original">
<input type="text" name="off" value="x" disabled>
<button name="go" value="1">Send</button>
</form>
<form action="/search"><input type="search" name="q"></form>`
	forms := formdoc.Forms(page)
	if len(forms) != 2 {
		t.Fatalf("found %d forms, want 2", len(forms))
	}
	f := forms[0]
	if f.Method != "POST" || f.Action != "/days/2031-05-14/capture?a=1&b=2" || f.Attr["data-action"] != "capture" || !f.Multipart() {
		t.Errorf("the form: %+v", f)
	}
	want := url.Values{"back": {"/days/2031-05-14"}, "import_key": {"K & L"}, "text": {"first line <b>"},
		"mood": {"4"}, "ground": {"boards"}, "kept": {"on"}, "kind": {"b"}}
	if !reflect.DeepEqual(f.Values, want) {
		t.Errorf("the values %v, want %v", f.Values, want)
	}
	if !reflect.DeepEqual(f.Files, []string{"original"}) {
		t.Errorf("the files %v, want [original]", f.Files)
	}
	if g := forms[1]; g.Method != "GET" || g.Action != "/search" || !reflect.DeepEqual(g.Values, url.Values{"q": {""}}) {
		t.Errorf("the second form: %+v", g)
	}
}

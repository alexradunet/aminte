package web

import (
	"encoding/hex"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// The tests of the receipts of the toast (feedback.go).

func receiptToken(location string) string {
	u, _ := url.Parse(location)
	return u.Query().Get("feedback")
}

// feedbackLocation reports a Location that is the page back (a path, with its query when it has one) and one receipt of
// feedback.
func feedbackLocation(location, back string) bool {
	want, err := url.Parse(back)
	if err != nil {
		return false
	}
	u, err := url.Parse(location)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Path != want.Path || u.Fragment != "" {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	values := q["feedback"]
	q.Del("feedback")
	if q.Encode() != want.Query().Encode() || len(values) != 1 || len(values[0]) != 64 {
		return false
	}
	_, err = hex.DecodeString(values[0])
	return err == nil
}

func TestFeedbackURLPrivacyUsesStructureNotRandomSubstrings(t *testing.T) {
	opaque := "bad" + strings.Repeat("0", 61)
	if !feedbackLocation("/pages/1?feedback="+opaque, "/pages/1") {
		t.Fatal("valid random hex may contain the substring bad")
	}
	for _, location := range []string{
		"/pages/1?feedback=" + opaque + "&message=bad",
		"/pages/1?feedback=" + opaque + "&body=private%GG",
		"/pages/1?feedback=" + opaque + "&private%GG=value",
		"/pages/1?feedback=" + opaque + "&note=private%",
		"/pages/1?feedback=bad",
		"/pages/Revived?feedback=" + opaque,
	} {
		if feedbackLocation(location, "/pages/1") {
			t.Fatalf("accepted private or malformed URL %s", location)
		}
	}
}

func TestBrowserMutationFeedbackReceipts(t *testing.T) {
	now := time.Unix(0, 0)
	s := feedbackStore{now: func() time.Time { return now }}
	token := receiptToken(s.put("/pages/1", map[string]any{"revived": []string{"<script>private</script>"}}))
	if len(token) != 64 {
		t.Fatal("not an opaque random token")
	}
	if s.take("/pages/2", token) != "" || s.take("/pages/1", "unknown") != "" {
		t.Fatal("invalid token disclosed feedback")
	}
	if got := s.take("/pages/1", token); !strings.Contains(got, "private") {
		t.Fatal("wrong target consumed receipt")
	}
	if s.take("/pages/1", token) != "" {
		t.Fatal("replayed")
	}
	token = receiptToken(s.put("/pages/1", true))
	now = now.Add(feedbackLifetime)
	if s.take("/pages/1", token) != "" {
		t.Fatal("expired receipt")
	}
	token = receiptToken(s.put("/pages/1", true))
	for range feedbackEntries {
		now = now.Add(time.Second)
		s.put("/pages/1", true)
	}
	if len(s.entries) != feedbackEntries || s.take("/pages/1", token) != "" {
		t.Fatal("entry cap/eviction")
	}
	token = receiptToken(s.put("/pages/1", strings.Repeat("x", feedbackLimit*2)))
	got := s.take("/pages/1", token)
	if len(got) > feedbackLimit || !strings.Contains(got, "Feedback truncated") {
		t.Fatal("payload cap/notice")
	}
	restarted := feedbackStore{now: s.now}
	if restarted.take("/pages/1", token) != "" {
		t.Fatal("restart retained receipt")
	}
}

// TestTheFeedbackDestinationKeepsItsFragment: the receipt goes in the query, before the fragment that names the row the
// page opens at. The receipt is matched by the path alone, as a browser never sends a fragment.
func TestTheFeedbackDestinationKeepsItsFragment(t *testing.T) {
	s := feedbackStore{now: func() time.Time { return time.Unix(0, 0) }}
	got := s.put("/metrics#row-1-3", map[string]any{"ok": true})
	u, err := url.Parse(got)
	if err != nil || u.Path != "/metrics" || u.Fragment != "row-1-3" || len(u.Query().Get("feedback")) != 64 {
		t.Fatalf("put gave %q", got)
	}
	if !strings.HasPrefix(got, "/metrics?feedback=") || !strings.HasSuffix(got, "#row-1-3") {
		t.Errorf("put gave %q, want /metrics?feedback=...#row-1-3", got)
	}
	if s.take("/metrics", u.Query().Get("feedback")) == "" {
		t.Error("the receipt was not found by the path alone")
	}
}

// TestAResultThatIsRawJSONKeepsItsNumbersAndNoResultHasNoReceipt: write.go gives the receipt the raw JSON of the result, so a
// number is as the core wrote it; no result, or a null, leaves the destination as it is.
func TestAResultThatIsRawJSONKeepsItsNumbersAndNoResultHasNoReceipt(t *testing.T) {
	s := feedbackStore{now: func() time.Time { return time.Unix(0, 0) }}
	token := receiptToken(s.put("/pages/1", resultOf([]byte(`{"class":["page"],"result":{"id":9007199254740993,"linked":["Sam"]}}`))))
	if got := s.take("/pages/1", token); !strings.Contains(got, "9007199254740993") || !strings.Contains(got, "\n  \"linked\"") {
		t.Errorf("the receipt: %q, want the number as written and the JSON indented", got)
	}
	for _, body := range []string{`{"class":["page"]}`, `{"result":null}`, `not json`} {
		if got := s.put("/pages/1", resultOf([]byte(body))); got != "/pages/1" {
			t.Errorf("%s: put gave %q, want the destination alone", body, got)
		}
	}
}

func TestBrowserMutationFeedbackIndependentTabs(t *testing.T) {
	s := feedbackStore{now: func() time.Time { return time.Unix(0, 0) }}
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			destination := "/pages/" + strings.Repeat("x", i+1)
			token := receiptToken(s.put(destination, destination))
			if !strings.Contains(s.take(destination, token), destination) {
				t.Error("tab lost its own feedback")
			}
		})
	}
	wg.Wait()
}

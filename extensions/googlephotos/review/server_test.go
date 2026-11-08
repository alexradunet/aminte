package review_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"aminte-googlephotos/review"
)

// fakeAminte answers the four calls of the extension. Every day of May 2031 has no picture, except the days in
// covered. It records the files it was sent.
type fakeAminte struct {
	*httptest.Server
	mu      sync.Mutex
	covered map[string]bool
	files   []string // "<day> <title> <preview?>"
}

func newFakeAminte(t *testing.T, covered ...string) *fakeAminte {
	f := &fakeAminte{covered: map[string]bool{}}
	for _, d := range covered {
		f.covered[d] = true
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /months/{m}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var days []string
		for d := 1; d <= 31; d++ {
			day := fmt.Sprintf("%s-%02d", r.PathValue("m"), d)
			pic := ""
			if f.covered[day] {
				pic = `,"picture":"/pages/1/preview"`
			}
			days = append(days, fmt.Sprintf(`{"day":%q,"page":%v%s}`, day, f.covered[day], pic))
		}
		fmt.Fprintf(w, `{"class":["month"],"properties":{"days":[%s]}}`, strings.Join(days, ","))
	})
	mux.HandleFunc("GET /days/{d}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.covered[r.PathValue("d")] {
			fmt.Fprint(w, `{"class":["day"],"properties":{"cover":{"id":1,"title":"x.jpg"}}}`)
			return
		}
		fmt.Fprint(w, `{"class":["day"],"properties":{}}`)
	})
	mux.HandleFunc("GET /actions", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"actions":[{"name":"add-file","method":"POST","href":"/files","fields":[
{"name":"day","type":"date"},{"name":"title","type":"text"},{"name":"mime","type":"text"},
{"name":"original","type":"file"},{"name":"preview","type":"file"}]}]}`)
	})
	mux.HandleFunc("POST /files", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		_, hasPreview := r.MultipartForm.File["preview"]
		f.files = append(f.files, fmt.Sprintf("%s %s %v", r.FormValue("day"), r.FormValue("title"), hasPreview))
		if r.Header.Get("Aminte-Source") != "import:google-photos" || r.Header.Get("Accept") != "application/vnd.siren+json" {
			t.Errorf("headers: source %q, accept %q", r.Header.Get("Aminte-Source"), r.Header.Get("Accept"))
		}
		fmt.Fprint(w, `{"class":["file"],"properties":{}}`)
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fakeAminte) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.files...)
}

func threeDays() []photo {
	return []photo{
		{ID: "a", Create: "2031-05-01T10:00:00Z", Type: "PHOTO", MIME: "image/jpeg", Seed: 1},
		{ID: "b", Create: "2031-05-02T10:00:00Z", Type: "PHOTO", MIME: "image/jpeg", Seed: 2},
		{ID: "v", Create: "2031-05-02T11:00:00Z", Type: "VIDEO", MIME: "video/mp4", Seed: 3},
		{ID: "c", Create: "2031-05-03T10:00:00Z", Type: "PHOTO", MIME: "image/heic", Seed: 4},
	}
}

func TestACrossSitePostIsRefused(t *testing.T) {
	g := newFakeGoogle(t, threeDays()...)
	s := newStack(t, g, newFakeAminte(t).URL, true)
	for name, h := range map[string]map[string]string{
		"another site by Fetch Metadata": {"Sec-Fetch-Site": "cross-site"},
		"the same site, not the origin":  {"Sec-Fetch-Site": "same-site"},
		"another origin":                 {"Origin": "http://evil.example"},
	} {
		if r := s.post(t, "/pick", nil, h); r.Status != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", name, r.Status)
		}
	}
	if r := s.post(t, "/pick", nil, map[string]string{"Sec-Fetch-Site": "same-origin"}); r.Status != http.StatusSeeOther {
		t.Errorf("same-origin: status %d, want 303", r.Status)
	}
	if r := s.post(t, "/skip", nil, map[string]string{"Origin": s.web.URL}); r.Status != http.StatusSeeOther {
		t.Errorf("own Origin: status %d, want 303", r.Status)
	}
}

func TestAnotherHostNameIsRefused(t *testing.T) {
	g := newFakeGoogle(t)
	s := newStack(t, g, newFakeAminte(t).URL, true)
	req, err := http.NewRequest(http.MethodGet, s.web.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "rebind.example:80"
	if r := s.do(t, req); r.Status != http.StatusForbidden {
		t.Errorf("status %d, want 403", r.Status)
	}
}

func TestLoopbackAddrRefusesOtherAddresses(t *testing.T) {
	for _, ok := range []string{"127.0.0.1:7790", "[::1]:7790", "localhost:0"} {
		if _, err := review.LoopbackAddr(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"0.0.0.0:7790", ":7790", "192.168.1.5:7790", "example.com:80", "127.0.0.1"} {
		if _, err := review.LoopbackAddr(bad); err == nil {
			t.Errorf("%s: accepted, want a refusal", bad)
		}
	}
}

func TestCallbackWithAWrongStateIsRefused(t *testing.T) {
	g := newFakeGoogle(t)
	s := newStack(t, g, newFakeAminte(t).URL, false)
	r := s.get(t, "/connect")
	if r.Status != http.StatusSeeOther || !strings.HasPrefix(r.Location, g.URL+"/auth?") {
		t.Fatalf("GET /connect = %d %q", r.Status, r.Location)
	}
	u, err := url.Parse(r.Location)
	if err != nil {
		t.Fatal(err)
	}
	state := u.Query().Get("state")
	if state == "" || u.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("auth url lacks a state or PKCE: %s", r.Location)
	}
	if want := s.web.URL + "/oauth/callback"; u.Query().Get("redirect_uri") != want {
		t.Errorf("redirect_uri = %s, want %s", u.Query().Get("redirect_uri"), want)
	}
	if r := s.get(t, "/oauth/callback?code=c&state=wrong"); r.Status != http.StatusBadRequest {
		t.Errorf("wrong state: status %d, want 400", r.Status)
	}
	if s.auth.Connected() {
		t.Error("connected after a refused callback")
	}
	// The state is used once: the right one no longer works after a failed try.
	if r := s.get(t, "/oauth/callback?code=c&state="+state); r.Status != http.StatusBadRequest {
		t.Errorf("state after a failed try: status %d, want 400", r.Status)
	}
	// A new sign in with the right state connects.
	r = s.get(t, "/connect")
	u, _ = url.Parse(r.Location)
	if r := s.get(t, "/oauth/callback?code=c&state="+u.Query().Get("state")); r.Status != http.StatusSeeOther {
		t.Fatalf("right state: status %d, want 303", r.Status)
	}
	if !s.auth.Connected() {
		t.Error("not connected after a good callback")
	}
}

func TestSkipGoesToTheNextDayAndTheLastDayLeadsToDone(t *testing.T) {
	g := newFakeGoogle(t, threeDays()...)
	s := newStack(t, g, newFakeAminte(t).URL, true)
	s.pickAll(t)
	body := s.home(t).Body
	if !strings.Contains(body, "<h2>2031-05-01</h2>") || !strings.Contains(body, "3 days left") {
		t.Fatalf("first page: %s", body)
	}
	for _, want := range []string{"2031-05-02", "2031-05-03"} {
		if r := s.post(t, "/skip", nil, nil); r.Status != http.StatusSeeOther {
			t.Fatalf("skip: %d", r.Status)
		}
		if body = s.home(t).Body; !strings.Contains(body, "<h2>"+want+"</h2>") {
			t.Fatalf("after a skip, want %s: %s", want, body)
		}
	}
	s.post(t, "/skip", nil, nil)
	body = s.home(t).Body
	if !strings.Contains(body, "<h2>Done</h2>") || strings.Contains(body, "/choose") {
		t.Fatalf("after the last day: %s", body)
	}
	if r := s.post(t, "/done", nil, nil); r.Status != http.StatusSeeOther {
		t.Fatalf("done: %d", r.Status)
	}
	if !g.deleted {
		t.Error("the Google session was not deleted")
	}
	if !strings.Contains(s.home(t).Body, "Pick photos") {
		t.Error("after Done the page does not offer a new pick")
	}
}

func TestAVideoIsNotOfferedAsATile(t *testing.T) {
	g := newFakeGoogle(t, threeDays()...)
	s := newStack(t, g, newFakeAminte(t).URL, true)
	s.pickAll(t)
	s.post(t, "/skip", nil, nil) // to 2031-05-02, which has the photo b and the video v
	body := s.home(t).Body
	if !strings.Contains(body, `value="b"`) || strings.Contains(body, `value="v"`) {
		t.Errorf("tiles of 2031-05-02: %s", body)
	}
}

func TestChooseKeepsThePhotoWithItsDayAndGoesOn(t *testing.T) {
	g := newFakeGoogle(t, threeDays()...)
	ll := newFakeAminte(t)
	s := newStack(t, g, ll.URL, true)
	s.pickAll(t)
	if r := s.post(t, "/choose", url.Values{"item": {"a"}}, nil); r.Status != http.StatusSeeOther {
		t.Fatalf("choose: %d %s", r.Status, r.Body)
	}
	if got := ll.sent(); len(got) != 1 || got[0] != "2031-05-01 2031-05-01.jpg false" {
		t.Errorf("files sent = %q, want one JPEG with its day, and no preview", got)
	}
	if body := s.home(t).Body; !strings.Contains(body, "<h2>2031-05-02</h2>") {
		t.Errorf("after a choice: %s", body)
	}
	// A photo of another day than the one shown is refused and writes nothing.
	s.post(t, "/choose", url.Values{"item": {"c"}}, nil)
	if got := ll.sent(); len(got) != 1 {
		t.Errorf("files sent = %q, want still one", got)
	}
	// A HEIC goes with a preview from Google.
	s.post(t, "/skip", nil, nil)
	s.post(t, "/choose", url.Values{"item": {"c"}}, nil)
	if got := ll.sent(); len(got) != 2 || got[1] != "2031-05-03 2031-05-03.heic true" {
		t.Errorf("files sent = %q, want a HEIC with a preview", got)
	}
	if !g.fetched("/media/c=w1280-h1280") {
		t.Error("the scaled JPEG of the HEIC was not fetched")
	}
}

func TestTheTypeOfTheDownloadNamesTheFile(t *testing.T) {
	// The list names a HEIC, but Google sends a JPEG: the file is a JPEG, and aminte makes its picture itself.
	g := newFakeGoogle(t, photo{ID: "h", Create: "2031-05-01T10:00:00Z", Type: "PHOTO", MIME: "image/heic", Served: "image/jpeg", Seed: 1})
	ll := newFakeAminte(t)
	s := newStack(t, g, ll.URL, true)
	s.pickAll(t)
	s.post(t, "/choose", url.Values{"item": {"h"}}, nil)
	if got := ll.sent(); len(got) != 1 || got[0] != "2031-05-01 2031-05-01.jpg false" {
		t.Errorf("files sent = %q, want one JPEG and no preview", got)
	}
	if g.fetched("/media/h=w1280-h1280") {
		t.Error("a preview was fetched for a JPEG")
	}
}

func TestADayThatGotACoverMeanwhileIsNotWritten(t *testing.T) {
	g := newFakeGoogle(t, threeDays()...)
	ll := newFakeAminte(t)
	s := newStack(t, g, ll.URL, true)
	s.pickAll(t)
	ll.mu.Lock()
	ll.covered["2031-05-01"] = true // the owner sets the cover in aminte
	ll.mu.Unlock()
	s.post(t, "/choose", url.Values{"item": {"a"}}, nil)
	if got := ll.sent(); len(got) != 0 {
		t.Errorf("files sent = %q, want none", got)
	}
	if g.fetched("/media/a=d") {
		t.Error("the photo was downloaded for a day that has a cover")
	}
	if body := s.home(t).Body; !strings.Contains(body, "<h2>2031-05-02</h2>") {
		t.Errorf("the page did not go to the next day: %s", body)
	}
}

func TestDaysThatHaveACoverAreNotOffered(t *testing.T) {
	g := newFakeGoogle(t, threeDays()...)
	s := newStack(t, g, newFakeAminte(t, "2031-05-01", "2031-05-02").URL, true)
	s.pickAll(t)
	body := s.home(t).Body
	if !strings.Contains(body, "<h2>2031-05-03</h2>") || !strings.Contains(body, "1 day left") {
		t.Errorf("page: %s", body)
	}
}

func TestAThumbnailIsFetchedWithTheToken(t *testing.T) {
	g := newFakeGoogle(t, threeDays()...)
	s := newStack(t, g, newFakeAminte(t).URL, true)
	s.pickAll(t)
	r := s.get(t, "/thumb/a")
	if r.Status != http.StatusOK || len(r.Body) == 0 || !g.fetched("/media/a=w256-h256-c") {
		t.Errorf("thumb: status %d, %d bytes, fetched %v", r.Status, len(r.Body), g.fetches)
	}
	if r := s.get(t, "/thumb/nope"); r.Status != http.StatusNotFound {
		t.Errorf("unknown thumb: status %d, want 404", r.Status)
	}
}

func TestAPickThatIsNotSetYetRefreshesThePage(t *testing.T) {
	g := newFakeGoogle(t, threeDays()...)
	g.set = false
	s := newStack(t, g, newFakeAminte(t).URL, true)
	s.post(t, "/pick", nil, nil)
	r := s.get(t, "/session")
	if r.Status != http.StatusOK || !strings.Contains(r.Body, `http-equiv="refresh"`) ||
		!strings.Contains(r.Body, `href="https://photos.example/picker/s1/autoclose"`) || !strings.Contains(r.Body, `target="_blank"`) {
		t.Errorf("session page: %d %s", r.Status, r.Body)
	}
	g.mu.Lock()
	g.set = true
	g.mu.Unlock()
	if r := s.get(t, "/session"); r.Status != http.StatusSeeOther {
		t.Errorf("after the pick: %d, want 303", r.Status)
	}
}

func TestThePageSaysConnectWhenThereIsNoToken(t *testing.T) {
	g := newFakeGoogle(t)
	s := newStack(t, g, newFakeAminte(t).URL, false)
	if body := s.home(t).Body; !strings.Contains(body, `href="/connect"`) || strings.Contains(body, "/pick") {
		t.Errorf("page: %s", body)
	}
}

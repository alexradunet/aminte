package review_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"lifelog-googlephotos/lifelog"
)

// buildLifelog builds the real lifelog of this repository into a temporary folder.
func buildLifelog(t *testing.T) string {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go command to build lifelog: %v", err)
	}
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "cmd", "lifelog")); err != nil {
		t.Skipf("the lifelog sources are not at %s: %v", root, err)
	}
	bin := filepath.Join(t.TempDir(), "lifelog")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	out, err := exec.Command(goBin, "build", "-C", root, "-o", bin, "./cmd/lifelog").CombinedOutput()
	if err != nil {
		t.Fatalf("build lifelog: %v\n%s", err, out)
	}
	return bin
}

// startLifelog makes a new life.db in a temporary folder, serves it on a free loopback port, and stops it at the end.
func startLifelog(t *testing.T, bin string) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "life.db")
	if out, err := exec.Command(bin, "init", dbPath).CombinedOutput(); err != nil {
		t.Fatalf("lifelog init: %v\n%s", err, out)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd := exec.Command(bin, "serve", "--db", dbPath, "--addr", addr)
	// No variable of lifelog reaches the server: a LIFELOG_TOKEN of the developer would turn on the bearer check, and the
	// plain requests of this test send none.
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "LIFELOG_") })
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }() // read below, in the cleanup or in the wait for the start
	t.Cleanup(func() {
		select {
		case <-exited:
			return
		default:
		}
		_ = cmd.Process.Signal(syscall.SIGINT)
		select {
		case <-exited:
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
	})
	base := "http://" + addr
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-exited:
			t.Fatalf("lifelog serve ended: %v\n%s", err, stderr.String())
		default:
		}
		if resp, err := http.Get(base + "/"); err == nil {
			resp.Body.Close()
			return base
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("lifelog serve did not answer\n%s", stderr.String())
	return ""
}

// siren reads an entity of lifelog.
func sirenGet(t *testing.T, base, path string) map[string]any {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "application/vnd.siren+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %d %v\n%s", path, resp.StatusCode, err, b)
	}
	var e struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return e.Properties
}

func capture(t *testing.T, base, day, text string) {
	t.Helper()
	resp, err := http.PostForm(base+"/days/"+day+"/capture", url.Values{"text": {text}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if b, _ := io.ReadAll(resp.Body); resp.StatusCode != http.StatusOK {
		t.Fatalf("capture %s: %d\n%s", day, resp.StatusCode, b)
	}
}

// keepFile makes a file with its own title and day, as another writer would.
func keepFile(t *testing.T, base, day, title string, b []byte) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("day", day)
	_ = w.WriteField("title", title)
	p, err := w.CreateFormFile("original", title)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = p.Write(b)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, base+"/files", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Accept", "application/vnd.siren+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out, _ := io.ReadAll(resp.Body); resp.StatusCode != http.StatusOK {
		t.Fatalf("keep %s: %d\n%s", title, resp.StatusCode, out)
	}
}

func coverTitle(t *testing.T, base, day string) string {
	t.Helper()
	cover, _ := sirenGet(t, base, "/days/"+day)["cover"].(map[string]any)
	title, _ := cover["title"].(string)
	return title
}

func mediaCount(t *testing.T, base string) int {
	t.Helper()
	rows, _ := sirenGet(t, base, "/browse?kind=media")["rows"].([]any)
	return len(rows)
}

var (
	dayHeading = regexp.MustCompile(`<h2>(\d{4}-\d\d-\d\d)</h2>`)
	firstItem  = regexp.MustCompile(`name="item" value="([^"]+)"`)
)

// TestCoversFromGooglePhotosAgainstTheRealLifelog drives the review page like a browser, against a fake Google and
// the real lifelog on a new life.db. The provenance (import:google-photos) is not asserted: the API shows no source.
func TestCoversFromGooglePhotosAgainstTheRealLifelog(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs lifelog")
	}
	base := startLifelog(t, buildLifelog(t))

	const textDay, coveredDay, clashDay, heicDay = "2031-05-02", "2031-05-03", "2031-05-04", "2031-05-06"
	capture(t, base, textDay, "Words of the day that stay.")
	capture(t, base, coveredDay, "A day with its own cover.")
	ll := lifelog.New(base, "")
	ctx := context.Background()
	if title, err := ll.AddCover(ctx, coveredDay, "image/jpeg", jpegOf(t, 50), nil); err != nil || title != coveredDay+".jpg" {
		t.Fatalf("set the cover of %s: %q, %v", coveredDay, title, err)
	}
	// Another file (of another day) holds the title of the day that clashes.
	keepFile(t, base, "2031-05-09", clashDay+".jpg", jpegOf(t, 60))
	before := mediaCount(t, base)

	g := newFakeGoogle(t,
		photo{ID: "a", Create: "2031-05-01T10:00:00Z", Type: "PHOTO", MIME: "image/jpeg", Seed: 1},
		photo{ID: "b", Create: textDay + "T10:00:00Z", Type: "PHOTO", MIME: "image/jpeg", Seed: 2},
		photo{ID: "c", Create: coveredDay + "T10:00:00Z", Type: "PHOTO", MIME: "image/jpeg", Seed: 3},
		photo{ID: "d", Create: clashDay + "T10:00:00Z", Type: "PHOTO", MIME: "image/jpeg", Seed: 4},
		photo{ID: "h", Create: heicDay + "T10:00:00Z", Type: "PHOTO", MIME: "image/heic", Seed: 5},
	)
	s := newStack(t, g, base, true)
	s.pickAll(t)

	var offered []string
	for range 10 {
		body := s.home(t).Body
		m := dayHeading.FindStringSubmatch(body)
		if m == nil {
			break
		}
		offered = append(offered, m[1])
		item := firstItem.FindStringSubmatch(body)
		if item == nil {
			t.Fatalf("no tile for %s: %s", m[1], body)
		}
		if r := s.post(t, "/choose", url.Values{"item": {item[1]}}, nil); r.Status != http.StatusSeeOther {
			t.Fatalf("choose on %s: %d\n%s", m[1], r.Status, r.Body)
		}
	}
	if got, want := strings.Join(offered, " "), "2031-05-01 2031-05-02 2031-05-04 2031-05-06"; got != want {
		t.Fatalf("days offered = %s, want %s (a day with a cover is never offered)", got, want)
	}
	if body := s.home(t).Body; !strings.Contains(body, "<h2>Done</h2>") {
		t.Errorf("after the last day: %s", body)
	}

	for day, want := range map[string]string{
		"2031-05-01": "2031-05-01.jpg",    // no day page before: it is made, with the cover
		textDay:      textDay + ".jpg",    // text and no cover before
		coveredDay:   coveredDay + ".jpg", // the cover set before, not replaced
		clashDay:     clashDay + " 2.jpg", // another file held the title
		heicDay:      heicDay + ".heic",
	} {
		if got := coverTitle(t, base, day); got != want {
			t.Errorf("cover of %s = %q, want %q", day, got, want)
		}
	}
	if g.fetched("/media/c=d") {
		t.Error("the photo of a day with a cover was downloaded")
	}
	pageID := sirenGet(t, base, "/days/"+textDay)["page_id"]
	body, _ := sirenGet(t, base, fmt.Sprintf("/pages/%.0f", pageID))["body"].(string)
	if !strings.Contains(body, "Words of the day that stay.") {
		t.Errorf("the text of %s is lost: %q", textDay, body)
	}

	// The same photo sent again writes no new file.
	if got := mediaCount(t, base); got != before+4 {
		t.Fatalf("files = %d, want %d (4 new covers)", got, before+4)
	}
	if title, err := ll.AddCover(ctx, "2031-05-01", "image/jpeg", jpegOf(t, 1), nil); err != nil {
		t.Fatalf("send the photo of 2031-05-01 again: %q, %v", title, err)
	}
	if got := mediaCount(t, base); got != before+4 {
		t.Errorf("files after a second send = %d, want %d", got, before+4)
	}
}

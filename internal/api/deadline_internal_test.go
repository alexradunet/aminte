package api

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"aminte/internal/core"
	"aminte/internal/db/dbtest"
	"aminte/siren"
)

// TestARequestPastItsDeadlineAnswers504: a route runs a query that does not end (a recursive query on the store's
// reader). The context of the request ends it, and the answer is 504 with the code timeout. The deadline is short
// through the limits that the test gives; the program's limits are defaultLimits.
func TestARequestPastItsDeadlineAnswers504(t *testing.T) {
	s := &core.Store{DB: dbtest.New(t)}
	h := newServer(s, NewApps())
	m := http.NewServeMux()
	m.HandleFunc("GET /endless", h.serve(func(r *http.Request) (*siren.Entity, error) {
		var n int
		err := s.DB.R.QueryRowContext(r.Context(), "WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM c) SELECT count(*) FROM c").Scan(&n)
		return nil, err
	}))
	handler := withDeadlines(limits{request: 100 * time.Millisecond, upload: time.Hour, stall: time.Minute}, m)
	start := time.Now()
	req := httptest.NewRequest("GET", "/endless", nil)
	req.Header.Set("Accept", "application/vnd.siren+json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusGatewayTimeout || !strings.Contains(rec.Body.String(), `"code": "timeout"`) {
		t.Errorf("answer %d, want 504 with the code timeout: %.300s", rec.Code, rec.Body.String())
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("the answer took %v: the deadline did not end the query", d)
	}
}

// TestAnUploadHasALongContextAndAStallDeadline: a multipart request gets the upload context limit, not the short
// request one, so a long upload is not cut. Its body has a read deadline that moves on with each read: a sender that
// goes on is never cut, and a sender that stops is, after the stall time. Other requests have the request limit.
func TestAnUploadHasALongContextAndAStallDeadline(t *testing.T) {
	var deadline time.Time
	var readErr error
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, _ = r.Context().Deadline()
		_, readErr = io.Copy(io.Discard, r.Body)
	})
	srv := httptest.NewServer(withDeadlines(limits{request: time.Minute, upload: time.Hour, stall: 200 * time.Millisecond}, next))
	t.Cleanup(srv.Close)

	// The pauses are real time, because the stall deadline is real time: a signal cannot stand for a clock that runs
	// out. The margins are 3x each way: a pause of 60 ms is a third of the 200 ms stall time, and a stop of 600 ms is
	// three times it. A slow machine can add delay to the first only if one read waits 140 ms more than planned.
	send := func(contentType string, chunks int, pause time.Duration) {
		t.Helper()
		pr, pw := io.Pipe()
		go func() {
			for range chunks {
				time.Sleep(pause)
				if _, err := pw.Write([]byte("x")); err != nil {
					return
				}
			}
			pw.Close()
		}()
		req, err := http.NewRequestWithContext(t.Context(), "POST", srv.URL, pr)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", contentType)
		res, err := srv.Client().Do(req)
		if err == nil {
			res.Body.Close()
		}
		pr.Close()
	}

	// Progress: ten chunks, 60 ms apart (600 ms in all, three times the stall time), and no error.
	send("multipart/form-data; boundary=b", 10, 60*time.Millisecond)
	if readErr != nil {
		t.Errorf("a sender that goes on was cut: %v", readErr)
	}
	if d := time.Until(deadline); d < 50*time.Minute || d > time.Hour {
		t.Errorf("the upload context ends in %v, want about an hour", d)
	}
	// A stop: one chunk, then 600 ms with nothing. The read ends with a deadline error.
	send("multipart/form-data; boundary=b", 2, 600*time.Millisecond)
	if !errors.Is(readErr, os.ErrDeadlineExceeded) {
		t.Errorf("a sender that stopped: read error %v, want a deadline error", readErr)
	}
	// Any other request has the short context limit and no read deadline of its own.
	send("application/x-www-form-urlencoded", 1, time.Millisecond)
	if d := time.Until(deadline); d < 30*time.Second || d > time.Minute {
		t.Errorf("a plain request's context ends in %v, want about a minute", d)
	}
}

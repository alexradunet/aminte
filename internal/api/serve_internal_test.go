package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"lifelog/internal/core"
	"lifelog/siren"
)

// TestErrorEntity: each error becomes one error entity, with the status, the code and the message. An error that
// core did not type is a fault: the client gets "internal error", and the log gets the cause one time, under the
// pattern of the route. A cancelled request is not logged.
func TestErrorEntity(t *testing.T) {
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged) // no test of this package runs in parallel
	t.Cleanup(func() { log.SetOutput(prev) })
	r := httptest.NewRequest("GET", "/metrics/weight", nil)
	r.Pattern = "GET /metrics/{name}"
	const home = "index:/:Home"
	for _, tc := range []struct {
		name string
		err  error
		want string // status code message, then each link as rel:href:title
		log  bool   // the log has the cause
	}{
		{"a 409 with its code", &core.Error{Status: 409, Msg: "the version is stale", Code: "stale_version"}, "409 stale_version the version is stale " + home, false},
		{"a title in use", &core.ExistsError{ID: 7, Title: "Taken"}, "409 exists a page titled Taken already exists " + home + " existing:/pages/7:Taken", false},
		{"a 404", core.NotFound("no page %d", 9), "404 not_found no page 9 " + home, false},
		{"a 422", core.Invalid("value %q is not a number", "x"), `422 invalid value "x" is not a number ` + home, false},
		{"a 409 with no code", core.Conflict("the page is deleted"), "409 conflict the page is deleted " + home, false},
		{"a 400", &core.Error{Status: 400, Msg: "bad"}, "400 bad_request bad " + home, false},
		{"a 401", &core.Error{Status: 401, Msg: "who"}, "401 unauthorized who " + home, false},
		{"a 403", &core.Error{Status: 403, Msg: "the owner's"}, "403 forbidden the owner's " + home, false},
		{"a 413", &core.Error{Status: 413, Msg: "big"}, "413 too_large big " + home, false},
		{"a 415", &core.Error{Status: 415, Msg: "multipart"}, "415 unsupported_media multipart " + home, false},
		{"a status with no general code", &core.Error{Status: 418, Msg: "odd"}, "418 internal odd " + home, false},
		{"a wrapped refusal", fmt.Errorf("save: %w", core.Invalid("no")), "422 invalid save: no " + home, false},
		{"a timeout", fmt.Errorf("read: %w", context.DeadlineExceeded), "504 timeout read: context deadline exceeded " + home, false},
		{"a refusal that wraps a timeout", &core.Error{Status: 422, Msg: "slow", Err: context.DeadlineExceeded}, "504 timeout slow " + home, false},
		{"a fault", errors.New("disk I/O error in /private/life.db"), "500 internal internal error " + home, true},
		{"a cancelled request", fmt.Errorf("read: %w", context.Canceled), "500 internal internal error " + home, false},
	} {
		logged.Reset()
		e, status := errorEntity(r, tc.err)
		p, _ := e.Properties.(map[string]any)
		got := fmt.Sprint(p["status"], " ", p["code"], " ", p["message"])
		for _, l := range e.Links {
			got += fmt.Sprintf(" %s:%s:%s", strings.Join(l.Rel, ","), l.Href, l.Title)
		}
		if got != tc.want || status != p["status"] || e.Class[0] != "error" || e.Title != "Error" {
			t.Errorf("%s: %d %v %q %s, want %s", tc.name, status, e.Class, e.Title, got, tc.want)
		}
		if tc.log != (logged.Len() > 0) {
			t.Errorf("%s: the log is %q", tc.name, logged.String())
		}
		if tc.log && (strings.Count(logged.String(), tc.err.Error()) != 1 || !strings.Contains(logged.String(), "GET /metrics/{name}")) {
			t.Errorf("%s: the log %q does not have the cause one time under the pattern of the route", tc.name, logged.String())
		}
	}
}

// TestAnAnswerThatJSONCannotCarryIsAFault: when an entity has a value that JSON cannot carry (a NaN, an infinity), a
// program gets 500 and the error entity with the general message, not 200 and a cut body. The server's log has the
// cause one time, with the pattern of the route and not the path.
func TestAnAnswerThatJSONCannotCarryIsAFault(t *testing.T) {
	for name, e := range map[string]*siren.Entity{
		"properties that are not JSON": {Class: []string{"thing"}, Title: "Thing", Properties: map[string]any{"value": math.NaN()}},
		"a result that is not JSON":    {Class: []string{"thing"}, Title: "Thing", Properties: map[string]any{"value": 1}, Result: map[string]any{"value": math.Inf(1)}},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/things/1", nil)
			req.Pattern = "GET /things/{id}" // as the routes set it
			req.Header.Set("Accept", "application/vnd.siren+json")
			rec := httptest.NewRecorder()
			var logged bytes.Buffer
			prev := log.Writer()
			log.SetOutput(&logged) // no test of this package runs in parallel
			t.Cleanup(func() { log.SetOutput(prev) })
			answer(rec, req, http.StatusOK, e)
			if n := strings.Count(logged.String(), "cannot be encoded"); n != 1 || !strings.Contains(logged.String(), "GET /things/{id}") ||
				strings.Contains(logged.String(), "/things/1") {
				t.Errorf("the log does not have the cause one time with the route's pattern, or names the path: %q", logged.String())
			}
			var got struct {
				Class      []string
				Properties struct {
					Status        int
					Code, Message string
				}
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("the body is not JSON: %v: %.300s", err, rec.Body.String())
			}
			if rec.Code != http.StatusInternalServerError || rec.Header().Get("Content-Type") != "application/vnd.siren+json" ||
				!slices.Equal(got.Class, []string{"error"}) || got.Properties.Status != http.StatusInternalServerError ||
				got.Properties.Code != "internal" || got.Properties.Message != "internal error" {
				t.Errorf("status %d %q, want 500 and the error entity with the general message: %.300s",
					rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
			}
		})
	}
}

// TestAnswerIsSirenForEveryAccept: answer gives every client the Siren JSON of the entity with the status it was
// given, whatever the client accepts. It adds no Vary and no rules of speculation: the core draws no page.
func TestAnswerIsSirenForEveryAccept(t *testing.T) {
	e := &siren.Entity{Class: []string{"preview"}, Title: "Preview", Properties: map[string]any{"body": "words"}}
	for _, accept := range []string{"", "application/vnd.siren+json", "text/html", "text/html,application/xhtml+xml,*/*;q=0.8"} {
		t.Run(accept, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/preview", nil)
			if accept != "" {
				req.Header.Set("Accept", accept)
			}
			rec := httptest.NewRecorder()
			answer(rec, req, http.StatusAccepted, e)
			if rec.Code != http.StatusAccepted || rec.Header().Get("Content-Type") != "application/vnd.siren+json" {
				t.Errorf("%d %q, want 202 and Siren JSON", rec.Code, rec.Header().Get("Content-Type"))
			}
			if !strings.Contains(rec.Body.String(), `"title": "Preview"`) {
				t.Errorf("not the entity: %.300s", rec.Body.String())
			}
			for _, h := range []string{"Vary", "Speculation-Rules"} {
				if v := rec.Header().Values(h); len(v) != 0 {
					t.Errorf("the header %s is %v: the core has one representation", h, v)
				}
			}
		})
	}
}

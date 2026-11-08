package api_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aminte/internal/api/apitest"
)

// TestAMultipartPostWithoutFileFieldsIsRefused: a multipart body to an action that takes no file is answered 415 with
// the code unsupported_media, never read as an empty form (the fields would be lost in silence). Nothing is written.
func TestAMultipartPostWithoutFileFieldsIsRefused(t *testing.T) {
	_, h, s := apitest.Fresh(t, "cli")
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("text", "Lost words"); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/days/today/capture", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("multipart POST to capture: %d, want 415: %.200s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code": "unsupported_media"`) {
		t.Errorf("the 415 entity has not the code unsupported_media: %.300s", rec.Body.String())
	}
	if n := scalar(t, s, "SELECT CAST(count(*) AS TEXT) FROM entities WHERE body LIKE '%Lost words%'"); n != "0" {
		t.Errorf("the refused request wrote %s pages", n)
	}
}

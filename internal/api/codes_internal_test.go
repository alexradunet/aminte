package api

import (
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"aminte/siren"
)

// TestEveryErrorCodeIsInTheContractAndTheStatusTable: the code row of API.md (Errors), siren.Codes and codeOf agree.
func TestEveryErrorCodeIsInTheContractAndTheStatusTable(t *testing.T) {
	doc, err := os.ReadFile("../../API.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(doc), "\n## Errors")
	if !ok {
		t.Fatal("API.md has no Errors section")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	var row string
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "| `code` |") {
			row = line
		}
	}
	if row == "" {
		t.Fatal("the Errors section has no code row")
	}
	words := map[string]bool{}
	for _, m := range regexp.MustCompile("`([^`]+)`").FindAllStringSubmatch(row, -1) {
		words[m[1]] = true
	}
	for _, c := range siren.Codes() {
		if !words[c] {
			t.Errorf("API.md does not name the code %q", c)
		}
	}
	// The row also names three links, the property it describes and a property of a save. These are not codes.
	rels := []string{"existing", "promote", "register", "code", "version"}
	snake := regexp.MustCompile(`^[a-z_]+$`)
	for w := range words {
		if snake.MatchString(w) && !slices.Contains(siren.Codes(), w) && !slices.Contains(rels, w) {
			t.Errorf("API.md names %q: it is neither a code nor a link", w)
		}
	}
	for status, want := range map[int]string{
		http.StatusBadRequest:            siren.CodeBadRequest,
		http.StatusUnauthorized:          siren.CodeUnauthorized,
		http.StatusForbidden:             siren.CodeForbidden,
		http.StatusNotFound:              siren.CodeNotFound,
		http.StatusConflict:              siren.CodeConflict,
		http.StatusRequestEntityTooLarge: siren.CodeTooLarge,
		http.StatusUnsupportedMediaType:  siren.CodeUnsupportedMedia,
		http.StatusUnprocessableEntity:   siren.CodeInvalid,
		http.StatusServiceUnavailable:    siren.CodeBusy,
		http.StatusBadGateway:            siren.CodeUpstream,
		http.StatusGatewayTimeout:        siren.CodeTimeout,
	} {
		got := codeOf(status)
		if got != want {
			t.Errorf("codeOf(%d) = %q, want %q", status, got, want)
		}
		if !slices.Contains(siren.Codes(), got) || got == siren.CodeInternal {
			t.Errorf("codeOf(%d) = %q: not a general code", status, got)
		}
	}
}

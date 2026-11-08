package api_test

import (
	"strings"
	"testing"

	"aminte/internal/api/apitest"
)

// TestANumberThatIsNotDecimalIsRefusedAndWritesNothing: a reading takes a decimal number only, and a mood one of its options. Go's float
// syntax also reads 1_000 and 0x1p-2; the API refuses them with 422 before the core runs, and the file does not
// change.
func TestANumberThatIsNotDecimalIsRefusedAndWritesNothing(t *testing.T) {
	c, _, s := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	count := func(table string) string { return scalar(t, s, "SELECT count(*) FROM "+table) }
	refused := func(err error, want string) {
		t.Helper()
		if apitest.Status(err) != 422 || !strings.Contains(err.Error(), want) {
			t.Fatalf("error=%v want 422 %q", err, want)
		}
	}

	record := apitest.Action(t, root, "record")
	before := count("measurements")
	for _, v := range []string{"NaN", "Inf", "1_000", "0x1p-2"} {
		_, err := c.Do(t.Context(), record, map[string]string{"metric": "weight", "value": v})
		refused(err, "is not a number")
		if got := count("measurements"); got != before {
			t.Fatalf("value %q: measurements %s, want %s", v, got, before)
		}
	}
	apitest.Must(t)(c.Do(t.Context(), record, map[string]string{"metric": "weight", "value": "70"}))
	if got := scalar(t, s, "SELECT count(*) FROM measurements WHERE value = 70"); got != "1" {
		t.Fatalf("readings of 70: %s, want 1", got)
	}

	day := apitest.Must(t)(c.Get(t.Context(), "/days/2026-09-01"))
	capture := apitest.Action(t, day, "capture")
	entities, readings := count("entities"), count("measurements")
	for _, mood := range []string{"NaN", "Inf", "0x1p1"} {
		_, err := c.Do(t.Context(), capture, map[string]string{"text": "entry 1", "mood": mood})
		refused(err, "mood must be one of") // the options of the field refuse it before the parser of numbers
		if e, m := count("entities"), count("measurements"); e != entities || m != readings {
			t.Fatalf("mood %q: entities %s readings %s, want %s and %s", mood, e, m, entities, readings)
		}
		if got := scalar(t, s, "SELECT count(*) FROM entities WHERE body LIKE '%entry 1%'"); got != "0" {
			t.Fatalf("mood %q: an entry was written", mood)
		}
	}
	apitest.Must(t)(c.Do(t.Context(), capture, map[string]string{"text": "entry 3", "mood": "3"}))
	if e, m := count("entities"), count("measurements"); e == entities || m == readings {
		t.Fatalf("mood 3: entities %s readings %s, want more than %s and %s", e, m, entities, readings)
	}
	if got := scalar(t, s, "SELECT count(*) FROM entities WHERE body LIKE '%entry 3%'"); got != "1" {
		t.Fatalf("pages with entry 3: %s, want 1", got)
	}
}

// TestACaptureRefusedAtItsMoodWritesNoEntry: the core appends the entry of a capture before it records the mood, in one
// transaction. A mood that the file refuses rolls the entry back too, so a refused capture makes no day page and no
// entry. The one refusal that reaches the core through the API with a number the parser accepts is a capture whose
// import_key was used for a Mood reading before (409, D28).
func TestACaptureRefusedAtItsMoodWritesNoEntry(t *testing.T) {
	c, _, s := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "record"), map[string]string{"metric": "Mood", "day": "2026-09-01", "value": "2", "import_key": "shared"}))
	count := func(table string) string { return scalar(t, s, "SELECT count(*) FROM "+table) }
	entities, readings := count("entities"), count("measurements")

	day := apitest.Must(t)(c.Get(t.Context(), "/days/2026-09-02"))
	_, err := c.Do(t.Context(), apitest.Action(t, day, "capture"), map[string]string{"text": "entry shared", "mood": "3", "import_key": "shared"})
	if apitest.Status(err) != 409 {
		t.Fatalf("a capture with the key of a reading: %v, want 409", err)
	}
	if e, m := count("entities"), count("measurements"); e != entities || m != readings {
		t.Fatalf("entities %s readings %s, want %s and %s", e, m, entities, readings)
	}
	for _, q := range []string{"SELECT count(*) FROM entities WHERE body LIKE '%entry shared%'", "SELECT count(*) FROM entity_names WHERE name_key = '2026-09-02'", "SELECT count(*) FROM capture_keys"} {
		if got := scalar(t, s, q); got != "0" {
			t.Fatalf("%s: %s, want 0", q, got)
		}
	}
}

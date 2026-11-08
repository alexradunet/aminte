// Package scaletest builds synthetic, reproducible workloads through the writer.
// It is development tooling, not an alternate importer or a performance promise.
package scaletest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"aminte/internal/core"
	"aminte/internal/db"
)

const Version = 1

var anchor = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
var metricNames = []string{"Scale Amount", "Scale Score", "Scale Dose", "Scale Habit"}

type Options struct {
	Profile   string
	Seed      int64
	Previews  bool
	Samples   int
	Directory string // parent of a newly created scratch directory
	Keep      bool
	Storage   string // operator-supplied hardware/filesystem description
	Progress  func(string)
}

type Profile struct {
	Name               string `json:"name"`
	Days               int    `json:"days"`
	MeasurementsPerDay int    `json:"measurements_per_day"`
	FilesPerDay        int    `json:"files_per_day"`
	Notes              int    `json:"notes"`
	TextMultiplier     int    `json:"text_multiplier"`
}

func workload(name string) (Profile, error) {
	days := int(anchor.AddDate(50, 0, 0).Sub(anchor).Hours() / 24)
	switch name {
	case "small":
		return Profile{name, 14, 4, 1, 8, 1}, nil
	case "lifetime":
		return Profile{name, days, 20, 3, 4000, 1}, nil
	case "stress":
		return Profile{name, days, 80, 6, 16000, 4}, nil
	default:
		return Profile{}, fmt.Errorf("profile must be small, lifetime or stress")
	}
}

type Manifest struct {
	Version          int     `json:"generator_version"`
	Seed             int64   `json:"seed"`
	Profile          Profile `json:"profile"`
	Anchor           string  `json:"anchor"`
	MediaMode        string  `json:"media_mode"`
	LogicalSHA256    string  `json:"logical_sha256"`
	Roots            int     `json:"root_readings"`
	Corrections      int     `json:"value_corrections"`
	Retractions      int     `json:"retractions"`
	CurrentReadings  int     `json:"current_readings"`
	JournalPages     int     `json:"journal_captures"`
	Files            int     `json:"files"`
	PreviewBytes     int64   `json:"preview_bytes"`
	PopularBacklinks int     `json:"popular_backlinks"`
	RareMatches      int     `json:"rare_search_matches"`
}

type Sample struct {
	Nanoseconds  int64  `json:"nanoseconds"`
	GoAllocBytes uint64 `json:"go_alloc_bytes"`
	GoAllocs     uint64 `json:"go_allocations"`
}
type Operation struct {
	Name    string   `json:"name"`
	Samples []Sample `json:"samples"`
}

// Result is what Run measured. Pragmas, SQLite and StorageBytes are read here because only Run sees the open file and
// its directory before the cleanup; the runner (tools/lifescale) adds the environment and the notes.
type Result struct {
	Manifest     Manifest          `json:"manifest"`
	Pragmas      map[string]string `json:"writer_pragmas"`
	SQLite       string            `json:"sqlite_version"`
	Build        Sample            `json:"fixture_build"`
	Operations   []Operation       `json:"operations"`
	StorageBytes map[string]int64  `json:"storage_bytes"`
	Directory    string            `json:"directory"`
	Retained     bool              `json:"retained"`
}

type expected struct {
	id    int64
	day   string
	value float64
}
type expectedPage struct {
	typ, day, bodyHash, fileHash, mime, previewHash string
	previewBytes                                    int
}
type probePage struct {
	id        int64
	day, body string
}
type fixture struct {
	manifest  Manifest
	series    [4][]expected
	replay    []core.Reading
	popular   int64
	pages     map[string]expectedPage
	backlinks map[string]bool
	rare      map[string]bool
	probes    map[string]probePage
}

// Run creates a new throwaway directory, validates known answers, then measures
// operations on the warm writer-built file. It never accepts an existing database.
func Run(ctx context.Context, options Options) (report *Result, err error) {
	p, err := workload(options.Profile)
	if err != nil {
		return nil, err
	}
	if options.Samples < 1 || options.Samples > 20 {
		return nil, fmt.Errorf("samples must be between 1 and 20")
	}
	dir, err := os.MkdirTemp(options.Directory, "aminte-scale-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if !options.Keep {
			err = errors.Join(err, os.RemoveAll(dir))
		}
	}()
	path := filepath.Join(dir, "life.db")
	if err = db.Init(ctx, path); err != nil {
		return nil, err
	}
	d, err := db.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, d.Close()) }()
	s := &core.Store{DB: d}
	report = &Result{Directory: dir, Retained: options.Keep, Pragmas: map[string]string{}, StorageBytes: map[string]int64{}}
	for _, pragma := range []string{"journal_mode", "synchronous", "foreign_keys", "recursive_triggers", "trusted_schema", "busy_timeout", "wal_autocheckpoint", "page_size"} {
		var value string
		if err := d.W.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&value); err != nil {
			return nil, err
		}
		report.Pragmas[pragma] = value
	}
	if err := d.R.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&report.SQLite); err != nil {
		return nil, err
	}
	var f *fixture
	mode := "metadata only"
	if options.Previews {
		mode = "unique JPEG previews"
	}
	progress(options, "Building %s seed=%d in %s (%s)", p.Name, options.Seed, dir, mode)
	report.Build, err = measure(func() error { var e error; f, e = build(ctx, s, p, options); return e })
	if err != nil {
		return nil, fmt.Errorf("build profile=%s seed=%d: %w", p.Name, options.Seed, err)
	}
	report.Manifest = f.manifest
	progress(options, "Checking all metric leaves, search, backlinks and integrity")
	// A malformed batch must roll back its valid prefix. The independent row
	// and leaf checks below detect leakage without timing this control.
	bad := s.Do(ctx, "import:scale", func(tx *core.Tx) error {
		if _, e := tx.Record(core.Reading{Metric: metricNames[0], Day: "1970-01-01", Value: 1, ImportKey: "refused-prefix"}); e != nil {
			return e
		}
		_, e := tx.Record(core.Reading{Metric: metricNames[0], Day: "1970-02-30", Value: 1, ImportKey: "refused-invalid"})
		return e
	})
	var refusal *core.Error
	if !errors.As(bad, &refusal) || refusal.Status != 422 {
		return nil, fmt.Errorf("malformed-batch refusal: %v", bad)
	}
	if err := verify(ctx, s, f); err != nil {
		return nil, fmt.Errorf("fixture profile=%s seed=%d: %w", p.Name, options.Seed, err)
	}
	progress(options, "Measuring warm workflows (%d samples each)", options.Samples)
	if err := exercise(ctx, s, f, options.Samples, report); err != nil {
		return nil, err
	}
	snapshot, restored := filepath.Join(dir, "snapshot.db"), filepath.Join(dir, "restored.db")
	progress(options, "Copying snapshot and restoring into a separate file")
	if err := record(report, "snapshot_copy", func() error { return db.Copy(ctx, path, snapshot) }); err != nil {
		return nil, err
	}
	var reopened *db.DB
	if err := record(report, "restore_copy_open", func() error {
		if err := db.Copy(ctx, snapshot, restored); err != nil {
			return err
		}
		var e error
		reopened, e = db.Open(ctx, restored)
		return e
	}); err != nil {
		return nil, err
	}
	err = verify(ctx, &core.Store{DB: reopened}, f)
	err = errors.Join(err, reopened.Close())
	if err != nil {
		return nil, fmt.Errorf("restored fixture: %w", err)
	}
	for _, name := range []string{"life.db", "life.db-wal", "life.db-shm", "snapshot.db", "restored.db", "restored.db-wal", "restored.db-shm"} {
		st, e := os.Stat(filepath.Join(dir, name))
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return nil, e
		}
		if e == nil {
			report.StorageBytes[name] = st.Size()
		} else {
			report.StorageBytes[name] = 0
		}
	}
	return report, nil
}

func progress(o Options, format string, args ...any) {
	if o.Progress != nil {
		o.Progress(fmt.Sprintf(format, args...))
	}
}

// build writes the fixture through the production writer, in this order: each day journals, then readings, corrections
// and retractions, then files; notes follow in 50-note transactions. Every seventh day skips a journal capture, but a
// preview embed may still create its day page. Readings shift into adjacent bursts and every fifth eligible reading is
// backdated three days. The payload targets are about 256 to 1280 bytes for a journal, 128, 1024 or 8192 for file
// text, and 256, 1024, 4096, 16384 or 65536 for a note, all multiplied by the text_multiplier of the profile. The
// logical digest covers the ordered generated payloads and leaves out the write-clock timestamps. The build time
// includes payload generation, previews when selected, commits, corrections and the bookkeeping of the oracle.
func build(ctx context.Context, s *core.Store, p Profile, o Options) (*fixture, error) {
	f := &fixture{manifest: Manifest{Version: Version, Seed: o.Seed, Profile: p, Anchor: anchor.Format(time.DateOnly), MediaMode: "metadata-only; no preview/storage realism claim"}, pages: map[string]expectedPage{}, backlinks: map[string]bool{}, rare: map[string]bool{}}
	if o.Previews {
		f.manifest.MediaMode = "unique synthetic JPEG previews (640x480, 1024x768, 1600x900; quality 75)"
	}
	rng := rand.New(rand.NewSource(o.Seed))
	digest := sha256.New()
	err := s.Do(ctx, "import:scale", func(tx *core.Tx) error {
		var e error
		f.popular, _, e = tx.CreatePage("Scale Popular", "Synthetic shared topic", "1970-01-01")
		if e != nil {
			return e
		}
		if _, e = tx.CreatePerson("Scale Person", "Synthetic person", "", ""); e != nil {
			return e
		}
		if _, _, e = tx.CreatePage("Scale Place", "", "1970-01-01"); e != nil {
			return e
		}
		for i, name := range metricNames {
			if _, e = tx.RegisterMetric(core.NewMetric{Name: name, Unit: []string{"kg", "", "mg", ""}[i], Note: "Synthetic metric"}); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	f.pages["Scale Popular"] = expectedPage{typ: "page", day: "1970-01-01", bodyHash: hashText("Synthetic shared topic")}
	f.pages["Scale Person"] = expectedPage{typ: "person", bodyHash: hashText("")}
	f.pages["Scale Place"] = expectedPage{typ: "page", day: "1970-01-01", bodyHash: hashText("")}
	for _, name := range metricNames {
		f.pages[name] = expectedPage{typ: "metric", bodyHash: hashText("Synthetic metric")}
	}
	for day := 0; day < p.Days; day++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		date := anchor.AddDate(0, 0, day).Format(time.DateOnly)
		files := make([]core.FileIn, p.FilesPerDay)
		for j := range files {
			n := day*p.FilesPerDay + j
			body := payload(fmt.Sprintf("Synthetic file %d. ", n), []int{128, 1024, 8192}[n%3]*p.TextMultiplier)
			original := []byte(body)
			mime := "text/plain"
			var preview []byte
			if o.Previews {
				preview, err = syntheticJPEG(o.Seed, n)
				if err != nil {
					return nil, err
				}
				original, mime = preview, "image/jpeg"
			}
			files[j] = core.FileIn{Title: fmt.Sprintf("Scale File %06d", n), SHA256: fmt.Sprintf("%x", sha256.Sum256(original)), MIME: mime, Body: body, Day: date, Preview: preview}
			fmt.Fprintf(digest, "file:%d:%s:%s\n", n, date, files[j].SHA256)
			f.manifest.PreviewBytes += int64(len(preview))
		}
		err = s.Do(ctx, "import:scale", func(tx *core.Tx) error {
			journalBody := ""
			if day%7 != 6 {
				body := payload("Synthetic journal [[Scale Popular]] [[Scale Person]] [[Scale Place]]. ", (256+day%5*256)*p.TextMultiplier)
				if day%11 == 0 {
					body += " scaleraretoken"
					f.manifest.RareMatches++
					f.rare[date] = true
				}
				if _, _, _, e := tx.CaptureWithKey("", date, body, nil); e != nil {
					return e
				}
				fmt.Fprintf(digest, "journal:%s:%s\n", date, body)
				f.manifest.JournalPages++
				f.manifest.PopularBacklinks++
				journalBody = body
				f.backlinks[date] = true
			}
			for j := 0; j < p.MeasurementsPerDay; j++ {
				n := day*p.MeasurementsPerDay + j
				metric := j % len(metricNames)
				when := day
				if day%7 == 6 {
					when--
				} // missing days and adjacent bursts
				if day >= 3 && n%5 == 0 {
					when = day - 3
				} // later ingestion of older facts
				value := float64(rng.Intn(400)) / 4
				if metric == 3 {
					value = float64(n % 2)
				}
				in := core.Reading{Metric: metricNames[metric], Day: anchor.AddDate(0, 0, when).Format(time.DateOnly), Value: value, ImportKey: fmt.Sprintf("reading:%d", n)}
				id, e := tx.Record(in)
				if e != nil {
					return fmt.Errorf("reading %d: %w", n, e)
				}
				if len(f.replay) < 128 {
					f.replay = append(f.replay, in)
				}
				fmt.Fprintf(digest, "reading:%d:%s:%s:%g\n", n, in.Metric, in.Day, value)
				f.manifest.Roots++
				if n%17 == 0 {
					value += .25
					if metric == 3 {
						value = 1 - in.Value
					}
					id, e = tx.Correct(id, &value)
					if e != nil {
						return e
					}
					f.manifest.Corrections++
				}
				if n%31 == 0 {
					if _, e = tx.Correct(id, nil); e != nil {
						return e
					}
					f.manifest.Retractions++
				} else {
					f.series[metric] = append(f.series[metric], expected{id, in.Day, value})
					f.manifest.CurrentReadings++
				}
			}
			for j, file := range files {
				if _, e := tx.AddFile(file); e != nil {
					return fmt.Errorf("file %d: %w", day*p.FilesPerDay+j, e)
				}
				f.manifest.Files++
				previewHash := ""
				if file.Preview != nil {
					previewHash = fmt.Sprintf("%x", sha256.Sum256(file.Preview))
					if journalBody != "" {
						journalBody += "\n\n"
					}
					journalBody += "![[" + file.Title + "]]"
				}
				f.pages[file.Title] = expectedPage{typ: "file", day: date, bodyHash: hashText(file.Body), fileHash: file.SHA256, mime: file.MIME, previewHash: previewHash, previewBytes: len(file.Preview)}
			}
			if journalBody != "" {
				f.pages[date] = expectedPage{typ: "page", day: date, bodyHash: hashText(journalBody)}
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("day %d (%s): %w", day, date, err)
		}
		if (day+1)%365 == 0 || day+1 == p.Days {
			progress(o, "Built %d/%d days: %d reading roots, %d files", day+1, p.Days, f.manifest.Roots, f.manifest.Files)
		}
	}
	// Notes arrive in bounded bursts after the dated rows, carrying backdated attribution.
	for start := 0; start < p.Notes; start += 50 {
		err := s.Do(ctx, "import:scale", func(tx *core.Tx) error {
			for n := start; n < min(start+50, p.Notes); n++ {
				body := payload(fmt.Sprintf("Synthetic note %d café Ελληνικά [[Scale Popular]]. ", n), []int{256, 1024, 4096, 16384, 65536}[n%5]*p.TextMultiplier)
				date := anchor.AddDate(0, 0, n%p.Days).Format(time.DateOnly)
				if _, _, e := tx.CreatePage(fmt.Sprintf("Scale Note %06d", n), body, date); e != nil {
					return e
				}
				fmt.Fprintf(digest, "note:%d:%s:%s\n", n, date, body)
				f.manifest.PopularBacklinks++
				title := fmt.Sprintf("Scale Note %06d", n)
				f.pages[title] = expectedPage{typ: "page", day: date, bodyHash: hashText(body)}
				f.backlinks[title] = true
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("note batch %d: %w", start, err)
		}
		if (start+50)%500 == 0 || start+50 >= p.Notes {
			progress(o, "Built %d/%d imported notes", min(start+50, p.Notes), p.Notes)
		}
	}
	for i := range f.series {
		sort.Slice(f.series[i], func(a, b int) bool {
			x, y := f.series[i][a], f.series[i][b]
			if x.day == y.day {
				return x.id < y.id
			}
			return x.day < y.day
		})
	}
	f.manifest.LogicalSHA256 = fmt.Sprintf("%x", digest.Sum(nil))
	return f, nil
}

func hashText(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }

func payload(prefix string, size int) string {
	return prefix + strings.Repeat("synthetic text ", max(0, (size-len(prefix))/15))
}

func syntheticJPEG(seed int64, index int) ([]byte, error) {
	size := [][2]int{{640, 480}, {1024, 768}, {1600, 900}}[index%3]
	m := image.NewRGBA(image.Rect(0, 0, size[0], size[1]))
	rng := rand.New(rand.NewSource(seed + int64(index)*7919))
	for i := 0; i < len(m.Pix); i += 4 {
		m.Pix[i], m.Pix[i+1], m.Pix[i+2], m.Pix[i+3] = byte(rng.Intn(256)), byte(rng.Intn(256)), byte(rng.Intn(256)), 255
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, m, &jpeg.Options{Quality: 75}); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func verify(ctx context.Context, s *core.Store, f *fixture) error {
	for title, want := range f.probes {
		got, err := s.PageByID(ctx, want.id)
		if err != nil {
			return err
		}
		if got.Title != title || got.Day != want.day || got.Body != want.body {
			return fmt.Errorf("probe %q did not survive restore: %+v", title, got)
		}
	}
	if err := verifyPages(ctx, s, f); err != nil {
		return err
	}
	for i, name := range metricNames {
		got, err := s.Series(ctx, name, "", "9999-12-31")
		if err != nil {
			return err
		}
		if err := checkSeries(got, f.series[i]); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	var raw, files int
	var previews int64
	if err := s.DB.R.QueryRowContext(ctx, "SELECT count(*) FROM measurements").Scan(&raw); err != nil {
		return err
	}
	if err := s.DB.R.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(length(preview)),0) FROM files").Scan(&files, &previews); err != nil {
		return err
	}
	if raw != f.manifest.Roots+f.manifest.Corrections+f.manifest.Retractions || files != f.manifest.Files || previews != f.manifest.PreviewBytes {
		return fmt.Errorf("stored totals readings/files/preview bytes=%d/%d/%d", raw, files, previews)
	}
	hits, err := s.SearchWith(ctx, "scaleraretoken", core.SearchOptions{}, f.manifest.Profile.Days, 0)
	if err != nil {
		return err
	}
	if len(hits) != f.manifest.RareMatches {
		return fmt.Errorf("rare search got %d want %d", len(hits), f.manifest.RareMatches)
	}
	seenHits := map[string]bool{}
	for _, hit := range hits {
		if !f.rare[hit.Title] {
			return fmt.Errorf("unexpected rare hit %q", hit.Title)
		}
		if seenHits[hit.Title] {
			return fmt.Errorf("duplicate rare hit %q", hit.Title)
		}
		seenHits[hit.Title] = true
	}
	p, err := s.PageByID(ctx, f.popular)
	if err != nil {
		return err
	}
	if len(p.In) != f.manifest.PopularBacklinks {
		return fmt.Errorf("popular backlinks got %d want %d", len(p.In), f.manifest.PopularBacklinks)
	}
	seenBacklinks := map[string]bool{}
	for _, edge := range p.In {
		if edge.Kind != "wikilink" || !f.backlinks[edge.Title] {
			return fmt.Errorf("unexpected popular backlink %+v", edge)
		}
		if seenBacklinks[edge.Title] {
			return fmt.Errorf("duplicate popular backlink %q", edge.Title)
		}
		seenBacklinks[edge.Title] = true
	}
	ic, err := s.Integrity(ctx)
	if err != nil {
		return err
	}
	if !ic.OK {
		return fmt.Errorf("integrity failed: %+v", ic)
	}
	return nil
}

func verifyPages(ctx context.Context, s *core.Store, f *fixture) error {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT n.title,e.entity_type,coalesce(e.day,''),e.body,e.deleted_at IS NULL,coalesce(f.sha256,''),coalesce(f.mime,''),f.preview FROM entities e JOIN entity_names n ON n.entity_id=e.id AND n.name_key=e.preferred_name_key LEFT JOIN files f ON f.id=e.id WHERE e.source='import:scale'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var title, typ, day, body, hash, mime string
		var live bool
		var preview []byte
		if err := rows.Scan(&title, &typ, &day, &body, &live, &hash, &mime, &preview); err != nil {
			return err
		}
		want, ok := f.pages[title]
		previewHash := ""
		if preview != nil {
			previewHash = fmt.Sprintf("%x", sha256.Sum256(preview))
		}
		got := expectedPage{typ, day, hashText(body), hash, mime, previewHash, len(preview)}
		if !ok || !live || got != want {
			return fmt.Errorf("page %q content mismatch: got %+v want %+v live=%v", title, got, want, live)
		}
		seen++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if seen != len(f.pages) {
		return fmt.Errorf("generated pages %d want %d", seen, len(f.pages))
	}
	return nil
}

func checkSeries(got []core.Reading, want []expected) error {
	if len(got) != len(want) {
		return fmt.Errorf("series length %d want %d", len(got), len(want))
	}
	for i, r := range got {
		w := want[i]
		if r.ID != w.id || r.Day != w.day || r.Value != w.value {
			return fmt.Errorf("series row %d got %+v want %+v", i, r, w)
		}
	}
	return nil
}

// timedSource is the writer of the timed checks: an import writer like the fixture's, not the owner's, and its own,
// so the oracle that verifies the fixture (source import:scale) does not see the probe rows.
const timedSource = "import:scale-timed"

func exercise(ctx context.Context, s *core.Store, f *fixture, samples int, report *Result) error {
	f.probes = map[string]probePage{}
	probeDay := anchor.AddDate(0, 0, f.manifest.Profile.Days+1).Format(time.DateOnly)
	var editID int64
	if err := s.Do(ctx, timedSource, func(tx *core.Tx) error {
		var e error
		editID, _, e = tx.CreatePage("Scale Edit Probe", "initial", probeDay)
		return e
	}); err != nil {
		return err
	}
	for i := 0; i < samples; i++ {
		day := anchor.AddDate(0, 0, f.manifest.Profile.Days+1+i).Format(time.DateOnly)
		var captureID int64
		if err := record(report, "capture_commit", func() error {
			var e error
			captureID, _, _, e = s.CaptureWithKey(ctx, timedSource, "", day, "Synthetic timed capture", nil)
			return e
		}); err != nil {
			return err
		}
		captured, err := s.PageByID(ctx, captureID)
		if err != nil {
			return err
		}
		if captured.Title != day || captured.Day != day || captured.Body != "Synthetic timed capture" {
			return fmt.Errorf("timed capture was not persisted: %+v", captured)
		}
		f.probes[day] = probePage{captureID, day, "Synthetic timed capture"}
		page, err := s.PageByID(ctx, editID)
		if err != nil {
			return err
		}
		body := fmt.Sprintf("Synthetic edit %d", i)
		if err := record(report, "save_commit", func() error { _, e := s.SaveBody(ctx, timedSource, editID, body, page.Version); return e }); err != nil {
			return err
		}
		page, err = s.PageByID(ctx, editID)
		if err != nil || page.Body != body {
			return fmt.Errorf("saved body: %v", err)
		}
		f.probes["Scale Edit Probe"] = probePage{editID, probeDay, body}
		date := anchor.AddDate(0, 0, f.manifest.Profile.Days/2).Format(time.DateOnly)
		var d *core.Day
		if err := record(report, "day_view", func() error { var e error; d, e = s.Day(ctx, date); return e }); err != nil {
			return err
		}
		journal, hasJournal := f.pages[date]
		if d.Day != date || (d.PageID != 0) != hasJournal {
			return fmt.Errorf("day view identity: day %q page %d, want day %q journal=%v", d.Day, d.PageID, date, hasJournal)
		}
		journalRows := 0
		for _, row := range d.Rows {
			if row.What == "day page" {
				journalRows++
				if !hasJournal || hashText(row.Detail) != journal.bodyHash {
					return fmt.Errorf("day view %s lost or changed journal text", date)
				}
			}
		}
		if hasJournal {
			page, err := s.PageByID(ctx, d.PageID)
			if err != nil {
				return err
			}
			if journalRows != 1 || page.Title != date || page.Day != date || hashText(page.Body) != journal.bodyHash {
				return fmt.Errorf("day view %s returned the wrong journal identity or row count", date)
			}
		} else if journalRows != 0 {
			return fmt.Errorf("day view %s invented a journal", date)
		}
		want := map[int64]expected{}
		for _, series := range f.series {
			for _, r := range series {
				if r.day == date {
					want[r.id] = r
				}
			}
		}
		if len(d.Readings) != len(want) {
			return fmt.Errorf("day reading count %d want %d", len(d.Readings), len(want))
		}
		for _, r := range d.Readings {
			w, ok := want[r.ID]
			if !ok || r.Value != w.value {
				return fmt.Errorf("wrong day reading %+v", r)
			}
			delete(want, r.ID) // A duplicate cannot stand in for a missing reading.
		}
		// A page of the stream from the middle of the record (docs/plans/130-the-explore-pass.md): its days are
		// newest first, none after today.
		var cards []core.Card
		middle := f.manifest.Profile.Days / 2
		if err := record(report, "stream_page", func() error { var e error; cards, _, e = s.Stream(ctx, 30, middle); return e }); err != nil {
			return err
		}
		if len(cards) == 0 && hasJournal {
			return fmt.Errorf("the stream at offset %d is empty", middle)
		}
		for j, c := range cards {
			if c.Day > s.Today() || j > 0 && c.Day >= cards[j-1].Day {
				return fmt.Errorf("the stream at offset %d is out of order at %s", middle, c.Day)
			}
		}
		var hits []core.Hit
		if err := record(report, "search_rare", func() error {
			var e error
			hits, e = s.SearchWith(ctx, "scaleraretoken", core.SearchOptions{}, f.manifest.Profile.Days, 0)
			return e
		}); err != nil {
			return err
		}
		if len(hits) != f.manifest.RareMatches {
			return fmt.Errorf("rare search changed")
		}
		if err := record(report, "page_with_popular_backlinks", func() error { var e error; page, e = s.PageByID(ctx, f.popular); return e }); err != nil {
			return err
		}
		if len(page.In) != f.manifest.PopularBacklinks {
			return fmt.Errorf("backlinks changed")
		}
		var series []core.Reading
		if err := record(report, "historical_metric_series", func() error {
			var e error
			series, e = s.Series(ctx, metricNames[0], "", "9999-12-31")
			return e
		}); err != nil {
			return err
		}
		if err := checkSeries(series, f.series[0]); err != nil {
			return err
		}
		if err := record(report, "keyed_core_replay_up_to_128_roots", func() error {
			return s.Do(ctx, "import:scale", func(tx *core.Tx) error {
				for _, in := range f.replay {
					id, e := tx.Record(in)
					if e != nil {
						return e
					}
					if id != 0 {
						return fmt.Errorf("keyed replay inserted %d", id)
					}
				}
				return nil
			})
		}); err != nil {
			return err
		}
	}
	return nil
}

func measure(fn func() error) (Sample, error) {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	err := fn()
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	return Sample{elapsed.Nanoseconds(), after.TotalAlloc - before.TotalAlloc, after.Mallocs - before.Mallocs}, err
}
func record(report *Result, name string, fn func() error) error {
	sample, err := measure(fn)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	for i := range report.Operations {
		if report.Operations[i].Name == name {
			report.Operations[i].Samples = append(report.Operations[i].Samples, sample)
			return nil
		}
	}
	report.Operations = append(report.Operations, Operation{name, []Sample{sample}})
	return nil
}

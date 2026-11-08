package tests

// The shared kit of the suites: the docs tree under test (with an overlay of broken files for a mutant), the
// expectation counter, fresh databases built from the DDL in the docs, and the insert conventions of the docs
// (entity first, ids by RETURNING, a named entity is one id with a page).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"aminte/internal/core"
	"aminte/internal/db"
	"aminte/internal/photo/phototest"
	"aminte/text"

	_ "modernc.org/sqlite"
)

// NOW is the instant format of aminte_meta.instants.
const NOW = "strftime('%Y-%m-%dT%H:%M:%fZ','now')"

var (
	repoRoot = ".." // go test runs in this package's directory
	docsRoot = filepath.Join("..", "docs")
)

// The current-truth folders of the docs, in reading order, and the dated records that are not current truth.
var (
	currentTops = []string{"README.md", "process.md", "architecture", "schema", "contract", "decisions", "cookbook", "guides", "research"}
	recordTops  = []string{"issues", "rfcs", "plans"}
)

// ---- the docs tree

var (
	diskMu sync.Mutex
	disk   = map[string]*string{}
)

// readFile is a file of the repository with its line endings normalised, read once per test binary; ok is false
// when it does not exist.
func readFile(p string) (string, bool) {
	diskMu.Lock()
	defer diskMu.Unlock()
	if t, seen := disk[p]; seen {
		return deref(t), t != nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		disk[p] = nil
		return "", false
	}
	t := strings.ReplaceAll(string(b), "\r\n", "\n")
	disk[p] = &t
	return t, true
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// Docs is the docs tree under test: the files on disk under root, and for a mutant an overlay of broken files.
type Docs struct {
	root string
	over map[string]string
}

func realDocs() *Docs { return &Docs{root: docsRoot} }

// with is the same tree with some files replaced.
func (d *Docs) with(over map[string]string) *Docs { return &Docs{root: d.root, over: over} }

// Page is one file of the tree, by its path relative to docs/ ("" if it does not exist).
func (d *Docs) Page(rel string) string {
	if t, ok := d.over[rel]; ok {
		return t
	}
	t, _ := readFile(filepath.Join(d.root, filepath.FromSlash(rel)))
	return t
}

// exists reports a file or folder of the tree.
func (d *Docs) exists(rel string) bool {
	if _, ok := d.over[rel]; ok {
		return true
	}
	_, err := os.Stat(filepath.Join(d.root, filepath.FromSlash(rel)))
	return err == nil
}

var cookbookIndexLink = regexp.MustCompile(`\]\(([a-z0-9-]+)\.md\)`)

// CookbookOrder is the recipe keys in the order the cookbook index lists them.
func (d *Docs) CookbookOrder() []string {
	var out []string
	for _, m := range cookbookIndexLink.FindAllStringSubmatch(d.Page("cookbook/README.md"), -1) {
		out = append(out, m[1])
	}
	return out
}

// Pages is every current-truth markdown page, in reading order (cookbook recipes in index order).
func (d *Docs) Pages() []string {
	var out []string
	for _, top := range currentTops {
		p := filepath.Join(d.root, top)
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !st.IsDir() {
			out = append(out, top)
			continue
		}
		ents, _ := os.ReadDir(p)
		var names []string
		for _, e := range ents {
			if strings.HasSuffix(e.Name(), ".md") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		if top == "cookbook" {
			order := d.CookbookOrder()
			listed := map[string]bool{}
			re := []string{"README.md"}
			for _, k := range order {
				listed[k] = true
				if slices.Contains(names, k+".md") {
					re = append(re, k+".md")
				}
			}
			for _, n := range names {
				if n != "README.md" && !listed[strings.TrimSuffix(n, ".md")] {
					re = append(re, n)
				}
			}
			names = re
		} else if slices.Contains(names, "README.md") {
			rest := []string{"README.md"}
			for _, n := range names {
				if n != "README.md" {
					rest = append(rest, n)
				}
			}
			names = rest
		}
		for _, n := range names {
			out = append(out, top+"/"+n)
		}
	}
	return out
}

// Text is every current-truth page, concatenated in reading order, each after a `<!-- file: rel -->` line.
func (d *Docs) Text() string {
	var b strings.Builder
	for _, r := range d.Pages() {
		fmt.Fprintf(&b, "<!-- file: %s -->\n%s\n", r, d.Page(r))
	}
	return b.String()
}

func (d *Docs) DDL() string { return d.Page("schema/schema.sql") }

var sqlBlock = regexp.MustCompile("(?s)```sql\n(.*?)\n```")

// sqlBlocks is every ```sql block of a page.
func sqlBlocks(text string) []string {
	var out []string
	for _, m := range sqlBlock.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	return out
}

type keyedBlock struct{ key, sql string }

// CookbookBlocks is every ```sql block of the cookbook, recipes in index order.
func (d *Docs) CookbookBlocks() []keyedBlock {
	var out []keyedBlock
	for _, k := range d.CookbookOrder() {
		for _, b := range sqlBlocks(d.Page("cookbook/" + k + ".md")) {
			out = append(out, keyedBlock{k, b})
		}
	}
	return out
}

// Blocks is the first sql block of each cookbook recipe, by its key, and the keys in order.
func (d *Docs) Blocks() (map[string]string, []string) {
	m, order := map[string]string{}, []string{}
	for _, b := range d.CookbookBlocks() {
		if _, ok := m[b.key]; !ok {
			m[b.key] = b.sql
			order = append(order, b.key)
		}
	}
	return m, order
}

func (d *Docs) Block(key string) string { m, _ := d.Blocks(); return m[key] }

// ---- the suite and its expectations

// S is one run of one suite: the tree under test, its expectations, and a scratch folder.
type S struct {
	name         string
	d            *Docs
	ddl          string
	dir          string
	ok, n        int
	fails        []string
	failedLabels []string
	mu           sync.Mutex
	open         []*sql.DB
	seq          int
}

// K records one expectation; the label states the expected outcome.
func (s *S) K(label string, cond bool, detail ...any) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	if cond {
		s.ok++
	} else {
		d := ""
		if len(detail) > 0 {
			d = " " + clip(fmt.Sprintf("%v", detail), 300)
		}
		s.failedLabels = append(s.failedLabels, label)
		s.fails = append(s.fails, label+d)
	}
	return cond
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// next is a counter for generated titles ("Person 3", "Note 4").
func (s *S) next() int { s.mu.Lock(); defer s.mu.Unlock(); s.seq++; return s.seq }

func (s *S) close() {
	for _, c := range s.open {
		c.Close()
	}
}

// stop ends a suite that cannot go on: a broken document can make a later step impossible, which is a failed
// expectation (reported by the runner), not a crash of the test binary.
func stop(format string, a ...any) { panic(fmt.Sprintf(format, a...)) }

// ---- connections

// C is one SQLite connection (a pool of exactly one, so raw BEGIN/COMMIT and :memory: databases behave).
type C struct {
	*sql.DB
	s *S
}

// F says how a fresh database is made.
type F struct {
	Path     string // "" = :memory:
	Hardened bool   // SQLITE_DBCONFIG_DEFENSIVE + trusted_schema = OFF (contract/connections)
	FKOff    bool
	RTOff    bool
	DDL      string // "" = the DDL under test
}

func dsn(p string, params ...string) string {
	if p == "" || p == ":memory:" {
		p = ":memory:"
	} else {
		parts := strings.Split(filepath.ToSlash(p), "/")
		for i := range parts {
			parts[i] = url.PathEscape(parts[i])
		}
		p = strings.Join(parts, "/")
	}
	q := append([]string{"_pragma=busy_timeout(5000)", "_txlock=immediate"}, params...)
	return "file:" + p + "?" + strings.Join(q, "&")
}

// connect opens one connection with no schema and no pragmas but a 5 s busy timeout.
func (s *S) connect(p string, params ...string) *C {
	d, err := sql.Open("sqlite", dsn(p, params...))
	if err != nil {
		stop("open %s: %v", p, err)
	}
	d.SetMaxOpenConns(1)
	d.SetMaxIdleConns(1)
	d.SetConnMaxLifetime(0)
	d.SetConnMaxIdleTime(0)
	s.mu.Lock()
	s.open = append(s.open, d)
	s.mu.Unlock()
	return &C{d, s}
}

// readOnly opens an existing file with mode=ro.
func (s *S) readOnly(p string) *C { return s.connect(p, "mode=ro") }

// fresh is a connection with the schema applied and the contract/connections pragmas set.
func (s *S) fresh() *C { return s.freshWith(F{}) }

func (s *S) freshWith(f F) *C {
	var params []string
	if f.Hardened {
		params = append(params, "_defensive=1", "_pragma=trusted_schema(0)")
	}
	c := s.connect(f.Path, params...)
	ddl := f.DDL
	if ddl == "" {
		ddl = s.ddl
	}
	c.must(ddl)
	c.must("PRAGMA foreign_keys=" + onOff(!f.FKOff))
	c.must("PRAGMA recursive_triggers=" + onOff(!f.RTOff))
	return c
}

func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

// P is a set of named parameters; a statement is bound with the ones its code names.
type P map[string]any

var paramName = regexp.MustCompile(`:(\w+)`)

func binds(q string, args []any) []any {
	if len(args) != 1 {
		return args
	}
	p, ok := args[0].(P)
	if !ok {
		return args
	}
	var out []any
	seen := map[string]bool{}
	for _, m := range paramName.FindAllStringSubmatch(code(q), -1) {
		if v, ok := p[m[1]]; ok && !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, sql.Named(m[1], v))
		}
	}
	return out
}

// must runs a statement (or a script) that has to succeed.
func (c *C) must(q string, args ...any) {
	if _, err := c.Exec(q, binds(q, args)...); err != nil {
		stop("%s: %v", clip(strings.TrimSpace(q), 80), err)
	}
}

// tryx is "OK" or "ERR <message>": a refused statement is a result, not a crash.
func (c *C) tryx(q string, args ...any) string {
	if _, err := c.Exec(q, binds(q, args)...); err != nil {
		return "ERR " + err.Error()
	}
	return "OK"
}

// query runs a statement and returns its rows, or the error.
func (c *C) query(q string, args ...any) ([][]any, error) {
	return c.queryCtx(context.Background(), q, args...)
}

func (c *C) queryCtx(ctx context.Context, q string, args ...any) ([][]any, error) {
	rs, err := c.QueryContext(ctx, q, binds(q, args)...)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	cols, _ := rs.Columns()
	var out [][]any
	for rs.Next() {
		row := make([]any, len(cols))
		ptr := make([]any, len(cols))
		for i := range row {
			ptr[i] = &row[i]
		}
		if err := rs.Scan(ptr...); err != nil {
			return nil, err
		}
		for i, v := range row {
			if b, ok := v.([]byte); ok {
				row[i] = string(b)
			}
		}
		out = append(out, row)
	}
	return out, rs.Err()
}

// rows is the rows of a statement that has to succeed.
func (c *C) rows(q string, args ...any) [][]any {
	r, err := c.query(q, args...)
	if err != nil {
		stop("%s: %v", clip(strings.TrimSpace(q), 80), err)
	}
	return r
}

// one is the first column of the first row, or nil (also when the statement is refused).
func (c *C) one(q string, args ...any) any {
	r, err := c.query(q, args...)
	if err != nil || len(r) == 0 || len(r[0]) == 0 {
		return nil
	}
	return r[0][0]
}

// n is one() as an integer, -1 when it is not one.
func (c *C) n(q string, args ...any) int64 {
	switch v := c.one(q, args...).(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	}
	return -1
}

// str is one() as text ("None" for NULL or nothing).
func (c *C) str(q string, args ...any) string { return val(c.one(q, args...)) }

// col is the first column of every row, as text.
func (c *C) col(q string, args ...any) []string {
	var out []string
	for _, r := range c.rows(q, args...) {
		out = append(out, val(r[0]))
	}
	return out
}

// tab is the rows as text: columns joined by |, rows by ; — what the expectations compare.
func (c *C) tab(q string, args ...any) string { return tab(c.rows(q, args...)) }

func tab(rows [][]any) string {
	var rs []string
	for _, r := range rows {
		var cs []string
		for _, v := range r {
			cs = append(cs, val(v))
		}
		rs = append(rs, strings.Join(cs, "|"))
	}
	return strings.Join(rs, "; ")
}

func val(v any) string {
	switch v := v.(type) {
	case nil:
		return "None"
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

func ids(v ...int64) string {
	var out []string
	for _, i := range v {
		out = append(out, strconv.FormatInt(i, 10))
	}
	return strings.Join(out, "; ")
}

func (c *C) integrityOK() bool {
	return c.tab("PRAGMA integrity_check") == "ok" && c.tab("PRAGMA foreign_key_check") == ""
}

// plan is the EXPLAIN QUERY PLAN of a statement, its details joined by |.
func (c *C) plan(q string, args ...any) string {
	var out []string
	for _, r := range c.rows("EXPLAIN QUERY PLAN "+q, args...) {
		out = append(out, val(r[3]))
	}
	return strings.Join(out, " | ")
}

// ---- the insert conventions

// M is a set of columns.
type M map[string]any

func (m M) split() (cols []string, marks string, vals []any) {
	for k := range m {
		cols = append(cols, k)
	}
	sort.Strings(cols)
	for _, k := range cols {
		vals = append(vals, m[k])
	}
	return cols, strings.TrimSuffix(strings.Repeat("?,", len(cols)), ","), vals
}

// identity constructs the actual preferred spelling with its owner in a nested
// savepoint. It works inside an existing transaction without changing its ownership.
// Typed detail is separate only for explicit extension-insertion/orphan probes.
func (c *C) identity(typ, source, title string, day any, body string) int64 {
	id, err := c.tryIdentity(typ, title, M{"source": source, "day": day, "body": body})
	if err != nil {
		stop("construct %s identity: %v", typ, err)
	}
	return id
}

// tryIdentity is a DDL probe with explicit owner/spelling, not a writer save.
// Even refusal at deferred-FK release rolls back the entire probe, not its caller.
func (c *C) tryIdentity(typ, title string, overrides M) (int64, error) {
	c.must("SAVEPOINT fixture_identity")
	key := text.TitleKey(title)
	cols := M{"entity_type": typ, "preferred_name_key": key, "source": "ui", "created_at": "2026-01-01T00:00:00.000Z", "updated_at": "2026-01-01T00:00:00.000Z"}
	for k, v := range overrides {
		cols[k] = v
	}
	names, marks, vals := cols.split()
	rows, err := c.query("INSERT INTO entities("+strings.Join(names, ",")+") VALUES ("+marks+") RETURNING id", vals...)
	var id int64
	if err == nil {
		id = rows[0][0].(int64)
		_, err = c.query("INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,?,?)", id, title, key)
	}
	if err == nil {
		_, err = c.query("RELEASE fixture_identity")
	}
	if err != nil {
		c.must("ROLLBACK TO fixture_identity")
		c.must("RELEASE fixture_identity")
		return 0, err
	}
	return id, nil
}

// anyIdentity is a synthetic named identity when the subject needs no particular
// spelling. This is its sole actual name, not a temporary alias or missing owner.
func (c *C) anyIdentity(typ string) int64 {
	return c.identity(typ, "ui", fmt.Sprintf("%s fixture %d", typ, c.s.next()), nil, "")
}

// page is a plain page titled title, key title_key(title), no day, an empty body.
func (c *C) page(title string) int64 { return c.pageW(title, nil, "") }

// pageW is a page with a day and a body.
func (c *C) pageW(title string, day any, body string) int64 {
	return c.identity("page", "ui", title, day, body)
}

// dayPage is the journal page of a local day (D5): titled with the day, its day the title.
func (c *C) dayPage(day, body string) int64 { return c.pageW(day, day, body) }

// named is a person, a metric or a file (D20): the entity and its page (entity_type = typ), and its typed row — one id.
func (c *C) named(typ, handle string, cols ...M) int64 {
	if handle == "" {
		handle = fmt.Sprintf("%s %d", strings.ToUpper(typ[:1])+typ[1:], c.s.next())
	}
	c.must("SAVEPOINT fixture_named")
	i := c.identity(typ, "ui", handle, nil, "")
	var m M
	if len(cols) > 0 {
		m = cols[0]
	}
	c.domain(typ, i, m)
	c.must("RELEASE fixture_named")
	return i
}

func (c *C) domain(typ string, id int64, cols M) {
	table, m := "people", M{"id": id, "name": "P"}
	switch typ {
	case "metric": // a metric's row holds its unit (D27)
		table, m = "metrics", M{"id": id, "unit": ""}
	case "file": // a file's row holds its original's hash and type (D9); each one a different original
		table, m = "files", M{"id": id, "sha256": fmt.Sprintf("%064x", c.s.next()), "mime": "image/jpeg"}
	}
	for k, v := range cols {
		m[k] = v
	}
	cs, marks, vals := m.split()
	c.must("INSERT INTO "+table+"("+strings.Join(cs, ",")+") VALUES ("+marks+")", vals...)
}

// thing is any entity with its domain row: a page, or a named one.
func (c *C) thing(typ string) int64 {
	if typ == "page" {
		return c.pageW(fmt.Sprintf("Page %d", c.s.next()), nil, "x")
	}
	return c.named(typ, "")
}

func (c *C) link(from, to any, kind string, source ...string) string {
	src := "ui"
	if len(source) > 0 {
		src = source[0]
	}
	return c.tryx("INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES (?,?,?,"+NOW+",?)", from, to, kind, src)
}

// measure inserts a reading: 'OK' or 'ERR <message>'. cols may set source and the optional columns.
func (c *C) measure(metric any, day string, value any, cols ...M) string {
	m := M{"metric_id": metric, "day": day, "value": value, "source": "ui"}
	for _, x := range cols {
		for k, v := range x {
			m[k] = v
		}
	}
	cs, marks, vals := m.split()
	return c.tryx("INSERT INTO measurements("+strings.Join(cs, ",")+",created_at) VALUES ("+marks+","+NOW+")", vals...)
}

// jpegBytes is a real JPEG (8×8): a preview the DDL and the writer both accept.
var jpegBytes = phototest.JPEG(8, 8)

// metric is a metric (D27): the entity, its page titled title, and its metrics row with the unit, one id.
func (c *C) metric(title, unit string) int64 {
	return c.named("metric", title, M{"unit": unit})
}

// ---- statements and blocks

// complete is sqlite3_complete(): whether the text ends a statement (a trigger body counts as one statement).
func complete(sql string) bool {
	const (
		tkSEMI = iota
		tkWS
		tkOTHER
		tkEXPLAIN
		tkCREATE
		tkTEMP
		tkTRIGGER
		tkEND
	)
	trans := [8][8]int{
		{1, 0, 2, 3, 4, 2, 2, 2}, // 0 INVALID
		{1, 1, 2, 3, 4, 2, 2, 2}, // 1 START
		{1, 2, 2, 2, 2, 2, 2, 2}, // 2 NORMAL
		{1, 3, 3, 2, 4, 2, 2, 2}, // 3 EXPLAIN
		{1, 4, 2, 2, 2, 4, 5, 2}, // 4 CREATE
		{6, 5, 5, 5, 5, 5, 5, 5}, // 5 TRIGGER
		{6, 6, 5, 5, 5, 5, 5, 7}, // 6 SEMI
		{1, 7, 5, 5, 5, 5, 5, 5}, // 7 END
	}
	idChar := func(b byte) bool {
		return b >= 0x80 || b == '_' || b == '$' || (b >= '0' && b <= '9') || (b|0x20 >= 'a' && b|0x20 <= 'z')
	}
	state := 0
	for i := 0; i < len(sql); i++ {
		var tok int
		switch b := sql[i]; {
		case b == ';':
			tok = tkSEMI
		case b == ' ' || b == '\r' || b == '\t' || b == '\n' || b == '\f':
			tok = tkWS
		case b == '/' && i+1 < len(sql) && sql[i+1] == '*':
			j := strings.Index(sql[i+2:], "*/")
			if j < 0 {
				return false
			}
			i += 2 + j + 1
			tok = tkWS
		case b == '-' && i+1 < len(sql) && sql[i+1] == '-':
			j := strings.IndexByte(sql[i:], '\n')
			if j < 0 {
				return state == 1
			}
			i += j
			tok = tkWS
		case b == '[':
			j := strings.IndexByte(sql[i+1:], ']')
			if j < 0 {
				return false
			}
			i += 1 + j
			tok = tkOTHER
		case b == '`' || b == '"' || b == '\'':
			j := strings.IndexByte(sql[i+1:], b)
			if j < 0 {
				return false
			}
			i += 1 + j
			tok = tkOTHER
		case idChar(b):
			j := i
			for j < len(sql) && idChar(sql[j]) {
				j++
			}
			switch strings.ToLower(sql[i:j]) {
			case "create":
				tok = tkCREATE
			case "trigger":
				tok = tkTRIGGER
			case "temp", "temporary":
				tok = tkTEMP
			case "end":
				tok = tkEND
			case "explain":
				tok = tkEXPLAIN
			default:
				tok = tkOTHER
			}
			i = j - 1
		default:
			tok = tkOTHER
		}
		state = trans[state][tok]
	}
	return state == 1
}

var lineComment = regexp.MustCompile(`--[^\n]*`)

// statements splits a block into complete statements, dropping comment-only ones.
func statements(sql string) []string {
	var out []string
	acc := ""
	for _, line := range strings.SplitAfter(sql, "\n") {
		acc += line
		if complete(acc) {
			if code(acc) != "" {
				out = append(out, acc)
			}
			acc = ""
		}
	}
	return out
}

// code is a statement without its comments.
func code(st string) string { return strings.TrimSpace(lineComment.ReplaceAllString(st, "")) }

// verb is the first word of a statement, upper case.
func verb(st string) string {
	f := strings.Fields(code(st))
	if len(f) == 0 {
		return ""
	}
	return strings.ToUpper(strings.TrimSuffix(f[0], ";"))
}

var returningName = regexp.MustCompile(`RETURNING id;[^\n]*?:(\w+)`)

// runBlock executes a document block statement by statement, as a writer would: it binds the :params a statement
// names, and keeps an id a statement RETURNs under the :name its comment gives ('RETURNING id;  -- ... :page_id').
// It returns each statement's rows.
func (c *C) runBlock(sql string, p P, after func(string)) ([][][]any, error) {
	var out [][][]any
	for _, st := range statements(sql) {
		rows, err := c.query(st, p)
		if err != nil {
			return out, err
		}
		if m := returningName.FindStringSubmatch(st); m != nil && len(rows) > 0 {
			p[m[1]] = rows[0][0]
		}
		out = append(out, rows)
		if after != nil {
			after(code(st))
		}
	}
	return out, nil
}

// ---- the save contract

// The steps of cookbook/save-a-body, each one statement of its block.
type saveSteps struct {
	begin, sp, sel, rev, ent, pg, ln, rel, dele, commit string
	counts                                              map[string]int
}

func (s *S) saveSteps() (saveSteps, bool) {
	sts := statements(s.d.Block("save-a-body"))
	st := saveSteps{counts: map[string]int{}}
	for _, x := range []struct {
		name, prefix string
		to           *string
	}{
		{"begin", "BEGIN IMMEDIATE", &st.begin}, {"sp", "SAVEPOINT", &st.sp}, {"sel", "SELECT", &st.sel},
		{"rev", "UPDATE ENTITIES", &st.rev}, {"ent", "INSERT INTO ENTITIES", &st.ent}, {"pg", "INSERT INTO ENTITY_NAMES", &st.pg},
		{"ln", "INSERT INTO LINKS", &st.ln}, {"rel", "RELEASE", &st.rel}, {"dele", "DELETE FROM LINKS", &st.dele},
		{"commit", "COMMIT", &st.commit},
	} {
		for _, s := range sts {
			if strings.HasPrefix(strings.ToUpper(code(s)), x.prefix) {
				st.counts[x.name]++
				*x.to = s
			}
		}
	}
	for _, n := range st.counts {
		if n != 1 {
			return st, false
		}
	}
	return st, len(st.counts) == 10
}

// docSave is the link sync of cookbook/save-a-body run literally (steps 1-4), inside the caller's transaction,
// with the targets the writer's extraction finds. With validate false it skips the title predicate, so the
// SAVEPOINT alone must keep a bad target from blocking the save. An error of a target's steps 1-3 skips the target;
// an error of SAVEPOINT, ROLLBACK TO or RELEASE stops the save, as the recipe says.
func (c *C) docSave(st saveSteps, pageID int64, body, ownKey string, validate bool) (linked []int64, skipped []string) {
	var keys, titles []string
	if validate {
		var rejected []string
		keys, titles, rejected = text.Targets(body, ownKey)
		skipped = rejected
	} else {
		seen := map[string]bool{}
		for _, t := range text.Candidates(body) {
			k := text.TitleKey(t)
			if k == ownKey || seen[k] {
				continue
			}
			seen[k] = true
			keys, titles = append(keys, k), append(titles, t)
		}
	}
	linkedIDs := map[int64]bool{}
	for i, key := range keys {
		title := titles[i]
		c.must(st.sp)
		tid, err := func() (int64, error) {
			r, err := c.query(st.sel, P{"key": key})
			if err != nil {
				return 0, err
			}
			var tid int64
			if len(r) == 0 {
				e, err := c.query(st.ent, P{"source": "ui", "key": key, "title": title})
				if err != nil {
					return 0, err
				}
				tid = e[0][0].(int64)
				if _, err := c.query(st.pg, P{"title": title, "key": key, "target_id": tid}); err != nil {
					return 0, err
				}
			} else {
				tid = r[0][0].(int64)
				if tid == pageID || linkedIDs[tid] {
					return 0, nil
				}
				if r[0][2] != nil {
					if _, err := c.query(st.rev, P{"found_id": tid}); err != nil {
						return 0, err
					}
				}
			}
			_, err = c.query(st.ln, P{"page_id": pageID, "target_id": tid, "source": "ui"})
			return tid, err
		}()
		if err != nil {
			c.must("ROLLBACK TO target")
			c.must("RELEASE target")
			skipped = append(skipped, title)
			continue
		}
		c.must(st.rel)
		if tid != 0 {
			linked = append(linked, tid)
			linkedIDs[tid] = true
		}
	}
	j, _ := json.Marshal(append([]int64{}, linked...))
	c.must(st.dele, P{"page_id": pageID, "target_ids": string(j)})
	return
}

// The writer's own save path (the application's core) on this connection: what a save of a body does.

func (c *C) store() *core.Store { return &core.Store{DB: &db.DB{W: c.DB, R: c.DB}} }

// savePage creates a page with this body (title "" = a fresh "Note N"), its links synced in the same transaction.
func (c *C) savePage(body, title string) (int64, core.Sync) {
	if title == "" {
		title = fmt.Sprintf("Note %d", c.s.next())
	}
	id, r, err := c.store().CreatePage(context.Background(), "ui", title, body, "")
	if err != nil {
		stop("save %q: %v", title, err)
	}
	return id, r
}

// editBody replaces a page's body and syncs its links in one transaction.
func (c *C) editBody(id int64, body string) core.Sync {
	var r core.Sync
	err := c.store().Do(context.Background(), "ui", func(t *core.Tx) (e error) { r, e = t.SetBody(id, body); return })
	if err != nil {
		stop("edit %d: %v", id, err)
	}
	return r
}

// capture appends an entry to the day page of day (cookbook/capture), creating or reviving it.
func (c *C) capture(entry, day string) int64 {
	id, _, err := c.store().Capture(context.Background(), "ui", day, entry, nil)
	if err != nil {
		stop("capture %q: %v", day, err)
	}
	return id
}

// linksOf is the titles a page links to by wikilink, sorted.
func (c *C) linksOf(pid int64) []string {
	return slices.Sorted(slices.Values(c.col("SELECT n.title FROM links l JOIN entities e ON e.id=l.to_id JOIN entity_names n ON n.entity_id=e.id AND n.name_key=e.preferred_name_key WHERE l.from_id=? AND l.kind='wikilink'", pid)))
}

// relPath is a link target on the page base, relative to docs/ ('..' leaves the tree).
func relPath(base, url string) (target, anchor string) {
	p, a, _ := strings.Cut(url, "#")
	if p == "" {
		return base, a
	}
	return path.Clean(path.Join(path.Dir(base), p)), a
}

// bypass runs change with one trigger dropped and restores it: deliberate damage, or a file written before the rule.
func bypass(c *C, trigger string, change func()) {
	definition := c.str("SELECT sql FROM sqlite_schema WHERE type='trigger' AND name=?", trigger)
	c.must("DROP TRIGGER " + trigger)
	change()
	c.must(definition)
}

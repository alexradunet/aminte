package tests

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// document: the docs themselves — presence of the threat model's 2075 reading summaries in a fresh database
// (each table's inside its CREATE statement, the cross-table ones in a few aminte_meta rows, which name only what
// the file has); the tree holds together (every decision D1..Dn in its own record, every relative link resolves,
// every page is reachable from docs/README.md); the totals in schema/README.md match.
func document(s *S) {
	// ---- the 2075 test
	c := s.fresh()
	meta := map[string]string{}
	for _, r := range c.rows("select key, value from aminte_meta") {
		meta[val(r[0])] = val(r[1])
	}
	schema := map[string]string{}
	for _, r := range c.rows("select name, sql from sqlite_schema where sql is not null") {
		schema[val(r[0])] = val(r[1])
	}
	rows := regexp.MustCompile(`(?m)^\|\s*(\d+)\s*\|([^|]+)\|([^|]+)\|([^|]+)\|\s*$`).FindAllStringSubmatch(s.d.Page("contract/threat-model.md"), -1)
	numbered := len(rows) >= 20
	var nums []string
	for i, r := range rows {
		nums = append(nums, r[1])
		numbered = numbered && r[1] == strconv.Itoa(i+1)
	}
	s.K("the 2075 table has at least 20 questions, numbered 1..n without a gap", numbered, nums)
	tick := regexp.MustCompile("`([^`]+)`")
	used := map[string]bool{}
	for _, r := range rows {
		var places, phrases, missing, absent []string
		for _, m := range tick.FindAllStringSubmatch(r[3], -1) {
			places = append(places, m[1])
		}
		for _, m := range tick.FindAllStringSubmatch(r[4], -1) {
			phrases = append(phrases, strings.ToLower(m[1]))
		}
		var text strings.Builder
		for _, p := range places {
			_, inMeta := meta[p]
			_, inSchema := schema[p]
			if !inMeta && !inSchema {
				missing = append(missing, p)
			}
			text.WriteString(meta[p] + " " + schema[p] + " ")
			used[p] = true
		}
		s.K("Q"+r[1]+": every place named is a aminte_meta key or a schema object", len(places) > 0 && len(missing) == 0, missing)
		lower := strings.ToLower(text.String())
		for _, p := range phrases {
			if !strings.Contains(lower, p) {
				absent = append(absent, p)
			}
		}
		s.K(fmt.Sprintf("Q%s: the answer says %v", r[1], phrases), len(phrases) > 0 && len(absent) == 0, absent)
	}
	var unused []string
	for k := range meta {
		if !used[k] {
			unused = append(unused, k)
		}
	}
	s.K("every aminte_meta key answers some question (no rule without a question)", len(unused) == 0, slices.Sorted(slices.Values(unused)))
	// A row names only what the file has (issue 0082): a word with an underscore is a column, a schema object or a
	// aminte_meta key of this file, or a pragma, which some row writes as PRAGMA x or x=VALUE.
	known := map[string]bool{}
	for k := range meta {
		known[k] = true
	}
	for name := range schema {
		known[name] = true
		for _, col := range c.col("select name from pragma_table_info(?)", name) {
			known[col] = true
		}
	}
	snake := regexp.MustCompile(`(PRAGMA )?\b([a-z][a-z0-9]*(?:_[a-z0-9]+)+)\b(=?)`)
	named := map[string][][]string{}
	for k, v := range meta {
		named[k] = snake.FindAllStringSubmatch(v, -1)
		for _, m := range named[k] {
			if m[1] != "" || m[3] != "" {
				known[m[2]] = true
			}
		}
	}
	var stale []string
	for k, ms := range named {
		for _, m := range ms {
			if !known[m[2]] {
				stale = append(stale, k+": "+m[2])
			}
		}
	}
	s.K("every name with an underscore in a aminte_meta row is a column, a schema object, a key or a pragma of this file", len(stale) == 0, slices.Sorted(slices.Values(stale)))
	// A stored CREATE text names only the link kinds of the file (issue 0082): a word of the shape x-of or x-in, the
	// shape of part-of, is a registered kind.
	kinds := map[string]bool{}
	for _, k := range c.col("select kind from link_kinds") {
		kinds[k] = true
	}
	relation := regexp.MustCompile(`\b[a-z]+-(?:of|in)\b`)
	var unregistered []string
	for name, sql := range schema {
		for _, w := range relation.FindAllString(sql, -1) {
			if !kinds[w] {
				unregistered = append(unregistered, name+": "+w)
			}
		}
	}
	s.K("every word of the shape x-of or x-in in the stored schema is a registered link kind", len(unregistered) == 0, slices.Sorted(slices.Values(unregistered)))
	s.K("import-a-row-once sends an imported body through the save contract (D19)", strings.Contains(s.d.Page("cookbook/import-a-row-once.md"), "run the link sync of [save a body](save-a-body.md)"))

	// ---- the rules live in the file, once
	s.K("aminte_meta holds only the few cross-table rules (at most 12 keys)", len(meta) <= 12, slices.Sorted(maps.Keys(meta)))
	head := ddlHeader(s.ddl)
	s.K("the DDL header before the first statement is a short pointer (<= 12 lines) naming aminte_meta", strings.Count(head, "\n") <= 12 && strings.Contains(head, "aminte_meta"))
	inner := regexp.MustCompile(`\n\s*--`)
	for _, t := range []string{"entities", "entity_names", "people", "metrics", "measurements", "link_kinds", "links", "aminte_meta"} {
		s.K(t+": its CREATE statement carries its rules as comments", inner.MatchString(schema[t]))
	}
	count := func(k string) int64 {
		return c.n("select count(*) from sqlite_schema where type=? and name not like 'sqlite_%' and name NOT IN (SELECT name FROM pragma_table_list WHERE type='shadow')", k)
	}
	nt, nv, ng := count("table"), count("view"), count("trigger")
	tot := regexp.MustCompile(`(?s)\*\*(\d+) tables \+ 1 FTS5 virtual table \+ (\d+) views\*\*.*?\*\*\+ (\d+) triggers\.\*\*`).FindStringSubmatch(s.d.Text())
	s.K("the totals in schema/README.md match the DDL (tables, views, triggers)", tot != nil && atoi(tot[1])+1 == nt && atoi(tot[2]) == nv && atoi(tot[3]) == ng, tot, nt, nv, ng)

	// ---- the tree holds together
	tree := map[string]string{}
	for _, r := range s.d.Pages() {
		tree[r] = s.d.Page(r)
	}
	for _, top := range recordTops[:2] { // issues, rfcs: their indexes and templates are linked pages too
		for _, f := range []string{"README.md", "template.md"} {
			tree[top+"/"+f] = s.d.Page(top + "/" + f)
		}
	}
	p := s.problems(tree)
	s.K("the tree holds together", len(p) == 0, p[:min(5, len(p))])
	const d7 = "decisions/D07-measurements.md"
	broken := func(r, old, nw string) map[string]string {
		t := copyTree(tree)
		t[r] = strings.Replace(t[r], old, nw, 1)
		return t
	}
	without := copyTree(tree)
	delete(without, d7)
	orphan := copyTree(tree)
	orphan["contract/orphan.md"] = "# Orphan\n"
	for _, x := range []struct {
		name string
		tree map[string]string
	}{
		{"a decision deleted", without},
		{"a decision without its status", broken(d7, "**Status:** accepted", "")},
		{"a broken link", broken("contract/imports.md", "\n", "\nSee [nowhere](nowhere.md).\n")},
		{"a broken anchor", broken("contract/imports.md", "\n", "\nSee [R999](../research/references.md#r999).\n")},
		{"an orphan page", orphan},
	} {
		q := s.problems(x.tree)
		s.K("a broken copy is noticed: "+x.name, len(q) > 0 && strings.Join(q, "\n") != strings.Join(p, "\n"), q[:min(1, len(q))])
	}
	var bad []string
	for _, f := range []string{"README.md", "AGENTS.md", "tests/README.md"} {
		text, ok := readFile(filepath.Join(repoRoot, filepath.FromSlash(f)))
		if !ok {
			bad = append(bad, f+": missing")
			continue
		}
		for _, u := range mdLinks(text) {
			target, _, _ := strings.Cut(u, "#")
			if _, err := os.Stat(filepath.Join(repoRoot, filepath.Dir(f), filepath.FromSlash(target))); err != nil {
				bad = append(bad, f+": "+u)
			}
		}
	}
	s.K("every relative link of README.md, AGENTS.md and tests/README.md resolves", len(bad) == 0, bad)
}

func atoi(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }

func copyTree(t map[string]string) map[string]string {
	out := make(map[string]string, len(t))
	for k, v := range t {
		out[k] = v
	}
	return out
}

// ddlHeader is the DDL before its first statement: up to the first line that is neither blank nor a comment,
// less the blank lines just before it.
func ddlHeader(ddl string) string {
	lines := strings.SplitAfter(ddl, "\n")
	at, first := 0, -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t != "" && !strings.HasPrefix(t, "--") {
			first = i
			break
		}
	}
	if first < 0 {
		return ddl
	}
	start := first
	for start > 0 && strings.TrimSpace(lines[start-1]) == "" {
		start--
	}
	for _, l := range lines[:start] {
		at += len(l)
	}
	return ddl[:at]
}

var (
	fence       = regexp.MustCompile("(?sm)^```.*?^```")
	heading     = regexp.MustCompile(`(?m)^#{1,6} (.+)$`)
	htmlAnchor  = regexp.MustCompile(`<a id="([^"]+)"></a>`)
	mdLink      = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	scheme      = regexp.MustCompile(`^[a-z]+:`)
	inlineLink  = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	notSlugChar = regexp.MustCompile(`[^\p{L}\p{N}_\- ]`)
	adrFile     = regexp.MustCompile(`^decisions/D\d\d-[a-z0-9-]+\.md$`)
	statusLine  = regexp.MustCompile(`(?m)^\*\*Status:\*\* (accepted|deferred)$`)
	indexStatus = regexp.MustCompile(`(?m)^\*\*Status:\*\* [^\n]{10,}$`)
)

// stripCode drops fenced blocks and code spans (a run of backticks, at least one character on the same line, the
// same run again), so their text is neither a link nor a heading.
func stripCode(text string) string {
	text = fence.ReplaceAllString(text, "")
	var b strings.Builder
	for i := 0; i < len(text); {
		if text[i] != '`' {
			b.WriteByte(text[i])
			i++
			continue
		}
		run := 0
		for i+run < len(text) && text[i+run] == '`' {
			run++
		}
		end := -1
		for k := run; k >= 1 && end < 0; k-- {
			ticks := strings.Repeat("`", k)
			for e := i + k + 1; e+k <= len(text); e++ {
				if text[e-1] == '\n' {
					break
				}
				if text[e:e+k] == ticks {
					end = e + k
					break
				}
			}
		}
		if end < 0 {
			b.WriteByte(text[i])
			i++
			continue
		}
		i = end
	}
	return b.String()
}

// slug is GitHub's heading anchor: lowercase, punctuation dropped, spaces to hyphens.
func slug(h string) string {
	h = strings.ToLower(strings.TrimSpace(inlineLink.ReplaceAllString(h, "$1")))
	return strings.ReplaceAll(notSlugChar.ReplaceAllString(h, ""), " ", "-")
}

func anchors(text string) map[string]bool {
	out := map[string]bool{}
	for _, m := range htmlAnchor.FindAllStringSubmatch(text, -1) {
		out[m[1]] = true
	}
	seen := map[string]int{}
	for _, m := range heading.FindAllStringSubmatch(stripCode(text), -1) {
		sl := slug(m[1])
		n := seen[sl]
		seen[sl] = n + 1
		if n == 0 {
			out[sl] = true
		} else {
			out[fmt.Sprintf("%s-%d", sl, n)] = true
		}
	}
	return out
}

// mdLinks is the relative links of a page, outside code.
func mdLinks(text string) []string {
	var out []string
	for _, m := range mdLink.FindAllStringSubmatch(stripCode(text), -1) {
		if !scheme.MatchString(m[1]) {
			out = append(out, m[1])
		}
	}
	return out
}

// problems is what is wrong with a docs tree: its decision records, its links and anchors, its reachability.
func (s *S) problems(tree map[string]string) []string {
	var out []string
	var adr []string
	for r := range tree {
		if adrFile.MatchString(r) {
			adr = append(adr, r)
		}
	}
	sort.Strings(adr)
	gap := len(adr) < 24
	var nums []int
	for i, r := range adr {
		n, _ := strconv.Atoi(r[11:13])
		nums = append(nums, n)
		gap = gap || n != i+1
	}
	if gap {
		out = append(out, fmt.Sprintf("decisions are not D01..Dn without a gap: %v", nums))
	}
	for _, r := range adr {
		n, _ := strconv.Atoi(r[11:13])
		h1, _, _ := strings.Cut(tree[r], "\n")
		if !strings.HasPrefix(h1, fmt.Sprintf("# D%d — ", n)) {
			out = append(out, r+`: the title is not "# D`+strconv.Itoa(n)+` — ..."`)
		}
		if strings.Contains(h1, "*(") {
			out = append(out, r+": the title carries a parenthetical status")
		}
		if !statusLine.MatchString(tree[r]) {
			out = append(out, r+": no **Status:** accepted|deferred line")
		}
		if !strings.Contains(tree["decisions/README.md"], "]("+r[10:]+")") {
			out = append(out, r+" is not in the decision index")
		}
	}
	if !indexStatus.MatchString(tree["README.md"]) {
		out = append(out, "docs/README.md has no one-line status")
	}
	var rels []string
	for r := range tree {
		rels = append(rels, r)
	}
	sort.Strings(rels)
	for _, r := range rels {
		for _, u := range mdLinks(tree[r]) {
			target, anchor := relPath(r, u)
			_, inTree := tree[target]
			exists := s.d.exists(target)
			if strings.HasPrefix(target, "..") {
				_, err := os.Stat(filepath.Join(docsRoot, filepath.FromSlash(target)))
				exists = err == nil
			}
			switch {
			case !inTree && !exists:
				out = append(out, r+": broken link "+u)
			case anchor != "" && inTree && !anchors(tree[target])[anchor]:
				out = append(out, fmt.Sprintf("%s: no anchor #%s in %s", r, anchor, target))
			}
		}
	}
	reach := map[string]bool{}
	todo := []string{"README.md"}
	for len(todo) > 0 {
		r := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		if _, ok := tree[r]; reach[r] || !ok {
			continue
		}
		reach[r] = true
		for _, u := range mdLinks(tree[r]) {
			t, _ := relPath(r, u)
			todo = append(todo, t)
		}
	}
	var orphans []string
	for _, r := range rels {
		if !reach[r] {
			orphans = append(orphans, r)
		}
	}
	if len(orphans) > 0 {
		out = append(out, fmt.Sprintf("pages not reachable from docs/README.md: %v", orphans))
	}
	return out
}

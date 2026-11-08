package tests

// A suite that cannot fail proves nothing. Each mutant is the docs tree with one rule broken — in schema.sql, in a
// cookbook block or in the text of a page — and the suite that owns the rule must notice: it must run to its end
// and fail its explicitly named rule witness (stopping on the broken document is not "noticed"). Every suite named
// here must first pass on the unmutated document. A new rule gets a mutant here.

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
)

// change is one broken rule: replace the nth occurrence of old (counted over schema.sql and then the pages in
// reading order) by new; or make one named CHECK always true; or make one trigger never fire.
type change struct {
	kind, a, b string
	nth        int
}

func edit(old, new string) change  { return change{"edit", old, new, 0} }
func nocheck(name string) change   { return change{"nocheck", name, "", 0} }
func notrigger(name string) change { return change{"notrigger", name, "", 0} }

// apply is the files the change breaks, by their path relative to docs/.
func (ch change) apply(d *Docs) (map[string]string, error) {
	files := append([]string{"schema/schema.sql"}, d.Pages()...)
	replace := func(old, new string, nth int) (map[string]string, error) {
		seen := 0
		for _, r := range files {
			t := d.Page(r)
			for i := 0; ; {
				j := strings.Index(t[i:], old)
				if j < 0 {
					break
				}
				if seen == nth {
					at := i + j
					return map[string]string{r: t[:at] + new + t[at+len(old):]}, nil
				}
				seen++
				i += j + len(old)
			}
		}
		return nil, fmt.Errorf("mutation target found %dx: %q", seen, clip(old, 70))
	}
	ddl := d.DDL()
	switch ch.kind {
	case "nocheck": // its condition is always true; the CHECK's parenthesis is found by counting
		m := regexp.MustCompile(`CONSTRAINT ` + ch.a + `\s+CHECK\s*\(`).FindStringIndex(ddl)
		if m == nil {
			return nil, fmt.Errorf("no CHECK %s", ch.a)
		}
		i, depth := m[1], 1
		for ; depth > 0 && i < len(ddl); i++ {
			switch ddl[i] {
			case '(':
				depth++
			case ')':
				depth--
			}
		}
		return replace(ddl[m[0]:i], "CONSTRAINT "+ch.a+" CHECK (1)", 0)
	case "notrigger": // its WHEN becomes WHEN 0, or WHEN 0 is added before BEGIN
		m := regexp.MustCompile(`(?s)CREATE TRIGGER ` + ch.a + `\b(.*?)BEGIN`).FindStringSubmatch(ddl)
		if m == nil {
			return nil, fmt.Errorf("no trigger %s", ch.a)
		}
		head := m[1]
		if w := strings.Index(head, "WHEN "); w >= 0 {
			head = head[:w]
		} else {
			head = strings.TrimRight(head, " \t\n") + " "
		}
		return replace(m[0], "CREATE TRIGGER "+ch.a+head+"WHEN 0 BEGIN", 0)
	}
	return replace(ch.a, ch.b, ch.nth)
}

var mutants = []struct {
	suite, name, witness string
	change               change
}{
	{"temporal-model", "category traversal starts at a tombstoned root", "category tombstoned root hides live descendant metrics", edit("SELECT id FROM entities WHERE id = :parent_id AND deleted_at IS NULL", "SELECT :parent_id")},
	{"temporal-model", "category traversal crosses a tombstoned intermediate", "category tombstoned intermediate stops descendant traversal", edit("JOIN entities child ON child.id = l.from_id AND child.deleted_at IS NULL", "JOIN entities child ON child.id = l.from_id")},
	{"schema-safeguards", "people identity guard omitted", "people id combined=false identity refusal is atomic", notrigger("people_identity_fixed")},
	{"schema-safeguards", "files identity guard omitted", "files id combined=false identity refusal is atomic", notrigger("files_identity_fixed")},
	{"schema-safeguards", "metrics identity guard omitted", "metrics id combined=false identity refusal is atomic", notrigger("metrics_identity_fixed")},
	{"schema-safeguards", "entity creation time can change", "entity creation time immutable", edit(" OR NEW.created_at IS NOT OLD.created_at\nBEGIN\n  -- provenance", "\nBEGIN\n  -- provenance")},
	{"schema-safeguards", "FTS compound accents retained", "FTS strips compound Latin diacritics", edit("tokenize='unicode61 remove_diacritics 2'", "tokenize='unicode61 remove_diacritics 1'")},
	{"measurement-query-plan", "measurements_capture lookup index omitted", "measurements_capture supports retained reference lookup", edit("CREATE INDEX measurements_capture ON measurements(captured_with_id) WHERE captured_with_id IS NOT NULL;", "")},
	{"integrity", "correction semantic check omitted", "correction integrity detects a correction of another metric", edit("WHERE m.supersedes_id IS NOT NULL AND (p.id IS NULL OR p.metric_id IS NOT m.metric_id) ORDER BY m.id;", "WHERE 0 ORDER BY m.id;")},
	{"integrity", "correction integrity treats every row as rooted", "correction integrity detects a rootless cycle", edit(" SELECT id FROM measurements WHERE supersedes_id IS NULL\n UNION\n", " SELECT id FROM measurements\n UNION\n")},
	{"integrity", "journal integrity ignores extra aliases", "journal integrity detects extra journal aliases", edit("AND (n.name_key IS NOT e.preferred_name_key OR n.title IS NOT e.preferred_name_key))\n OR (e.period", "AND 0 AND (n.name_key IS NOT e.preferred_name_key OR n.title IS NOT e.preferred_name_key))\n OR (e.period")},
	{"integrity", "journal integrity ignores a date name on another owner", "journal integrity detects a date name on another owner", edit("(e.entity_type<>'page' OR e.preferred_name_key IS NOT n.name_key OR e.day IS NOT n.name_key OR n.title IS NOT n.name_key)", "(n.title IS NOT n.name_key)")},
	{"document", "in-file summary omits writer conformance dependency", "Q1: the answer says [aminte create statement conforming writer vectors]", edit("a conforming writer also needs the matching docs contract and vectors", "a reader needs no other specification")},
	{"document", "in-file instants omit commit-order limitation", "Q2: the answer says [utc iso-8601 commit order]", edit("supplied clock timestamps may coincide or regress and do not encode commit order", "timestamps establish chronology")},
	{"document", "in-file deletion summary omits revival limitation", "Q6: the answer says [tombstone links registries revival]", edit("revival clears the tombstone", "restoration clears the tombstone")},
	{"document", "in-file evolution omits preservation boundary", "Q16: the answer says [additive user_version data-preserving explicit columns]", edit("additive means data-preserving", "additive means unrestricted")},
	{"document", "in-file provenance omits last-editor limitation", "Q19: the answer says [written at insert agent last editor]", edit("not the last editor or an audit trail", "the current modifier")},
	{"cookbook", "metric series ignores metric tombstones", "active measurement recipes hide tombstoned metrics", edit("JOIN entities e ON e.id = me.metric_id AND e.deleted_at IS NULL\n  JOIN entity_names n ON n.entity_id = me.metric_id AND n.name_key = 'weight'", "JOIN entities e ON e.id = me.metric_id\n  JOIN entity_names n ON n.entity_id = me.metric_id AND n.name_key = 'weight'")},
	{"cookbook", "mood series ignores metric tombstones", "active measurement recipes hide tombstoned metrics", edit("JOIN entities e ON e.id = me.metric_id AND e.deleted_at IS NULL\n  JOIN entity_names n ON n.entity_id = me.metric_id AND n.name_key = 'mood'", "JOIN entities e ON e.id = me.metric_id\n  JOIN entity_names n ON n.entity_id = me.metric_id AND n.name_key = 'mood'")},
	{"cookbook", "day measurement read ignores metric tombstones", "active measurement recipes hide tombstoned metrics", edit("FROM measurement_values me JOIN metrics m ON m.id = me.metric_id\n    JOIN entities e ON e.id = m.id AND e.deleted_at IS NULL", "FROM measurement_values me JOIN metrics m ON m.id = me.metric_id\n    JOIN entities e ON e.id = m.id")},
	{"cookbook", "recorded-time history ignores cutoff", "recorded-time cutoff excludes later recorded facts", edit("WHERE me.created_at <= :as_of", "WHERE 1")},
	{"cookbook", "recorded-time history only follows immediate corrections", "recorded-time cutoff follows eligible descendants across a clock regression", edit("SELECT me.supersedes_id FROM measurements me JOIN overridden o ON me.id = o.id\n  WHERE me.supersedes_id IS NOT NULL", "SELECT me.supersedes_id FROM measurements me JOIN overridden o ON me.id = o.id\n  WHERE 0")},
	{"cookbook", "recorded-time history exposes retractions as readings", "recorded-time cutoff applies retractions at the cutoff", edit("WHERE me.value IS NOT NULL AND NOT EXISTS (SELECT 1 FROM overridden o WHERE o.id = me.id)", "WHERE NOT EXISTS (SELECT 1 FROM overridden o WHERE o.id = me.id)")},
	{"cookbook", "recorded-time cutoff excludes equal timestamps", "recorded-time cutoff resolves equal timestamps by correction ancestry", edit("WHERE me.created_at <= :as_of", "WHERE me.created_at < :as_of")},
	{"schema-safeguards", "entities_source NUL guard omitted", "entities_source rejects embedded NUL", edit("CONSTRAINT entities_source CHECK (instr(source, char(0)) = 0 AND ", "CONSTRAINT entities_source CHECK (")},
	{"schema-safeguards", "entity_names_key_folded NUL guard omitted", "entity_names_key_folded rejects embedded NUL", edit("CONSTRAINT entity_names_key_folded CHECK (instr(name_key, char(0)) = 0 AND ", "CONSTRAINT entity_names_key_folded CHECK (")},
	{"schema-safeguards", "files_sha256 NUL guard omitted", "files_sha256 rejects embedded NUL", edit("CONSTRAINT files_sha256 CHECK (instr(sha256, char(0)) = 0 AND ", "CONSTRAINT files_sha256 CHECK (")},
	{"schema-safeguards", "files_mime NUL guard omitted", "files_mime rejects embedded NUL", edit("CONSTRAINT files_mime CHECK (instr(mime, char(0)) = 0 AND ", "CONSTRAINT files_mime CHECK (")},
	{"schema-safeguards", "measurements_tz NUL guard omitted", "measurements_tz rejects embedded NUL", edit("CONSTRAINT measurements_tz CHECK (tz IS NULL OR (instr(tz, char(0)) = 0 AND ", "CONSTRAINT measurements_tz CHECK (tz IS NULL OR (")},
	{"schema-safeguards", "measurements_source NUL guard omitted", "measurements_source rejects embedded NUL", edit("CONSTRAINT measurements_source CHECK (instr(source, char(0)) = 0 AND ", "CONSTRAINT measurements_source CHECK (")},
	{"schema-safeguards", "links_source NUL guard omitted", "links_source rejects embedded NUL", edit("CONSTRAINT links_source CHECK (instr(source, char(0)) = 0 AND ", "CONSTRAINT links_source CHECK (")},
	{"schema-safeguards", "link_kinds_kind NUL guard omitted", "link_kinds_kind rejects embedded NUL", edit("CONSTRAINT link_kinds_kind CHECK (instr(kind, char(0)) = 0 AND ", "CONSTRAINT link_kinds_kind CHECK (")},
	{"schema-safeguards", "link_kinds_from_types NUL guard omitted", "link_kinds_from_types rejects embedded NUL", edit("CONSTRAINT link_kinds_from_types CHECK (from_types IS NULL OR (instr(from_types, char(0)) = 0 AND ", "CONSTRAINT link_kinds_from_types CHECK (from_types IS NULL OR (")},
	{"schema-safeguards", "link_kinds_to_types NUL guard omitted", "link_kinds_to_types rejects embedded NUL", edit("CONSTRAINT link_kinds_to_types CHECK (to_types   IS NULL OR (instr(to_types, char(0)) = 0 AND ", "CONSTRAINT link_kinds_to_types CHECK (to_types   IS NULL OR (")},
	{"schema-safeguards", "ghost view ignores measurement references", "ghost pages exclude measurement capture provenance", edit("     AND NOT EXISTS (SELECT 1 FROM measurements m WHERE m.captured_with_id = e.id)", "")},
	{"id-guards", "links ignore rowid alias updates", "links rowid combined=true identity refusal is atomic", edit("CREATE TRIGGER links_fixed BEFORE UPDATE ON links", "CREATE TRIGGER links_fixed BEFORE UPDATE OF id, from_id, to_id, kind, created_at, source ON links")},
	{"id-guards", "registry ignores rowid alias updates", "entity_names rowid combined=true identity refusal is atomic", edit("CREATE TRIGGER entity_names_fixed BEFORE UPDATE ON entity_names", "CREATE TRIGGER entity_names_fixed BEFORE UPDATE OF id, entity_id, name_key ON entity_names")},
	{"identity", "alias insertion preserves stale revision", "owned alias insertion advances revision", notrigger("entity_names_touch_insert")},
	{"identity", "equivalent spelling preserves stale revision", "equivalent owned spelling edit advances revision", notrigger("entity_names_touch_update")},
	{"identity", "owned preferred selection preserves stale revision", "already-owned preferred selection advances revision", edit(" OR NEW.preferred_name_key IS NOT OLD.preferred_name_key\n    OR NEW.deleted_at", "\n    OR NEW.deleted_at")},
	{"name-grammar", "registry permits opening brackets", "registry rejects bracket spelling 0", edit("AND instr(title, '[') = 0 ", "")},
	{"name-grammar", "registry permits closing brackets", "registry rejects bracket spelling 1", edit("AND instr(title, ']') = 0", "")},
	{"name-grammar", "NFC syntax pair is addressable", "reference-name predicate agrees with a`b`c", edit("{\"name\":\"a\\u1fefb\\u1fefc\",\"accepted\":false}", "{\"name\":\"a\\u1fefb\\u1fefc\",\"accepted\":true}")},
	{"name-grammar", "entity escape spelling is addressable", "reference-name predicate agrees with R&amp;D", edit("{\"name\":\"R&amp;D\",\"accepted\":false}", "{\"name\":\"R&amp;D\",\"accepted\":true}")},
	{"name-grammar", "intraword underscores are not addressable", "reference-name predicate agrees with a_b", edit("{\"name\":\"a_b\",\"key\":\"a_b\",\"accepted\":true}", "{\"name\":\"a_b\",\"key\":\"a_b\",\"accepted\":false}")},
	{"name-grammar", "NFD names retain a noncanonical key", "raw and NFC reference forms preserve key for Café", edit("{\"name\":\"Cafe\\u0301\",\"key\":\"café\",\"accepted\":true}", "{\"name\":\"Cafe\\u0301\",\"key\":\"cafe\\u0301\",\"accepted\":true}")},
	{"named", "person extensions ignore owner type", "a person extension requires a person owner", edit("FOREIGN KEY (id, entity_type) REFERENCES entities(id, entity_type),\n  CONSTRAINT people_death_day_order", "FOREIGN KEY (id) REFERENCES entities(id),\n  CONSTRAINT people_death_day_order")},
	{"names", "alias index is updated before insertion", "name insertion indexes an alias on the existing document", edit("CREATE TRIGGER entity_names_fts_insert AFTER INSERT ON entity_names BEGIN", "CREATE TRIGGER entity_names_fts_insert BEFORE INSERT ON entity_names BEGIN")},
	{"names", "journal preferred identity may change", "journal selection refuses a deliberately damaged owned alias", notrigger("entities_day_identity")},

	{"name-ownership", "preferred owner may be NULL", "preferred ownership is mandatory", edit("preferred_name_key TEXT NOT NULL,", "preferred_name_key TEXT,")},
	{"name-ownership", "preferred ownership is immediate", "entity insertion defers actual owned-name completion", edit("REFERENCES entity_names(entity_id, name_key) DEFERRABLE INITIALLY DEFERRED", "REFERENCES entity_names(entity_id, name_key)")},
	{"name-ownership", "names may reference an absent entity", "a name cannot refer to an absent owner", edit("entity_id   INTEGER NOT NULL REFERENCES entities(id),", "entity_id   INTEGER NOT NULL,")},
	{"names", "registry ids may change", "names own immutable registry ids", edit("NEW.id IS NOT OLD.id OR NEW.entity_id IS NOT OLD.entity_id OR NEW.name_key IS NOT OLD.name_key", "NEW.entity_id IS NOT OLD.entity_id OR NEW.name_key IS NOT OLD.name_key")},
	{"names", "owned aliases may transfer", "names cannot transfer owners", edit("NEW.id IS NOT OLD.id OR NEW.entity_id IS NOT OLD.entity_id OR NEW.name_key IS NOT OLD.name_key", "NEW.id IS NOT OLD.id OR NEW.name_key IS NOT OLD.name_key")},
	{"names", "owned keys may change", "names cannot change normalized keys", edit("NEW.id IS NOT OLD.id OR NEW.entity_id IS NOT OLD.entity_id OR NEW.name_key IS NOT OLD.name_key", "NEW.id IS NOT OLD.id OR NEW.entity_id IS NOT OLD.entity_id")},
	{"names", "aliases may be deleted", "owned names cannot be deleted", notrigger("entity_names_no_delete")},
	{"names", "preferred name may belong to another owner", "an unowned preferred selection refuses", edit("FOREIGN KEY (id, preferred_name_key) REFERENCES entity_names(entity_id, name_key) DEFERRABLE INITIALLY DEFERRED", "FOREIGN KEY (preferred_name_key) REFERENCES entity_names(name_key) DEFERRABLE INITIALLY DEFERRED")},
	{"names", "alias insert is not indexed", "name insertion indexes an alias on the existing document", notrigger("entity_names_fts_insert")},
	{"names", "alias insert deletes the wrong aggregate", "name insertion preserves the original indexed content", edit("WHERE entity_id = e.id AND id <> NEW.id", "WHERE entity_id = e.id")},
	{"names", "preferred selection keeps old index", "preferred selection reindexes without losing old aliases", notrigger("entities_fts_before_update")},
	{"names", "preferred selection omits new index", "preferred selection reindexes without losing old aliases", notrigger("entities_fts_update")},
	{"names", "spelling keeps old tokens", "case-equivalent spelling update keeps FTS consistent", notrigger("entity_names_fts_before_update")},
	{"names", "spelling omits new tokens", "case-equivalent spelling update keeps FTS consistent", notrigger("entity_names_fts_update")},
	{"names", "journal accepts aliases", "journal ownership permits no extra names", notrigger("entity_names_day_insert")},
	{"names", "journal spelling is editable", "journal spelling is fixed", notrigger("entity_names_day_update")},
	{"names", "name ownership is optional", "an unowned preferred selection refuses", edit("FOREIGN KEY (id, preferred_name_key) REFERENCES entity_names(entity_id, name_key) DEFERRABLE INITIALLY DEFERRED", "CHECK (1)")},
	{"dates", "exact days accept a negative year", "exact entities.day \"-0001-01-01\" accepted=false", edit("length(day) = 10 AND day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND ", "")},
	{"dates", "exact instant representation check absent", "exact entities.created_at \"-001-01-01T00:00:00.000Z\" accepted=false", nocheck("entities_created_at")},
	{"dates", "exact instants accept hour 24", "exact entities.created_at \"2026-01-01T24:59:59.000Z\" accepted=false", edit("substr(created_at,12,2) BETWEEN '00' AND '23' AND ", "")},
	{"integrity", "semantic integrity ignores invalid typed destinations", "typed-edge semantic query detects deliberate endpoint damage", edit("OR (k.to_types IS NOT NULL AND instr(',' || k.to_types || ',', ',' || t.entity_type || ',')=0)", "OR (0)")},

	{"links", "a category may be a day page", "link part-of page->day page (a category is never a day page, D26): ERR", edit("WHERE NEW.kind = 'part-of' AND coalesce((SELECT is_journal FROM entities WHERE id = NEW.to_id)", "WHERE 0 AND coalesce((SELECT is_journal FROM entities WHERE id = NEW.to_id)")},
	{"links", "a day page may be filed", "link part-of day page->page (a day page is never filed, D26): ERR", edit("WHERE NEW.kind = 'part-of' AND coalesce((SELECT is_journal FROM entities WHERE id = NEW.from_id)", "WHERE 0 AND coalesce((SELECT is_journal FROM entities WHERE id = NEW.from_id)")},
	{"links", "a page with a part-of link may become a day page", "a page with no name yet, filed in a category, cannot become a day page (D26)", edit(" WHERE NEW.is_journal\n   AND (EXISTS (SELECT 1 FROM links l", " WHERE 0\n   AND (EXISTS (SELECT 1 FROM links l")},
	{"links", "link ID can change", "links are immutable: id", edit("WHEN NEW.id IS NOT OLD.id OR NEW.from_id", "WHEN NEW.from_id")},
	{"links", "link creation time can change", "links are immutable: created_at", edit("OR NEW.created_at IS NOT OLD.created_at OR NEW.source", "OR NEW.source")},
	{"links", "a used kind can be replaced past its fixed structure", "REPLACE of a used kind changing the endpoint types is refused by the links FK", edit("REFERENCES link_kinds(kind) ON DELETE RESTRICT,", "REFERENCES link_kinds(kind),")},
	{"evolution", "type changes strand retained typed links", "type change refuses a retained incoming typed edge", notrigger("entities_endpoint_types")},
	{"identity", "revision can start at zero", "zero revision refused", nocheck("entities_revision")},
	{"identity", "revision can decrease", "revision decrease refused", notrigger("entities_revision_monotonic")},
	{"identity", "body edits keep the revision", "body changes advance revision", notrigger("entities_touch")},
	{"identity", "lifecycle edits keep the revision", "lifecycle changes advance revision", notrigger("entities_touch")},
	{"identity", "person edits keep the revision", "person details advance revision", notrigger("people_touch")},
	{"identity", "metric edits keep the revision", "metric details advance revision", notrigger("metrics_touch")},
	{"identity", "file edits keep the revision", "file details advance revision", notrigger("files_touch")},
	{"identity", "link insertion keeps revision", "a link insertion advances the page it leaves", notrigger("links_touch_insert")},
	{"identity", "link deletion keeps revision", "a link deletion advances the page it leaves", notrigger("links_touch_delete")},
	{"identity", "link notes keep revision", "a link note edit advances the page it leaves", notrigger("links_touch_note")},
	{"identity", "the owned preferred name inserted with its entity advances the revision", "creation is not an edit: a page made by cookbook/capture's entity and owned-name statements is at revision 1 with updated_at = created_at", edit("CREATE TRIGGER entity_names_touch_insert AFTER INSERT ON entity_names\n WHEN NEW.name_key IS NOT (SELECT preferred_name_key FROM entities WHERE id = NEW.entity_id)\nBEGIN", "CREATE TRIGGER entity_names_touch_insert AFTER INSERT ON entity_names\nBEGIN")},
	{"identity", "typed detail insertions advance the revision (people, metrics and files, as before)", "creation is not an edit: a page, person, metric and file stay at revision 1 with updated_at = created_at after their typed row", edit("CREATE TRIGGER links_fixed BEFORE UPDATE ON links", "CREATE TRIGGER people_touch_insert AFTER INSERT ON people BEGIN UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id = NEW.id; END;\nCREATE TRIGGER metrics_touch_insert AFTER INSERT ON metrics BEGIN UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id = NEW.id; END;\nCREATE TRIGGER files_touch_insert AFTER INSERT ON files BEGIN UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id = NEW.id; END;\n"+"CREATE TRIGGER links_fixed BEFORE UPDATE ON links")},
	{"identity", "a link insertion advances the page it points at", "a link insertion advances no page it points at", edit("revision = revision + 1 WHERE id = NEW.from_id;\nEND;\nCREATE TRIGGER links_touch_delete", "revision = revision + 1 WHERE id IN (NEW.from_id,NEW.to_id);\nEND;\nCREATE TRIGGER links_touch_delete")},
	{"identity", "a link deletion advances the page it pointed at", "a link deletion advances no page it points at", edit("revision = revision + 1 WHERE id = OLD.from_id;", "revision = revision + 1 WHERE id IN (OLD.from_id,OLD.to_id);")},
	{"identity", "a link note edit advances the page it points at", "a link note edit advances no page it points at", edit("revision = revision + 1 WHERE id = NEW.from_id;\nEND;\nCREATE TRIGGER entities_endpoint_types", "revision = revision + 1 WHERE id IN (NEW.from_id,NEW.to_id);\nEND;\nCREATE TRIGGER entities_endpoint_types")},
	{"identity", "a wikilink insertion advances its page", "a wikilink insertion advances no page: it is derived from the body", edit("CREATE TRIGGER links_touch_insert AFTER INSERT ON links WHEN NEW.kind <> 'wikilink' BEGIN", "CREATE TRIGGER links_touch_insert AFTER INSERT ON links BEGIN")},
	{"identity", "a wikilink deletion advances its page", "a wikilink deletion advances no page: it is derived from the body", edit("CREATE TRIGGER links_touch_delete AFTER DELETE ON links WHEN OLD.kind <> 'wikilink' BEGIN", "CREATE TRIGGER links_touch_delete AFTER DELETE ON links BEGIN")},
	{"journal", "the day view flags a page by comparing its two clocks", "a page that was only created, with its owned name, is not flagged \"(edited)\", whatever its two clocks say", edit("CASE WHEN e.revision > 1 THEN ' (edited)'", "CASE WHEN e.updated_at > e.created_at THEN ' (edited)'")},
	{"document", "in-file edit summary counts creation as an edit", "Q23: the answer says [monotonic independent no-ops rollback exhaustion creation is not an edit never the page it points at]", edit("creation is not an edit (nothing advances", "creation is an edit (nothing advances")},

	{"dates", "a day CHECK uses = instead of IS", "entities.day rejects \"2026-13-01\"", edit("CONSTRAINT entities_day CHECK (day IS NULL OR length(day) = 10 AND day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(day) IS day)", "CONSTRAINT entities_day CHECK (day IS NULL OR length(day) = 10 AND day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(day) = day)")},
	{"dates", "instants lose their milliseconds", "measurements.created_at accepts a UTC instant with milliseconds", edit("CONSTRAINT measurements_created_at CHECK (length(created_at) = 24 AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z' AND substr(created_at,12,2) BETWEEN '00' AND '23' AND strftime('%Y-%m-%dT%H:%M:%fZ', created_at) IS created_at)", "CONSTRAINT measurements_created_at CHECK (length(created_at) = 24 AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z' AND substr(created_at,12,2) BETWEEN '00' AND '23' AND strftime('%Y-%m-%dT%H:%M:%SZ', created_at) IS created_at)")},
	{"identity", "cookbook/capture attaches the mood with last_insert_rowid()", "cookbook/capture run literally, with a link sync inside: the mood reading points at the day page the RETURNING gave", edit("SELECT e.id, '2026-09-29', 4, 'ui', :page_id, strftime", "SELECT e.id, '2026-09-29', 4, 'ui', last_insert_rowid(), strftime")},
	{"identity", "entities.source may be NULL", "entities.source is TEXT NOT NULL with no default", edit("source      TEXT NOT NULL CONSTRAINT entities_source", "source      TEXT CONSTRAINT entities_source")},
	{"identity", "entities.source can change", "entities.source cannot change", edit("  WHEN NEW.source IS NOT OLD.source OR NEW.import_key IS NOT OLD.import_key", "  WHEN NEW.import_key IS NOT OLD.import_key")},
	{"identity", "entities.import_key can change", "entities.import_key cannot change", edit("  WHEN NEW.source IS NOT OLD.source OR NEW.import_key IS NOT OLD.import_key", "  WHEN NEW.source IS NOT OLD.source")},
	{"identity", "an entity may be imported twice", "a second entity with the same (source, import_key) is refused", edit("CREATE UNIQUE INDEX entities_import", "CREATE INDEX entities_import")},
	{"identity", "cookbook/import-a-row-once revives a tombstoned import", "cookbook/import-a-row-once: a tombstoned import is neither updated nor inserted again", edit("WHERE source = 'import:vault' AND import_key = :import_key AND deleted_at IS NULL);", "WHERE source = 'import:vault' AND import_key = :import_key);")},
	{"identity", "people may be hard-deleted", "DELETE FROM people is refused", edit("CREATE TRIGGER people_no_delete BEFORE DELETE ON people\nBEGIN SELECT RAISE(ABORT, 'people are never deleted: tombstone the entity (entities.deleted_at)'); END;\n", "")},
	{"identity", "editing a page does not bump updated_at", "updating a page bumps entities.updated_at", notrigger("entities_touch")},
	{"identity", "the tombstone does not bump updated_at", "a tombstone bumps updated_at to the tombstone instant, and the trigger does not re-fire itself", notrigger("entities_touch")},
	{"named", "a day page may be promoted", "a day page cannot become a person or a metric: the cascade meets entities_day_page_plain", nocheck("entities_day_page_plain")},
	{"named", "a metric may be promoted", "a metric is never promoted to a person, a file or a plain page (entities_promote_from_page)", notrigger("entities_promote_from_page")},
	{"named", "entities_promote_from_page refuses an update that keeps the type", "a full-row update of a person that keeps its type passes", edit("  WHEN NEW.entity_type IS NOT OLD.entity_type AND OLD.entity_type IS NOT 'page'\n", "  WHEN OLD.entity_type IS NOT 'page'\n")},
	{"named", "ghost_pages lists the page of a person", "ghost_pages lists the real ghost and none of the person pages, and not a page a rename points at", edit("WHERE e.entity_type = 'page' AND e.body = ''", "WHERE e.body = ''")},
	{"named", "a wikilink cannot land on a person", "the day page links to the person's own id, and no new page was made", edit("('wikilink', 'page,person,metric,file', 'page,person,metric,file',", "('wikilink', 'page,person,metric,file', 'page',")},
	{"named", "a page may claim an unknown type", "entities.entity_type is checked without foreign keys", nocheck("entities_entity_type")},
	{"pages", "the title CHECK lets the zero-width space through", "U+200B in a title is rejected by the DB and by the writer", edit("|| char(8203) || char(8206)", "|| char(8206)")},
	{"pages", "a device name before an extension is allowed", "unsafe or device title \"con.txt\" rejected", edit("upper(CASE WHEN instr(title, '.') > 0 THEN substr(title, 1, instr(title, '.') - 1) ELSE title END)", "upper(title)")},
	{"pages", "the title index is not unique", "DIET without a day collides with Diet with a day", edit("name_key    TEXT NOT NULL UNIQUE,", "name_key    TEXT NOT NULL,")},
	{"pages", "a page may have no title", "a page with a key but no title rejected", edit("title       TEXT NOT NULL,              -- preferred spelling", "title       TEXT,                       -- preferred spelling")},
	{"pages", "pages_fts_update re-indexes on every update", "a new day touches the entity revision only, not the derived index", edit("CREATE TRIGGER entities_fts_update AFTER UPDATE OF body, preferred_name_key ON entities\n WHEN NEW.body IS NOT OLD.body OR NEW.preferred_name_key IS NOT OLD.preferred_name_key BEGIN", "CREATE TRIGGER entities_fts_update AFTER UPDATE OF body, day, preferred_name_key ON entities BEGIN")},
	{"pages", "the cookbook/save-a-body resolve scans by lower(title)", "...and is an indexed SEARCH on the registry", edit("WHERE n.name_key = :key;", "WHERE lower(n.title) = :key;")},
	{"links", "link kinds may change structure", "link_kinds: to_types is fixed", edit("  WHEN NEW.from_types IS NOT OLD.from_types OR NEW.to_types IS NOT OLD.to_types\n", "  WHEN 0\n")},
	{"facts", "a scale value is not held to its range", "Mood 6 refused: a 1-5 scale", notrigger("measurements_scale_range")},
	{"facts", "a scale's unit may have its bounds out of order", "a scale's unit with its bounds out of order (5-1, 3-3) is refused, and nothing is written", notrigger("metrics_range_order")},
	{"facts", "a correction may be of another metric", "a correction of another metric is refused", edit("   WHERE (SELECT metric_id FROM measurements WHERE id = NEW.supersedes_id) IS NOT NEW.metric_id;", "   WHERE 0;")},
	{"facts", "measurement values may be infinite", "+Infinity refused", edit("CHECK (value IS NULL OR abs(value) <= 1.7976931348623157e308)", "CHECK (value IS NULL OR value = value)")},
	{"facts", "measurements may be updated", "UPDATE of a value refused", edit("CREATE TRIGGER measurements_no_update BEFORE UPDATE ON measurements\nBEGIN\n", "CREATE TRIGGER measurements_no_update BEFORE UPDATE ON measurements WHEN 0\nBEGIN\n")},
	{"journal", "a day page may have no day", "journal day cannot be cleared", nocheck("entities_day_page")},
	{"journal", "the journal definition expects nine characters", `entities.is_journal of "2026-01-01" is 1`, edit("length(preferred_name_key) = 10 AND date(preferred_name_key) IS preferred_name_key) VIRTUAL", "length(preferred_name_key) = 9 AND date(preferred_name_key) IS preferred_name_key) VIRTUAL")},
	{"journal", "the journal definition drops the date round trip", `entities.is_journal of "2459000.50" is 0`, edit("length(preferred_name_key) = 10 AND date(preferred_name_key) IS preferred_name_key) VIRTUAL", "length(preferred_name_key) = 10 AND date(preferred_name_key) IS NOT NULL) VIRTUAL")},
	{"journal", "no key is a journal day", `entities.is_journal of "2026-01-01" is 1`, edit("length(preferred_name_key) = 10 AND date(preferred_name_key) IS preferred_name_key) VIRTUAL", "0) VIRTUAL")},
	{"journal", "a period page may have a day", "a period page with a day is refused (entities_period_page)", nocheck("entities_period_page")},
	{"journal", "a period may not exist", `the period "2027-W53" does not exist: entities_period_exists refuses it`, nocheck("entities_period_exists")},
	{"journal", "a week key is not a period", `the page "2026-W43" is accepted and its period is week`, edit("WHEN preferred_name_key GLOB '[0-9][0-9][0-9][0-9]-w[0-9][0-9]' THEN 'week' END) VIRTUAL", "WHEN preferred_name_key GLOB '[0-9][0-9][0-9][0-9]-x[0-9][0-9]' THEN 'week' END) VIRTUAL")},
	{"journal", "a period page may have an alias", "an alias on a month page is refused", edit("AND ((e.period IS NOT NULL AND NEW.name_key IS NOT e.preferred_name_key)", "AND ((0 AND NEW.name_key IS NOT e.preferred_name_key)")},
	{"journal", "a period page may respell its key", "a week spelled in lower case cannot be set by an update", edit("   AND NEW.title IS NOT upper(OLD.name_key);", "   AND 0;")},
	{"journal", "a period page may be renamed", "a year page cannot be renamed", edit("WHERE OLD.period IS NOT NULL\n    OR (NEW.period", "WHERE 0\n    OR (NEW.period")},
	{"journal", "a category may be a period page", "a part-of link to a month page is refused (a category is never a period page)", edit("WHERE NEW.kind = 'part-of' AND coalesce((SELECT period IS NOT NULL FROM entities WHERE id = NEW.to_id), 0)", "WHERE NEW.kind = 'part-of' AND 0")},
	{"journal", "a filed page may become a period page", "a page with no name yet, filed in a category, cannot become a period page (D30)", edit("WHERE NEW.period IS NOT NULL\n   AND (EXISTS (SELECT 1 FROM links l WHERE l.from_id", "WHERE 0\n   AND (EXISTS (SELECT 1 FROM links l WHERE l.from_id")},
	{"journal", "a page may acquire a period key through its aliases", "a plain page does not acquire a period key through its aliases", edit("OR (NEW.period IS NOT NULL\n        AND EXISTS (SELECT 1 FROM entity_names n", "OR (0\n        AND EXISTS (SELECT 1 FROM entity_names n")},
	{"integrity", "period-name integrity ignores an alias on a period page", "period-name integrity detects an alias on a period page and a period key on another page, spelled in lower case", edit(" OR (e.period IS NOT NULL AND n.name_key IS NOT e.preferred_name_key)\n OR ((n.name_key GLOB", " OR (0)\n OR ((n.name_key GLOB")},
	{"journal", "a period page may be filed", "a part-of link from a week page is refused (a period page is never filed)", edit("coalesce((SELECT period IS NOT NULL FROM entities WHERE id = NEW.from_id), 0)", "0")},
	{"journal", "pages_day_page compares the day with = instead of IS", "journal day cannot be cleared", edit("OR day IS preferred_name_key)", "OR day = preferred_name_key)")},
	{"journal", "cookbook/capture looks for the day page under another key", "cookbook/capture finds the day page by its key, the day itself", edit("WHERE n.name_key = '2026-09-29';", "WHERE n.name_key = 'today';")},
	{"journal", "cookbook/days-that-name lists a tombstoned day", "...and drops a tombstoned day", edit("JOIN entities d ON d.id = l.from_id AND d.preferred_name_key = d.day AND d.deleted_at IS NULL\n WHERE l.to_id = :entity_id", "JOIN entities d ON d.id = l.from_id AND d.preferred_name_key = d.day\n WHERE l.to_id = :entity_id")},
	{"journal", "cookbook/days-that-name lists pages that are not days", "cookbook/days-that-name lists the day pages that link Ana, newest first, and not an essay that names her", edit("JOIN entities d ON d.id = l.from_id AND d.preferred_name_key = d.day AND d.deleted_at IS NULL\n WHERE l.to_id = :entity_id", "JOIN entities d ON d.id = l.from_id AND d.deleted_at IS NULL\n WHERE l.to_id = :entity_id")},
	{"journal", "cookbook/capture writes tombstoned Mood", "cookbook/capture does not write tombstoned Mood", edit("WHERE e.id = 1 AND e.deleted_at IS NULL;", "WHERE e.id = 1;")},
	{"journal", "cookbook/day-view shows superseded readings", "...not another day's page, a link target, a person's page, a page of another day or the superseded reading", edit("FROM measurement_values me JOIN metrics m ON m.id = me.metric_id\n    JOIN entities e ON e.id = m.id", "FROM measurements me JOIN metrics m ON m.id = me.metric_id\n    JOIN entities e ON e.id = m.id")},
	{"streaks", "cookbook/streaks counts a day page with an empty body", "a day page with an empty body is not written: the streak ends yesterday", edit("WHERE p.day = :today AND p.is_journal AND p.deleted_at IS NULL AND trim(p.body, ' ' || char(9,10,13)) <> ''", "WHERE p.day = :today AND p.is_journal AND p.deleted_at IS NULL")},
	{"streaks", "cookbook/streaks counts a tombstoned day page", "a tombstoned day page breaks the run: today, yesterday and the day before count 2", edit("p.day = date(r.day, '-1 day') AND p.is_journal AND p.deleted_at IS NULL", "p.day = date(r.day, '-1 day') AND p.is_journal")},
	{"writers", "contract/connections no longer sets trusted_schema = OFF", "the contract/connections block sets every pragma the contract names", edit("PRAGMA trusted_schema = OFF;   -- the schema may call only side-effect-free functions (all of this one's are)\n", "")},
	{"integrity", "the orphan query forgets people", "a person with an owned name but no extension: only the orphan query sees it", edit(" OR (e.entity_type='person' AND NOT EXISTS (SELECT 1 FROM people p WHERE p.id=e.id))", "")},
	{"integrity", "contract/integrity-checks loses the FTS5 integrity-check", "contract/integrity-checks has four groups: structure, foreign keys, domain/typed-edge semantics, FTS5", edit("INSERT INTO entities_fts(entities_fts, rank) VALUES ('integrity-check', 1);   -- no error\n", "")},
	{"imports", "the import block loses WHERE true", "the document's block loads 1 000 rows", edit("  FROM s.staging WHERE true\n", "  FROM s.staging\n")},
	{"imports", "contract/imports copies life.db through a read-write shell", "contract/imports step 1: the documented command opens the file read-only, with trusted_schema=OFF", edit("sqlite3 -readonly -cmd \"PRAGMA trusted_schema=OFF\" life.db \"VACUUM INTO '/tmp/trial.db'", "sqlite3 life.db \"VACUUM INTO '/tmp/trial.db'")},
	{"imports", "contract/imports copies life.db through a reader that trusts the schema", "contract/imports step 1: the documented command opens the file read-only, with trusted_schema=OFF", edit("sqlite3 -readonly -cmd \"PRAGMA trusted_schema=OFF\" life.db \"VACUUM INTO '/tmp/trial.db'", "sqlite3 -readonly life.db \"VACUUM INTO '/tmp/trial.db'")},
	{"snapshots", "the shell form of the snapshot trusts the schema", "the shell form of the snapshot opens life.db read-only, with trusted_schema=OFF", edit("sqlite3 -readonly -cmd \"PRAGMA trusted_schema=OFF\" life.db \"VACUUM INTO 'life-", "sqlite3 -readonly life.db \"VACUUM INTO 'life-")},
	{"snapshots", "cookbook/take-a-snapshot takes it on the writer's connection", "the snapshot block runs on a connection that opened life.db read-only (mode=ro)", edit("-- on a connection to life.db opened read-only (mode=ro), while", "-- on the writer's connection to life.db, while")},
	{"snapshots", "the restore check opens the snapshot read-only, where the FTS5 check is refused", "the restore check opens the snapshot with the writer's connection settings", edit("open it with the writer's connection settings", "open it read-only (mode=ro)")},
	{"snapshots", "the restore leaves life.db in rollback-journal mode", "the restored life.db is in WAL mode again", edit("PRAGMA journal_mode = WAL;   -- once, on the restored life.db", "PRAGMA journal_mode = DELETE;   -- once, on the restored life.db")},
	{"evolution", "one CHECK is unnamed", "every CHECK in schema is named", edit("CONSTRAINT people_death_day_order CHECK", "CHECK")},
	{"document", "a 2075 answer is gone from the file", "Q18: the answer says [3.51.3 3.53]", edit("  ('sqlite',    'writers need SQLite >= 3.51.3", "  ('sqlite',    'writers need SQLite >= 3.51")},
	{"document", "a aminte_meta key answers no question", "every aminte_meta key answers some question (no rule without a question)", edit("  ('evolution', 'after the freeze", "  ('orphan',    'x'),\n  ('evolution', 'after the freeze")},
	{"document", "cookbook/import-a-row-once bypasses the save contract", "import-a-row-once sends an imported body through the save contract (D19)", edit("run the link sync of [save a body](save-a-body.md)", "skip")},
	{"document", "the deletes row hides the registries", "Q6: the answer says [tombstone links registries revival]", edit("the registries (link_kinds, aminte_meta) are the owner''s administrative rows", "link_kinds and aminte_meta are the owner''s administrative rows")},
	{"document", "a aminte_meta row names a column of a removed table", "every name with an underscore in a aminte_meta row is a column, a schema object, a key or a pragma of this file", edit("never recomputed from an instant; CHECK", "never recomputed from an instant; task_occurrences.due_day is a chosen planning day; CHECK")},
	{"document", "a comment names a removed link kind", "every word of the shape x-of or x-in in the stored schema is a registered link kind", edit("a relationship between them is words in a page (D8).", "relationships between them are links (friend, family, parent-of).")},
	{"document", "the schema totals drift from the DDL", "the totals in schema/README.md match the DDL (tables, views, triggers)", edit("**+ 42 triggers.**", "**+ 30 triggers.**")},
	{"diagrams", "a foreign key is not drawn", "every foreign key is drawn as `parent --- child : first FK column`", edit("    entities ||--o| people : \"id\"\n", "")},
	{"diagrams", "the link map invents an edge", "the link map has the same edges as link_kinds", edit("    any -->|\"part-of\"| page\n", "    any -->|\"part-of\"| page\n    person -->|\"mentioned\"| page\n")},
	{"diagrams", "a new link kind, the map unchanged", "the link map has the same edges as link_kinds", edit("  ('part-of',  NULL,        'page',", "  ('mentor',   'person',    'person',       NULL),\n  ('part-of',  NULL,        'page',")},
	{"diagrams", "the correction story shows a wrong value", "executed, the view shows what each state of the diagram says", edit("state \"view shows 70.8\" as V2", "state \"view shows 70.9\" as V2")},
	{"diagrams", "a non-key column is drawn", "er-core.link_kinds.note is a key column (the diagrams draw keys only)", edit("    link_kinds {\n        TEXT kind PK\n", "    link_kinds {\n        TEXT kind PK\n        TEXT note\n")},
	{"facts", "metric-series has no upper bound", "cookbook/metric-series: the 90 days ending on :day, none after it", edit("me.day <= :day", "1")},
	{"facts", "metric-series includes the 91st day", "cookbook/metric-series: the 90 days ending on :day, none after it", edit("me.day > date(", "me.day >= date(")},
	{"named", "a promotion does not revive a tombstoned page", "a tombstoned ghost is promoted and revived", edit(", deleted_at = NULL   -- revives", "   -- revives")},
	{"dates", "entities.updated_at may lack milliseconds", "entities.updated_at without milliseconds refused", nocheck("entities_updated_at")},
	{"dates", "a tombstone need not be an instant", "a tombstone that is not an instant refused", nocheck("entities_deleted_at")},
	{"dates", "links.created_at need not be an instant", "links.created_at must be an instant", nocheck("links_created_at")},
	{"dates", "measurements.created_at need not be an instant", "measurements.created_at must be an instant", nocheck("measurements_created_at")},
	{"dates", "people.death_day need not round-trip", "people.death_day must round-trip", nocheck("people_death_day")},
	{"named", "a death may precede the birth", "a death before the birth is refused", nocheck("people_death_day_order")},
	{"identity", "links.source may be anything", "links.source takes the same GLOB as entities.source", nocheck("links_source")},
	{"pages", "a title key may hold an ASCII capital", "a non-ASCII title's key may not hold an ASCII capital", nocheck("entity_names_key_folded")},
	{"pages", "a title may have leading or trailing space", "a title with leading space refused (a key the writer folded to match)", edit("CHECK (title = trim(title) AND length(title) >= 1", "CHECK (length(title) >= 1")},
	{"pages", "a title may hold a NUL byte", "a title with a NUL byte refused", edit("         AND instr(title, char(0)) = 0\n", "")},
	{"facts", "a correction of a row that does not exist passes with foreign_keys=OFF", "with foreign_keys=OFF, a correction of a row that does not exist is refused by the trigger", edit("   WHERE (SELECT metric_id FROM measurements WHERE id = NEW.supersedes_id) IS NOT NEW.metric_id;", "   WHERE (SELECT metric_id FROM measurements WHERE id = NEW.supersedes_id) <> NEW.metric_id;")},
	{"links", "an unknown endpoint id passes as a page with foreign_keys=OFF", "with foreign_keys=OFF, a typed link to an id that does not exist is refused", edit("coalesce((SELECT entity_type FROM entities WHERE id = NEW.to_id), '?')", "coalesce((SELECT entity_type FROM entities WHERE id = NEW.to_id), 'page')")},
	{"named", "ghost_pages lists a page younger than 30 days", "ghost_pages leaves a page younger than 30 days alone", edit("     AND e.created_at < strftime('%Y-%m-%dT%H:%M:%fZ','now','-30 day')\n", "")},
	{"named", "ghost_pages lists a tombstoned page", "ghost_pages leaves a tombstoned page alone", edit("   WHERE e.entity_type = 'page' AND e.body = '' AND e.deleted_at IS NULL", "   WHERE e.entity_type = 'page' AND e.body = ''")},
	{"facts", "cookbook/mood-over-time shows superseded readings", "cookbook/mood-over-time skips superseded and retracted readings", edit("FROM measurement_values me\n  JOIN entities e ON e.id = me.metric_id AND e.deleted_at IS NULL\n  JOIN entity_names n ON n.entity_id = me.metric_id AND n.name_key = 'mood'", "FROM measurements me\n  JOIN entities e ON e.id = me.metric_id AND e.deleted_at IS NULL\n  JOIN entity_names n ON n.entity_id = me.metric_id AND n.name_key = 'mood'")},
	{"facts", "cookbook/metric-series shows superseded readings", "cookbook/metric-series skips superseded and retracted readings", edit("FROM measurement_values me\n  JOIN entities e ON e.id = me.metric_id AND e.deleted_at IS NULL\n  JOIN entity_names n ON n.entity_id = me.metric_id AND n.name_key = 'weight'", "FROM measurements me\n  JOIN entities e ON e.id = me.metric_id AND e.deleted_at IS NULL\n  JOIN entity_names n ON n.entity_id = me.metric_id AND n.name_key = 'weight'")},
	{"facts", "measurements may be deleted", "DELETE refused", notrigger("measurements_no_delete")},
	{"writers", "the DDL does not mark the file as Aminte", "the DDL marks the file as Aminte: application_id 0x4C494645 and user_version 1", edit("PRAGMA application_id = 0x4C494645;", "PRAGMA application_id = 0;")},
	{"pages", "a title can change", "an owned name key cannot change", notrigger("entity_names_fixed")},
	{"identity", "entities may be hard-deleted", "with foreign_keys=OFF: DELETE FROM entities still refused (the trigger, not the FK)", notrigger("entities_no_delete")},
	{"identity", "a link's endpoints can change", "a link's endpoints cannot change", notrigger("links_fixed")},
	{"identity", "editing a person does not bump updated_at", "updating a person bumps entities.updated_at", notrigger("people_touch")},
	{"identity", "measurements.source may be anything", "source  rejected on entities and measurements", nocheck("measurements_source")},
	{"identity", "entities.entity_type may be anything", "entities.entity_type rejects an unknown type", nocheck("entities_entity_type")},
	{"facts", "a metrics row may name a page that is not a metric", "...and cannot claim another type to hang off a person's page", nocheck("metrics_entity_type")},
	{"facts", "a metric's daily mark takes any number", "metrics.daily takes only 0 or 1", nocheck("metrics_daily")},
	{"facts", "a metric's unit can change", "metrics.unit cannot change", notrigger("metrics_unit_fixed")},
	{"facts", "a reading may be corrected twice", "a second correction of the same row is refused", edit("CREATE UNIQUE INDEX measurements_one_correction", "CREATE INDEX measurements_one_correction")},
	{"facts", "a measurement may be imported twice", "a duplicate measurement import is refused by its unique index", edit("CREATE UNIQUE INDEX measurements_import", "CREATE INDEX measurements_import")},
	{"facts", "a reading may supersede itself", "a row cannot supersede itself", nocheck("measurements_not_self")},
	{"facts", "a first reading may have no value", "a first reading with NULL value is refused", nocheck("measurements_first_has_value")},
	{"facts", "a metrics row may hang off a page that is not a metric", "a metrics row needs a page of type metric", edit("  FOREIGN KEY (id, entity_type) REFERENCES entities(id, entity_type)\n) STRICT;\nCREATE TRIGGER metrics_unit_fixed", "  FOREIGN KEY (id) REFERENCES entities(id)\n) STRICT;\nCREATE TRIGGER metrics_unit_fixed")},
	{"identity", "metrics may be hard-deleted", "DELETE FROM metrics is refused", notrigger("metrics_no_delete")},
	{"integrity", "the orphan query forgets metrics", "a metric with an owned name but no extension: only the orphan query sees it", edit(" OR (e.entity_type='metric' AND NOT EXISTS (SELECT 1 FROM metrics m WHERE m.id=e.id))", "")},
	{"facts", "a category may be a person", "...but a category is a plain page: never a person or a metric", edit("  ('part-of',  NULL,        'page',", "  ('part-of',  NULL,        'page,person',")},
	{"facts", "cookbook/metrics-by-category walks no deeper than the category itself", "cookbook/metrics-by-category: a category holds the metrics of every category under it", edit("  SELECT l.from_id FROM links l JOIN tree t ON l.to_id = t.id AND l.kind = 'part-of'\n", "  SELECT l.from_id FROM links l JOIN tree t ON l.to_id = t.id AND l.kind = 'part-of' AND 0\n")},
	{"facts", "cookbook/metrics-by-category files metrics in a deleted category", "cookbook/metrics-by-category: a deleted category's metrics read as filed nowhere", edit("FROM links l JOIN entities e ON e.id = l.to_id AND e.deleted_at IS NULL", "FROM links l JOIN entities e ON e.id = l.to_id")},
	{"facts", "measurement_values shows retractions", "the tap and its retraction are hidden", edit("   WHERE me.value IS NOT NULL\n     AND NOT EXISTS", "   WHERE NOT EXISTS")},
	{"dates", "measurements.tz may be anything", "measurements.tz \"\" rejected", nocheck("measurements_tz")},
	{"dates", "measurements.day is checked with = instead of IS", "exact measurements.day \"2026-13-01\" accepted=false", edit("CONSTRAINT measurements_day CHECK (length(day) = 10 AND day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(day) IS day)", "CONSTRAINT measurements_day CHECK (length(day) = 10 AND day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(day) = day)")},
	{"dates", "people.birth_day is checked with = instead of IS", "exact people.birth_day \"2026-13-01\" accepted=false", edit("date(birth_day) IS birth_day", "date(birth_day) = birth_day")},
	{"imports", "measurements.taken_at may be anything", "without NULLIF an empty taken_at ('' is not NULL) fails its CHECK", nocheck("measurements_taken_at")},
	{"pages", "a pure-ASCII title may have any key", "ASCII title with a wrong key rejected", nocheck("entity_names_key_ascii")},
	{"pages", "a title may be 400 bytes", "241 bytes rejected (é × 121 = 242 bytes)", edit("length(CAST(title AS BLOB)) <= 240", "length(CAST(title AS BLOB)) <= 400")},
	{"links", "an unregistered kind passes the trigger", "with foreign_keys=OFF an unregistered kind is still refused (the trigger, in autocommit)", edit("   WHERE NOT EXISTS (SELECT 1 FROM link_kinds k WHERE k.kind = NEW.kind);", "   WHERE 0 AND NOT EXISTS (SELECT 1 FROM link_kinds k WHERE k.kind = NEW.kind);")},
	{"links", "a link kind may be named in upper case", "a kind name in upper case is refused", nocheck("link_kinds_kind")},
	{"links", "from_types may be malformed", "a malformed type list is refused", nocheck("link_kinds_from_types")},
	{"writers", "the DDL does not set WAL", "journal_mode=WAL is stored in the file by the DDL", edit("PRAGMA journal_mode  = WAL;", "PRAGMA journal_mode  = DELETE;")},
	{"writers", "contract/connections lets readers keep trusted_schema on", "contract/connections: readers are read-only and set trusted_schema = OFF", edit("Readers must be **read-only** and set **`PRAGMA trusted_schema = OFF`** per connection, as writers do.", "Readers must be **read-only**.")},
	{"pages", "the Cn rule names a Unicode version the writer does not pin", "the Cn rule names one Unicode version, the one the writer pins", edit("**Unicode 15.0** has not", "**Unicode 16.0** has not")},
	{"pages", "the Cn rule accepts a code point Unicode 15.0 has not assigned", "the Cn rule lists code points a writer refuses and accepts", edit("and `U+1FAE9` (16.0) in a title, and accepts `U+1FAE8` (15.0)", "in a title, and accepts `U+1FAE8` (15.0) and `U+1FAE9` (16.0)")},
	{"files", "files.sha256 may be anything", "sha256 AAAAAAAAAAAA… refused", nocheck("files_sha256")},
	{"files", "files.mime may be anything", "mime Image/JPEG refused", nocheck("files_mime")},
	{"files", "files_preview compares with = and lets an empty preview through", "preview: an empty blob refused", edit("substr(preview, 1, 3) IS x'FFD8FF'", "substr(preview, 1, 3) = x'FFD8FF'")},
	{"files", "a preview need not be a JPEG", "preview: a PNG refused", edit("(substr(preview, 1, 3) IS x'FFD8FF'\n", "(1\n")},
	{"files", "a preview may be 2 MB", "preview: 1 MB accepted, 1 MB and a byte refused", edit("AND length(preview) <= 1048576", "AND length(preview) <= 2097152")},
	{"files", "one original may be kept twice", "one page per original: a second files row with the same sha256 is refused (files_sha256)", edit("CREATE UNIQUE INDEX files_sha256", "CREATE INDEX files_sha256")},
	{"files", "the hash and type of an original can change", "files.sha256 cannot change", notrigger("files_original_fixed")},
	{"files", "adding a preview does not bump updated_at", "adding it bumps entities.updated_at (files_touch)", notrigger("files_touch")},
	{"files", "a files row may hang off a page that is not a file", "a files row needs a page of type file", edit("  FOREIGN KEY (id, entity_type) REFERENCES entities(id, entity_type)\n) STRICT;\nCREATE UNIQUE INDEX files_sha256", "  FOREIGN KEY (id) REFERENCES entities(id)\n) STRICT;\nCREATE UNIQUE INDEX files_sha256")},
	{"files", "a files row may claim another type", "...and cannot claim another type to hang off a person's page", nocheck("files_entity_type")},
	{"files", "an embed cannot land on a file page", "an embed in a day page lands on the file page (a wikilink)", edit("('wikilink', 'page,person,metric,file', 'page,person,metric,file',", "('wikilink', 'page,person,metric,file', 'page,person,metric',")},
	{"files", "cookbook/keep-a-file finds an original only under its own source", "the same original again, from another source: step 0 finds the page, and nothing is written", edit(" WHERE f.sha256 = :sha256;", " WHERE f.sha256 = :sha256 AND e.source = :source;")},
	{"files", "cookbook/keep-a-file replaces a preview a file already has", "...and an existing preview is never replaced", edit("WHERE sha256 = :sha256 AND preview IS NULL AND :preview IS NOT NULL", "WHERE sha256 = :sha256 AND :preview IS NOT NULL")},
	{"files", "cookbook/keep-a-file fills the preview of a tombstoned file", "a tombstoned file is left alone: no preview is filled", edit("\n   AND id IN (SELECT id FROM entities WHERE deleted_at IS NULL);", ";")},
	{"files", "cookbook/keep-a-file leaves a promoted page without its missing day", "promotion missing day: recipe and writer agree, filling only a missing day", edit("UPDATE entities SET day = :day WHERE id = :file_id AND entity_type = 'file' AND day IS NULL;", "UPDATE entities SET day = day WHERE id = :file_id AND entity_type = 'file' AND day IS NULL;")},
	{"files", "cookbook/keep-a-file overwrites the text of the page it promotes", "a page with text becomes the file with its text kept; :body fills only an empty body", edit("WHERE id = :file_id AND entity_type = 'file' AND body = '';", "WHERE id = :file_id AND entity_type = 'file';")},
	{"identity", "files may be hard-deleted", "DELETE FROM files is refused", notrigger("files_no_delete")},
	{"integrity", "the orphan query forgets files", "a file with an owned name but no extension: only the orphan query sees it", edit(" OR (e.entity_type='file' AND NOT EXISTS (SELECT 1 FROM files f WHERE f.id=e.id))", "")},
	{"integrity", "the orphan query forgets the preferred owner", "missing preferred ownership is detected by FK and semantic checks, not structure", edit("WHERE NOT EXISTS (SELECT 1 FROM entity_names n WHERE n.entity_id=e.id AND n.name_key=e.preferred_name_key) OR (e.entity_type='person'", "WHERE 0 OR (e.entity_type='person'")},
	{"integrity", "the orphan query forgets who owns the preferred name", "a preferred key that another entity owns is an orphan: the orphan query asks who owns the name", edit("WHERE n.entity_id=e.id AND n.name_key=e.preferred_name_key) OR (e.entity_type='person'", "WHERE n.name_key=e.preferred_name_key) OR (e.entity_type='person'")},
	{"integrity", "the link-rule query forgets a part-of link to a day page", "the link-rule query detects a part-of link to or from a day page, while the registry query sees nothing", edit("WHERE (l.kind='part-of' AND coalesce(t.is_journal,0))\n OR (l.kind='part-of' AND coalesce(f.is_journal,0))", "WHERE 0\n OR (l.kind='part-of' AND coalesce(f.is_journal,0))")},
	{"integrity", "the scale query forgets the range", "the scale query detects a value above the range, a fraction and a value below it", edit("OR m.value NOT BETWEEN", "OR 0 AND m.value NOT BETWEEN")},
	{"doc-save-contract", "an embed makes no link", "A every printed vector reproduces with the writer's extraction", edit("| `![[Lake.jpg]] and ![[Lake.jpg\\|a lake]]` | `Lake.jpg` |", "| `![[Lake.jpg]] and ![[Lake.jpg\\|a lake]]` | — |")},
	{"doc-save-contract", "cookbook/save-a-body lets a save go on after a fault of SAVEPOINT, ROLLBACK TO or RELEASE", "D cookbook/save-a-body says that an error of SAVEPOINT, ROLLBACK TO or RELEASE fails the whole save", edit("of `RELEASE` fails the whole save", "of `RELEASE` skips the target")},
	{"schema-safeguards", "entities_import_key NUL guard omitted", "entities_import_key refuses an embedded NUL", edit("CONSTRAINT entities_import_key CHECK (import_key IS NULL OR (instr(import_key, char(0)) = 0 AND ", "CONSTRAINT entities_import_key CHECK (import_key IS NULL OR (")},
	{"schema-safeguards", "entities_import_key lets the empty key through", "entities_import_key refuses the empty key", edit("CONSTRAINT entities_import_key CHECK (import_key IS NULL OR (instr(import_key, char(0)) = 0 AND length(CAST(import_key AS BLOB)) BETWEEN 1 AND 512", "CONSTRAINT entities_import_key CHECK (import_key IS NULL OR (instr(import_key, char(0)) = 0 AND length(CAST(import_key AS BLOB)) BETWEEN 0 AND 512")},
	{"schema-safeguards", "entities_import_key has no upper bound", "entities_import_key refuses 513 ASCII bytes", edit("CONSTRAINT entities_import_key CHECK (import_key IS NULL OR (instr(import_key, char(0)) = 0 AND length(CAST(import_key AS BLOB)) BETWEEN 1 AND 512", "CONSTRAINT entities_import_key CHECK (import_key IS NULL OR (instr(import_key, char(0)) = 0 AND length(CAST(import_key AS BLOB)) >= 1")},
	{"schema-safeguards", "entities_import_key counts characters, not bytes", "entities_import_key refuses 514 bytes of two-byte characters", edit("CONSTRAINT entities_import_key CHECK (import_key IS NULL OR (instr(import_key, char(0)) = 0 AND length(CAST(import_key AS BLOB)) BETWEEN 1 AND 512", "CONSTRAINT entities_import_key CHECK (import_key IS NULL OR (instr(import_key, char(0)) = 0 AND length(import_key) BETWEEN 1 AND 512")},
	{"schema-safeguards", "measurements_import_key NUL guard omitted", "measurements_import_key refuses an embedded NUL", edit("CONSTRAINT measurements_import_key CHECK (import_key IS NULL OR (instr(import_key, char(0)) = 0 AND ", "CONSTRAINT measurements_import_key CHECK (import_key IS NULL OR (")},
	{"schema-safeguards", "measurements_import_key lets the empty key through", "measurements_import_key refuses the empty key", edit("CONSTRAINT measurements_import_key CHECK (import_key IS NULL OR (instr(import_key, char(0)) = 0 AND length(CAST(import_key AS BLOB)) BETWEEN 1 AND 512", "CONSTRAINT measurements_import_key CHECK (import_key IS NULL OR (instr(import_key, char(0)) = 0 AND length(CAST(import_key AS BLOB)) BETWEEN 0 AND 512")},
	{"schema-safeguards", "measurements_import_key has no upper bound", "measurements_import_key refuses 513 ASCII bytes", edit("CONSTRAINT measurements_import_key CHECK (import_key IS NULL OR (instr(import_key, char(0)) = 0 AND length(CAST(import_key AS BLOB)) BETWEEN 1 AND 512", "CONSTRAINT measurements_import_key CHECK (import_key IS NULL OR (instr(import_key, char(0)) = 0 AND length(CAST(import_key AS BLOB)) >= 1")},
	{"schema-safeguards", "measurements_import_key counts characters, not bytes", "measurements_import_key refuses 514 bytes of two-byte characters", edit("CONSTRAINT measurements_import_key CHECK (import_key IS NULL OR (instr(import_key, char(0)) = 0 AND length(CAST(import_key AS BLOB)) BETWEEN 1 AND 512", "CONSTRAINT measurements_import_key CHECK (import_key IS NULL OR (instr(import_key, char(0)) = 0 AND length(import_key) BETWEEN 1 AND 512")},
	{"schema-safeguards", "capture_keys_import_key NUL guard omitted", "capture_keys_import_key refuses an embedded NUL", edit("CONSTRAINT capture_keys_import_key CHECK (instr(import_key, char(0)) = 0 AND ", "CONSTRAINT capture_keys_import_key CHECK (")},
	{"schema-safeguards", "capture_keys_import_key lets the empty key through", "capture_keys_import_key refuses the empty key", edit("CONSTRAINT capture_keys_import_key CHECK (instr(import_key, char(0)) = 0 AND length(CAST(import_key AS BLOB)) BETWEEN 1 AND 512", "CONSTRAINT capture_keys_import_key CHECK (instr(import_key, char(0)) = 0 AND length(CAST(import_key AS BLOB)) BETWEEN 0 AND 512")},
	{"schema-safeguards", "capture_keys_import_key has no upper bound", "capture_keys_import_key refuses 513 ASCII bytes", edit("CONSTRAINT capture_keys_import_key CHECK (instr(import_key, char(0)) = 0 AND length(CAST(import_key AS BLOB)) BETWEEN 1 AND 512", "CONSTRAINT capture_keys_import_key CHECK (instr(import_key, char(0)) = 0 AND length(CAST(import_key AS BLOB)) >= 1")},
	{"schema-safeguards", "capture_keys_import_key counts characters, not bytes", "capture_keys_import_key refuses 514 bytes of two-byte characters", edit("CONSTRAINT capture_keys_import_key CHECK (instr(import_key, char(0)) = 0 AND length(CAST(import_key AS BLOB)) BETWEEN 1 AND 512", "CONSTRAINT capture_keys_import_key CHECK (instr(import_key, char(0)) = 0 AND length(import_key) BETWEEN 1 AND 512")},
	{"journal", "capture_keys.source may be anything", "capture_keys refuses a source that is not a writer's name (capture_keys_source)", nocheck("capture_keys_source")},
	{"dates", "capture_keys.recorded_at need not be an instant", "exact capture_keys.recorded_at \"2026-01-01T24:59:59.000Z\" accepted=false", nocheck("capture_keys_recorded_at")},
	{"journal", "a capture key is unique across sources, not per source", "...and one key under another source is another row", edit("  UNIQUE (source, import_key)\n) STRICT;", "  UNIQUE (import_key)\n) STRICT;")},
	{"journal", "a capture key may name a second page", "...even for another page: the key names one capture", edit("  UNIQUE (source, import_key)\n) STRICT;", "  UNIQUE (source, import_key, page_id)\n) STRICT;")},
	{"journal", "a capture key may name a page that does not exist", "capture_keys refuses a key for a page that does not exist", edit("page_id     INTEGER NOT NULL REFERENCES entities(id),", "page_id     INTEGER NOT NULL,")},
	{"journal", "a capture key may be changed", "an UPDATE of a capture key is refused (capture_keys_no_update)", notrigger("capture_keys_no_update")},
	{"journal", "a capture key may be deleted", "a DELETE of a capture key is refused (capture_keys_no_delete)", notrigger("capture_keys_no_delete")},
	{"journal", "cookbook/capture records the Mood of a keyed capture without its key", "...the Mood reading of that capture carries the same key", edit("SELECT e.id, '2026-09-29', 4, 'ui', :import_key, :page_id,", "SELECT e.id, '2026-09-29', 4, 'ui', NULL, :page_id,")},
	{"schema-safeguards", "people_name NUL guard omitted", "people_name refuses an embedded NUL", edit("CONSTRAINT people_name CHECK (instr(name, char(0)) = 0 AND ", "CONSTRAINT people_name CHECK (")},
	{"schema-safeguards", "people_name allows padding", "people_name refuses a leading space", edit(" AND name = trim(name)\n                           AND length(CAST(name AS BLOB))", " AND length(CAST(name AS BLOB))")},
	{"schema-safeguards", "people_name allows the empty name", "people_name refuses the empty name", edit("length(CAST(name AS BLOB)) BETWEEN 1 AND 240", "length(CAST(name AS BLOB)) BETWEEN 0 AND 240")},
	{"schema-safeguards", "people_name has no upper bound", "people_name refuses 241 ASCII bytes", edit("length(CAST(name AS BLOB)) BETWEEN 1 AND 240", "length(CAST(name AS BLOB)) >= 1")},
	{"schema-safeguards", "people_name counts characters, not bytes", "people_name refuses 242 bytes of two-byte characters", edit("length(CAST(name AS BLOB)) BETWEEN 1 AND 240", "length(name) BETWEEN 1 AND 240")},
	{"schema-safeguards", "metrics_unit NUL guard omitted", "metrics_unit refuses an embedded NUL", edit("CONSTRAINT metrics_unit CHECK (instr(unit, char(0)) = 0 AND ", "CONSTRAINT metrics_unit CHECK (")},
	{"schema-safeguards", "metrics_unit allows padding", "metrics_unit refuses a leading space", edit(" AND unit = trim(unit)\n                           AND length(CAST(unit AS BLOB))", " AND length(CAST(unit AS BLOB))")},
	{"schema-safeguards", "metrics_unit has no upper bound", "metrics_unit refuses 33 ASCII bytes", edit("length(CAST(unit AS BLOB)) <= 32", "length(CAST(unit AS BLOB)) >= 0")},
	{"schema-safeguards", "metrics_unit counts characters, not bytes", "metrics_unit refuses 34 bytes of two-byte characters", edit("length(CAST(unit AS BLOB)) <= 32", "length(unit) <= 32")},
	{"schema-safeguards", "links_note NUL guard omitted", "links_note refuses an embedded NUL", nocheck("links_note")},
	{"schema-safeguards", "link_kinds_note NUL guard omitted", "link_kinds_note refuses an embedded NUL", nocheck("link_kinds_note")},
	{"schema-safeguards", "entities tombstone may precede creation", "entities_deleted_after_created refuses a tombstone before creation", nocheck("entities_deleted_after_created")},
	{"schema-safeguards", "entities tombstone must be strictly later", "entities_deleted_after_created accepts a tombstone at the creation instant", edit("CONSTRAINT entities_deleted_after_created CHECK (deleted_at IS NULL OR deleted_at >= created_at)", "CONSTRAINT entities_deleted_after_created CHECK (deleted_at IS NULL OR deleted_at > created_at)")},
	{"schema-safeguards", "entities.body moves in front of the small columns", "entities.body is the last column, behind every small column", edit("  import_key  TEXT CONSTRAINT entities_import_key CHECK (import_key IS NULL OR (instr(import_key, char(0)) = 0 AND length(CAST(import_key AS BLOB)) BETWEEN 1 AND 512)),   -- the sender's key, unique per source; NULL = sent once. Imported or not: source\n  body        TEXT NOT NULL DEFAULT '',   -- CommonMark, including journal and file text\n", "  body        TEXT NOT NULL DEFAULT '',   -- CommonMark, including journal and file text\n  import_key  TEXT CONSTRAINT entities_import_key CHECK (import_key IS NULL OR (instr(import_key, char(0)) = 0 AND length(CAST(import_key AS BLOB)) BETWEEN 1 AND 512)),   -- the sender's key, unique per source; NULL = sent once. Imported or not: source\n")},
	{"schema-safeguards", "the Mood metrics row is seeded after the commit", "a failing last seed statement leaves no half-seeded Mood entity or name", edit("VALUES (1, 'Mood', 'mood');\nINSERT INTO metrics(id, unit) VALUES (1, '1-5');\nCOMMIT;", "VALUES (1, 'Mood', 'mood');\nCOMMIT;\nINSERT INTO metrics(id, unit) VALUES (1, '1-5');")},
	{"measurement-query-plan", "links_to loses its kind column", "links_to serves the backlinks of one kind by both columns", edit("CREATE INDEX links_to ON links(to_id, kind);", "CREATE INDEX links_to ON links(to_id);")},
	{"measurement-query-plan", "links_to omitted", "links_to still serves the backlinks of every kind", edit("CREATE INDEX links_to ON links(to_id, kind);   -- backlinks, all or of one kind (from_id is served by the UNIQUE index)\n", "")},
	{"measurement-query-plan", "entities_day holds pages without a day", "entities_day holds only pages with a day: a lookup of pages without one does not use it", edit("CREATE INDEX entities_day ON entities(day) WHERE day IS NOT NULL;", "CREATE INDEX entities_day ON entities(day);")},
	{"measurement-query-plan", "entities_day omitted", "entities_day serves a lookup by day", edit("CREATE INDEX entities_day ON entities(day) WHERE day IS NOT NULL;   -- day view; a link-target page has no day\n", "")},
}

var (
	baselineMu sync.Mutex
	baseline   = map[string]string{}
)

// clean runs a suite on the unmutated document once: "" when every expectation is met.
func clean(t *testing.T, suite string) string {
	baselineMu.Lock()
	defer baselineMu.Unlock()
	if r, ok := baseline[suite]; ok {
		return r
	}
	s, stopped := runSuite(suite, realDocs(), t.TempDir())
	r := ""
	if stopped != "" || s.ok != s.n || s.n == 0 {
		r = fmt.Sprintf("%d/%d: %v", s.ok, s.n, s.fails)
	}
	baseline[suite] = r
	return r
}

// TestMutants runs every mutant against the suite that owns its rule (skipped with -short).
func TestMutants(t *testing.T) {
	if testing.Short() {
		t.Skip("mutants: skipped with -short")
	}
	for _, m := range mutants {
		if m.witness == "" {
			t.Fatalf("%s: missing intended assertion witness", m.name)
		}
		if r := clean(t, m.suite); r != "" {
			t.Fatalf("%s must pass on the unmutated document first: %s", m.suite, r)
		}
	}
	for i, m := range mutants {
		t.Run(fmt.Sprintf("%03d %s: %s", i+1, m.suite, m.name), func(t *testing.T) {
			t.Parallel()
			over, err := m.change.apply(realDocs())
			if err != nil {
				t.Fatal(err)
			}
			s, stopped := runSuite(m.suite, realDocs().with(over), t.TempDir())
			if !mutantKilled(s, stopped, m.witness) {
				how := fmt.Sprintf("intended assertion %q did not fail; failed labels: %q", m.witness, s.failedLabels)
				if stopped != "" {
					how = "stopped only: " + clip(stopped, 120)
				}
				t.Errorf("MISSED: %s did not notice %q (%d/%d; %s)", m.suite, m.name, s.ok, s.n, how)
			}
		})
	}
	t.Logf("mutants: %d", len(mutants))
}

// mutantKilled matches the exact expectation label, never formatted diagnostics.
// A setup stop invalidates even an earlier intended failure.
func mutantKilled(s *S, stopped, witness string) bool {
	return witness != "" && stopped == "" && slices.Contains(s.failedLabels, witness)
}

func TestMutantWitness(t *testing.T) {
	t.Run("diagnostics are not identities", func(t *testing.T) {
		s := &S{}
		s.K("unrelated", false, "intended")
		if mutantKilled(s, "", "intended") || !slices.Contains(s.failedLabels, "unrelated") {
			t.Fatal("formatted details credited as an assertion identity")
		}
	})
	for _, tc := range []struct {
		name             string
		failures         []string
		stopped, witness string
		want             bool
	}{
		{"unrelated failure", []string{"unrelated"}, "", "intended", false},
		{"intended failure", []string{"intended"}, "", "intended", true},
		{"stopped suite", []string{"intended", "other"}, "setup failed", "intended", false},
		{"missing witness", []string{"intended"}, "", "", false},
		{"no failure", nil, "", "intended", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &S{n: len(tc.failures), failedLabels: tc.failures}
			if got := mutantKilled(s, tc.stopped, tc.witness); got != tc.want {
				t.Errorf("kill=%v want %v", got, tc.want)
			}
		})
	}
	t.Run("unchanged mutation", func(t *testing.T) {
		over, err := edit("CONSTRAINT entities_day CHECK", "CONSTRAINT entities_day CHECK").apply(realDocs())
		if err != nil {
			t.Fatal(err)
		}
		s, stopped := runSuite("dates", realDocs().with(over), t.TempDir())
		if stopped != "" || s.ok != s.n || s.n == 0 {
			t.Fatalf("no-op baseline: %d/%d stopped=%s failures=%v", s.ok, s.n, stopped, s.fails)
		}
		if mutantKilled(s, stopped, `entities.day rejects "banana"`) {
			t.Fatal("unchanged mutation credited")
		}
	})
}

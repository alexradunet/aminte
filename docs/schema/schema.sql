-- ============================================================
-- Aminte schema v1: the single init file, edited in place until the freeze;
-- numbered migrations begin only after the freeze (D13).
-- Each table's rules are comments INSIDE its CREATE statement, so .schema shows them (a comment
-- outside a statement is not stored in the file); the rules that span tables: SELECT * FROM aminte_meta;
-- Every writer connection: SQLite >= 3.51.3; PRAGMA foreign_keys = ON;
-- PRAGMA recursive_triggers = ON; PRAGMA synchronous = FULL; PRAGMA trusted_schema = OFF;
-- and every write transaction starts with BEGIN IMMEDIATE (docs/contract/connections.md).
-- ============================================================
PRAGMA application_id = 0x4C494645;   -- 'LIFE' — recognizable to file(1) and tools
PRAGMA user_version  = 1;
PRAGMA journal_mode  = WAL;           -- persistent; readers (Datasette) don't block the writer

CREATE TABLE aminte_meta (
  -- the rules that span tables, readable with a SELECT by someone who has only this file;
  -- the rules of one table are comments inside its own CREATE statement
  -- rows are rules the owner adds or removes; deleting one removes the rule from the file itself
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
) STRICT;
INSERT INTO aminte_meta(key, value) VALUES
  ('schema',    'aminte v1: the journal (one page per day), wiki, people, health metrics and the files kept as their text and a small picture, of one person: a life log, not a project manager; the rules of each table are comments inside its CREATE statement (.schema), the rules that span tables are these rows; this is an in-file reading summary: a conforming writer also needs the matching docs contract and vectors'),
  ('instants',  'every *_at column is an exact 24-character ASCII UTC ISO-8601 TEXT instant, years 0000..9999 and hours 00..23, with milliseconds, e.g. 2026-06-09T21:14:03.482Z, written by the app, never by a SQLite default (CURRENT_TIMESTAMP has no milliseconds and is not ISO-8601; a trigger writes strftime(''%Y-%m-%dT%H:%M:%fZ'',''now'')); fixed width, so it sorts as text; supplied clock timestamps may coincide or regress and do not encode commit order; CHECK strftime(''%Y-%m-%dT%H:%M:%fZ'', x) IS x; created_at, on every table that has it, is when the row was written to life.db, never back-dated'),
  ('days',      'every *_day column (and day) is the LOCAL exact 10-character ASCII calendar date YYYY-MM-DD, years 0000..9999, where the thing happened for recorded facts, written by the app: the local calendar day of the device that captured it when the client names the date, and the local day of the writer''s own clock and zone when the client sends the word today or no day; never recomputed from an instant; CHECK explicit ASCII shape and date(x) IS x (IS, not =: a CHECK passes on NULL, and date(''2026-9-3'') is NULL)'),
  ('periods',   'a period page has a canonical key like a day page: a week 2026-W43 (ISO 8601: Monday first, week 1 holds 4 January), a month 2026-10, a quarter 2026-Q4 or a year 2026; its key is reserved, it has no alias, no rename and no day (day is NULL), it is never filed and never a category, and entities.period names its shape (D30)'),
  ('deletes',   'life data is never deleted except links rows: an entity is a tombstone (entities.deleted_at); revival clears the tombstone, so lifecycle transitions are not retained history; a measurement is corrected by inserting a row; BEFORE DELETE triggers enforce it on entities and every domain row; ordinary active reads filter lifecycle tombstones; explicit historical and append-only audits retain them; the registries (link_kinds, aminte_meta) are the owner''s administrative rows, deletable while nothing references them (each CREATE comment says so)'),
  ('source',    'entities, links and measurements: source names the original inserter of the row, not the last editor or an audit trail (ui, cli, api, agent:<name>, import:<name>; schema for the rows this file seeds); written at insert, never changed; import_key is unique per source on entities and on capture_keys, and per source and metric on measurements; a present import_key is 1 to 512 bytes without NUL'),
  ('edit_revisions', 'entities.revision is a monotonic integer edit token independent of updated_at; creation is not an edit (nothing advances a row whose creation transaction is its only write: it keeps revision 1 and the updated_at its insert wrote, its owned preferred name and typed detail row included); a later change of the row''s own content advances it: names, body, day, type, lifecycle, typed detail and the links it leaves (a link advances only the page it leaves, never the page it points at; a wikilink is derived from the body and advances nothing); no-ops do not, rollback restores it, exhaustion refuses; clients compare the token inside BEGIN IMMEDIATE, never a timestamp; exact increment counts are not meaningful'),
  ('typed_links', 'both endpoints of every retained link must satisfy link_kinds; a type change refuses invalid incoming or outgoing edges atomically; semantic integrity checks the registry independently of structural integrity'),
  ('writers',   'one writing application; every connection sets foreign_keys=ON, recursive_triggers=ON, synchronous=FULL, trusted_schema=OFF and starts write transactions with BEGIN IMMEDIATE; every other tool opens the file read-only; imports use INSERT ... ON CONFLICT DO NOTHING, never OR IGNORE (skips CHECK/NOT NULL violations silently) or OR REPLACE (a delete)'),
  ('sqlite',    'writers need SQLite >= 3.51.3 (fixes a WAL race between concurrent writers and checkpoints); migrations need >= 3.53 (ALTER TABLE ADD/DROP CONSTRAINT); CHECKs use only functions every such version has'),
  ('evolution', 'after the freeze (the first kept write that a rebuild would lose, which no program can write again from its source; before it a file is rebuilt, not migrated): numbered forward-only SQL migrations, additive only (new tables, columns and indexes; a named CHECK may be replaced with ALTER TABLE DROP/ADD CONSTRAINT, so every CHECK is named), counted in PRAGMA user_version; additive means data-preserving, not reader-compatible: renames can break readers and replaced CHECKs can break writers; check user_version and select explicit columns');

CREATE TABLE entities (
  -- One named identity and prose body per linkable thing (page, person, metric, file).
  -- Creation time is immutable evidence of initial recording (entities_provenance_fixed).
  -- Insert this row first with RETURNING id, then its owned preferred entity_names row in the same
  -- BEGIN IMMEDIATE transaction; preferred ownership is NOT NULL and checked by the deferred composite FK.
  -- Never carry last_insert_rowid() across statements. Typed extension rows use this same id and
  -- (id, entity_type) FK directly. Promotion changes type, not identity.
  -- A canonical date key is the one plain journal-day identity for that local day: its day equals its
  -- preferred key, neither may transfer, and its only registry spelling is that date. Other objects
  -- may have a day without being journal days. Their aliases and preferred names remain editable.
  -- A period page is the same kind of identity for a longer span (D30): its preferred key is a week (2026-W43), a
  -- month (2026-10), a quarter (2026-Q4) or a year (2026), written in the key as 2026-w43 and 2026-q4. Its day is
  -- NULL, it shares the identity rules of a day page, and it is the owner's horizon: the week, the month, the
  -- quarter or the year a review belongs to.
  -- Body saves synchronize wikilinks by resolved entity id (D19); invalid names do not block a save.
  -- Type changes refuse any incoming or outgoing link whose registered endpoint types would become invalid;
  -- no retained edge is silently removed. Nothing is ever deleted: deleted_at is the tombstone (D11), never
  -- earlier than created_at (entities_deleted_after_created).
  -- import_key: the key a writer that may send the row twice gives it (an importer, an offline phone, a
  -- retrying agent); whether the row was imported is source, not import_key. Unique per source, written at
  -- insert and never changed. Insert with ON CONFLICT(source, import_key) WHERE import_key IS NOT NULL
  -- DO NOTHING RETURNING id: no id back = imported before, so no domain row is inserted. A key is 1 to 512
  -- bytes without NUL (entities_import_key): an empty key would make every later row look imported before.
  -- Column order is part of the file: body, the one large text, is last, so the small columns before it
  -- (deleted_at, entity_type, ...) are read without walking the overflow pages of a long body.
  id          INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL CONSTRAINT entities_entity_type
                  CHECK (entity_type IN ('page','person','metric','file')),
  preferred_name_key TEXT NOT NULL,       -- selects exactly one owned registry row; spelling lives only there
  day         TEXT CONSTRAINT entities_day CHECK (day IS NULL OR length(day) = 10 AND day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(day) IS day),
  -- The one definition of "a journal day page" (D5): the preferred key is a canonical local date. The CHECKs and
  -- triggers on entities rows read this column, and so does the integrity SQL. entity_names.name_key has no such
  -- column: a rule about a NAME being date-shaped spells the predicate out.
  is_journal  INTEGER NOT NULL GENERATED ALWAYS AS (length(preferred_name_key) = 10 AND date(preferred_name_key) IS preferred_name_key) VIRTUAL,
  -- The one definition of "a period page" (D30): the folded key has the shape of a year, a month, a quarter or an
  -- ISO week. A key of ten characters never has these shapes, so a page is never both a day and a period.
  period      TEXT GENERATED ALWAYS AS (CASE
                WHEN preferred_name_key GLOB '[0-9][0-9][0-9][0-9]' THEN 'year'
                WHEN preferred_name_key GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]' THEN 'month'
                WHEN preferred_name_key GLOB '[0-9][0-9][0-9][0-9]-q[0-9]' THEN 'quarter'
                WHEN preferred_name_key GLOB '[0-9][0-9][0-9][0-9]-w[0-9][0-9]' THEN 'week' END) VIRTUAL,
  created_at  TEXT NOT NULL CONSTRAINT entities_created_at CHECK (length(created_at) = 24 AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z' AND substr(created_at,12,2) BETWEEN '00' AND '23' AND strftime('%Y-%m-%dT%H:%M:%fZ', created_at) IS created_at),   -- the write time (aminte_meta.instants)
  updated_at  TEXT NOT NULL CONSTRAINT entities_updated_at CHECK (length(updated_at) = 24 AND updated_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z' AND substr(updated_at,12,2) BETWEEN '00' AND '23' AND strftime('%Y-%m-%dT%H:%M:%fZ', updated_at) IS updated_at),   -- kept by the *_touch triggers
  revision    INTEGER NOT NULL DEFAULT 1 CONSTRAINT entities_revision CHECK (revision >= 1), -- edit token, independent of the clock; overflow refuses the write
  deleted_at  TEXT     CONSTRAINT entities_deleted_at CHECK (deleted_at IS NULL OR length(deleted_at) = 24 AND deleted_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z' AND substr(deleted_at,12,2) BETWEEN '00' AND '23' AND strftime('%Y-%m-%dT%H:%M:%fZ', deleted_at) IS deleted_at),   -- the tombstone
  source      TEXT NOT NULL CONSTRAINT entities_source CHECK (instr(source, char(0)) = 0 AND length(source) BETWEEN 1 AND 64 AND source NOT GLOB '*[^a-z0-9_:.-]*'),   -- the writer (aminte_meta.source)
  import_key  TEXT CONSTRAINT entities_import_key CHECK (import_key IS NULL OR (instr(import_key, char(0)) = 0 AND length(CAST(import_key AS BLOB)) BETWEEN 1 AND 512)),   -- the sender's key, unique per source; NULL = sent once. Imported or not: source
  body        TEXT NOT NULL DEFAULT '',   -- CommonMark, including journal and file text
  UNIQUE (id, entity_type),
  CONSTRAINT entities_day_page CHECK (NOT is_journal OR day IS preferred_name_key),
  CONSTRAINT entities_day_page_plain CHECK (NOT is_journal OR entity_type = 'page'),
  CONSTRAINT entities_period_page CHECK (period IS NULL OR (entity_type = 'page' AND day IS NULL)),
  -- the period exists: a month 01..12, a quarter 1..4, a week 01..52, and week 53 only in a year that has one (the
  -- year starts on a Thursday, or on a Wednesday and is a leap year); the Monday of week 53 is then on or before 28 December
  CONSTRAINT entities_period_exists CHECK (period IS NULL OR period = 'year'
    OR (period = 'month' AND date(preferred_name_key || '-01') IS preferred_name_key || '-01')
    OR (period = 'quarter' AND substr(preferred_name_key, 7, 1) BETWEEN '1' AND '4')
    OR (period = 'week' AND (substr(preferred_name_key, 7, 2) BETWEEN '01' AND '52'
        OR (substr(preferred_name_key, 7, 2) = '53'
            AND (strftime('%w', substr(preferred_name_key, 1, 4) || '-01-01') = '4'
              OR (strftime('%w', substr(preferred_name_key, 1, 4) || '-01-01') = '3'
                  AND date(substr(preferred_name_key, 1, 4) || '-02-29') IS substr(preferred_name_key, 1, 4) || '-02-29')))))),
  CONSTRAINT entities_deleted_after_created CHECK (deleted_at IS NULL OR deleted_at >= created_at),
  FOREIGN KEY (id, preferred_name_key) REFERENCES entity_names(entity_id, name_key) DEFERRABLE INITIALLY DEFERRED
) STRICT;
CREATE UNIQUE INDEX entities_import ON entities(source, import_key) WHERE import_key IS NOT NULL;

CREATE TABLE entity_names (
  -- The sole authoritative spellings: one preferred name selected by entities, plus direct retained aliases.
  -- Globally unique name_key = NFC(casefold(NFC(title))) is computed by the writer; SQLite verifies ASCII.
  -- Every name maps directly to one entity_id. Keys and owners never change and names are never deleted,
  -- including after tombstoning; rename is not merge, even into an empty ghost. A case-only spelling edit
  -- retains its key; selecting an already-owned alias creates no new registry identity. Old prose is not rewritten.
  -- Canonical journal dates are reserved to their matching plain owner and permit no additional aliases.
  -- Filename checks below are supplemented by the pinned Unicode/raw+NFC CommonMark addressability
  -- predicate in contract/titles-and-wikilinks.md. Display text after | registers no alias.
  id          INTEGER PRIMARY KEY,
  entity_id   INTEGER NOT NULL REFERENCES entities(id),
  title       TEXT NOT NULL,              -- preferred spelling or retained alias; case-only edits retain the key
  name_key    TEXT NOT NULL UNIQUE,       -- NFC(casefold(NFC(title))), computed by the writer
  UNIQUE (entity_id, name_key),
  CONSTRAINT entity_names_key_folded CHECK (instr(name_key, char(0)) = 0 AND length(name_key) >= 1 AND name_key = trim(name_key)
                               AND name_key NOT GLOB '*[A-Z]*'),      -- a folded key has no ASCII capitals
  CONSTRAINT entity_names_key_ascii CHECK (title GLOB '*[^ -~]*' OR name_key = lower(title)),   -- pure-ASCII titles: the DB verifies the key
  CONSTRAINT entity_names_title_len CHECK (title = trim(title) AND length(title) >= 1
                           AND length(CAST(title AS BLOB)) <= 240),  -- bytes: a filename limit is 255 bytes
  CONSTRAINT entity_names_title_safe CHECK (   -- a title must be a valid file name on Linux, macOS and Windows: keep it safe
         title NOT GLOB '*[/\:*?"<>|]*' AND instr(title, '[') = 0 AND instr(title, ']') = 0            -- path separators and Windows-reserved characters
         AND title NOT GLOB ('*[' || char(1) || '-' || char(31) || char(127) || '-' || char(159) || char(173) || char(1564)
                             || char(8203) || char(8206) || '-' || char(8207) || char(8234) || '-' || char(8238)
                             || char(8288) || '-' || char(8292) || char(8294) || '-' || char(8297) || char(65279) || ']*')
                                                   -- control characters (C0, DEL, C1) and invisible or bidi ones: soft hyphen, Arabic
                                                   -- letter mark, zero-width space, LRM/RLM, embeddings and overrides, word joiner and
                                                   -- invisible operators, isolates, BOM. ZWNJ/ZWJ (U+200C/D) stay: scripts and emoji need them
         AND instr(title, char(0)) = 0
         AND substr(title, 1, 1) <> '.' AND substr(title, -1) <> '.'   -- no hidden files, '..', trailing dot
         AND upper(CASE WHEN instr(title, '.') > 0 THEN substr(title, 1, instr(title, '.') - 1) ELSE title END)
             NOT IN ('CON','PRN','AUX','NUL',                -- Windows device names, bare or before an extension (CON.backup)
               'COM1','COM2','COM3','COM4','COM5','COM6','COM7','COM8','COM9','COM¹','COM²','COM³',
               'LPT1','LPT2','LPT3','LPT4','LPT5','LPT6','LPT7','LPT8','LPT9','LPT¹','LPT²','LPT³'))
) STRICT;
CREATE INDEX entities_day ON entities(day) WHERE day IS NOT NULL;   -- day view; a link-target page has no day
CREATE TRIGGER entity_names_fixed BEFORE UPDATE ON entity_names
 WHEN NEW.id IS NOT OLD.id OR NEW.entity_id IS NOT OLD.entity_id OR NEW.name_key IS NOT OLD.name_key
BEGIN SELECT RAISE(ABORT, 'name keys and ownership are immutable'); END;
CREATE TRIGGER entity_names_no_delete BEFORE DELETE ON entity_names
BEGIN SELECT RAISE(ABORT, 'names remain reserved, including tombstones'); END;
CREATE TRIGGER entity_names_day_insert BEFORE INSERT ON entity_names BEGIN
 SELECT RAISE(ABORT, 'a journal day has only its canonical date name')
 WHERE EXISTS (SELECT 1 FROM entities e WHERE e.id = NEW.entity_id
   AND ((e.is_journal
         AND (NEW.name_key IS NOT e.preferred_name_key OR NEW.title IS NOT e.preferred_name_key))
     OR (length(NEW.name_key) = 10 AND date(NEW.name_key) IS NEW.name_key AND e.preferred_name_key IS NOT NEW.name_key)));
 -- a period key is reserved like a date (D30): only its own page has it, as its preferred name, spelled in upper case
 SELECT RAISE(ABORT, 'a period page has only its canonical key name (D30)')
 WHERE EXISTS (SELECT 1 FROM entities e WHERE e.id = NEW.entity_id
   AND ((e.period IS NOT NULL AND NEW.name_key IS NOT e.preferred_name_key)
     OR ((NEW.name_key GLOB '[0-9][0-9][0-9][0-9]' OR NEW.name_key GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]'
           OR NEW.name_key GLOB '[0-9][0-9][0-9][0-9]-q[0-9]' OR NEW.name_key GLOB '[0-9][0-9][0-9][0-9]-w[0-9][0-9]')
         AND (e.preferred_name_key IS NOT NEW.name_key OR NEW.title IS NOT upper(NEW.name_key)))));
END;
CREATE TRIGGER entity_names_day_update BEFORE UPDATE OF title ON entity_names BEGIN
 SELECT RAISE(ABORT, 'a journal day spelling is its canonical date')
 WHERE length(OLD.name_key) = 10 AND date(OLD.name_key) IS OLD.name_key AND NEW.title IS NOT OLD.name_key;
 SELECT RAISE(ABORT, 'a period spelling is its canonical key (D30)')
 WHERE (OLD.name_key GLOB '[0-9][0-9][0-9][0-9]' OR OLD.name_key GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]'
           OR OLD.name_key GLOB '[0-9][0-9][0-9][0-9]-q[0-9]' OR OLD.name_key GLOB '[0-9][0-9][0-9][0-9]-w[0-9][0-9]')
   AND NEW.title IS NOT upper(OLD.name_key);
END;
CREATE TRIGGER entities_day_identity BEFORE UPDATE OF preferred_name_key ON entities
 WHEN NEW.preferred_name_key IS NOT OLD.preferred_name_key
BEGIN
 SELECT RAISE(ABORT, 'journal day identity cannot be renamed or acquired through aliases')
 WHERE OLD.is_journal
    OR (NEW.is_journal
        AND EXISTS (SELECT 1 FROM entity_names n WHERE n.entity_id = OLD.id AND n.name_key IS NOT NEW.preferred_name_key));
 -- a page with no name yet (its key's FK is deferred) has no name that refuses a date: its part-of links do, since a
 -- day page is never filed and never a category (D26, links_endpoint_types)
 SELECT RAISE(ABORT, 'a page with a part-of link never becomes a day page (D26)')
 WHERE NEW.is_journal
   AND (EXISTS (SELECT 1 FROM links l WHERE l.from_id = OLD.id AND l.kind = 'part-of')
     OR EXISTS (SELECT 1 FROM links l WHERE l.to_id = OLD.id AND l.kind = 'part-of'));
 SELECT RAISE(ABORT, 'period identity cannot be renamed or acquired through aliases (D30)')
 WHERE OLD.period IS NOT NULL
    OR (NEW.period IS NOT NULL
        AND EXISTS (SELECT 1 FROM entity_names n WHERE n.entity_id = OLD.id AND n.name_key IS NOT NEW.preferred_name_key));
 SELECT RAISE(ABORT, 'a page with a part-of link never becomes a period page (D30)')
 WHERE NEW.period IS NOT NULL
   AND (EXISTS (SELECT 1 FROM links l WHERE l.from_id = OLD.id AND l.kind = 'part-of')
     OR EXISTS (SELECT 1 FROM links l WHERE l.to_id = OLD.id AND l.kind = 'part-of'));
END;

CREATE VIEW entity_search_content AS
 SELECT e.id, n.title AS preferred,
        (SELECT group_concat(title, char(10) ORDER BY name_key) FROM entity_names WHERE entity_id = e.id) AS all_names,
        e.body
   FROM entities e JOIN entity_names n ON n.entity_id = e.id AND n.name_key = e.preferred_name_key;
CREATE VIRTUAL TABLE entities_fts USING fts5(
  -- Derived, one document per entity: body once, all names deterministically ordered.
  -- A phrase may cross adjacent aliases in all_names; exact resolution uses entity_names instead.
  -- unicode61 remove_diacritics 2 folds Latin accents, including compound diacritics; a CJK run is one token.
  -- Token folding is not the full Unicode casefold used by name keys (Straße differs from strasse).
  -- Rebuild from entity_search_content; this is a derived index, never name identity.
  preferred, all_names, body, content='entity_search_content', content_rowid='id',
  tokenize='unicode61 remove_diacritics 2'
);
CREATE TRIGGER entity_names_fts_insert AFTER INSERT ON entity_names BEGIN
  -- AFTER an actual insert: skipped INSERT/UPSERT must not delete an existing document.
  -- Reconstruct the prior aggregate by excluding this row; the first preferred name had no prior document.
  INSERT INTO entities_fts(entities_fts, rowid, preferred, all_names, body)
  SELECT 'delete', e.id, p.title,
         (SELECT group_concat(title, char(10) ORDER BY name_key) FROM entity_names WHERE entity_id = e.id AND id <> NEW.id), e.body
    FROM entities e JOIN entity_names p ON p.entity_id = e.id AND p.name_key = e.preferred_name_key
   WHERE e.id = NEW.entity_id AND p.id <> NEW.id;
  INSERT INTO entities_fts(rowid, preferred, all_names, body)
  SELECT id, preferred, all_names, body FROM entity_search_content WHERE id = NEW.entity_id;
END;
CREATE TRIGGER entity_names_fts_before_update BEFORE UPDATE OF title ON entity_names WHEN NEW.title IS NOT OLD.title BEGIN
  INSERT INTO entities_fts(entities_fts, rowid, preferred, all_names, body)
  SELECT 'delete', id, preferred, all_names, body FROM entity_search_content WHERE id = OLD.entity_id;
END;
CREATE TRIGGER entity_names_fts_update AFTER UPDATE OF title ON entity_names WHEN NEW.title IS NOT OLD.title BEGIN
  INSERT INTO entities_fts(rowid, preferred, all_names, body)
  SELECT id, preferred, all_names, body FROM entity_search_content WHERE id = NEW.entity_id;
END;
CREATE TRIGGER entities_fts_before_update BEFORE UPDATE OF body, preferred_name_key ON entities
 WHEN NEW.body IS NOT OLD.body OR NEW.preferred_name_key IS NOT OLD.preferred_name_key BEGIN
  INSERT INTO entities_fts(entities_fts, rowid, preferred, all_names, body)
  SELECT 'delete', id, preferred, all_names, body FROM entity_search_content WHERE id = OLD.id;
END;
CREATE TRIGGER entities_fts_update AFTER UPDATE OF body, preferred_name_key ON entities
 WHEN NEW.body IS NOT OLD.body OR NEW.preferred_name_key IS NOT OLD.preferred_name_key BEGIN
  INSERT INTO entities_fts(rowid, preferred, all_names, body)
  SELECT id, preferred, all_names, body FROM entity_search_content WHERE id = NEW.id;
END;

CREATE TABLE people (
  -- The owning id is immutable (people_identity_fixed), including through rowid aliases.
  -- people in the owner's life; a relationship between them is words in a page (D8).
  -- A person is also a page with the same id (D20): registry names are the handles [[wikilinks]] write
  -- ('Sam (barber)' tells two Sams apart), its body holds the prose; name is the editable full name.
  id          INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL DEFAULT 'person' CONSTRAINT people_entity_type CHECK (entity_type = 'person'),
  name        TEXT NOT NULL CONSTRAINT people_name CHECK (instr(name, char(0)) = 0 AND name = trim(name)
                           AND length(CAST(name AS BLOB)) BETWEEN 1 AND 240),   -- bytes, like a title
  birth_day   TEXT CONSTRAINT people_birth_day CHECK (birth_day IS NULL OR length(birth_day) = 10 AND birth_day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(birth_day) IS birth_day),
  death_day   TEXT CONSTRAINT people_death_day CHECK (death_day IS NULL OR length(death_day) = 10 AND death_day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(death_day) IS death_day),
  FOREIGN KEY (id, entity_type) REFERENCES entities(id, entity_type),
  CONSTRAINT people_death_day_order CHECK (death_day IS NULL OR birth_day IS NULL OR death_day >= birth_day)
) STRICT;

CREATE TRIGGER people_identity_fixed BEFORE UPDATE ON people
 WHEN NEW.id IS NOT OLD.id
BEGIN SELECT RAISE(ABORT, 'people identity and ownership are immutable'); END;

CREATE TABLE metrics (
  -- The owning id is immutable (metrics_identity_fixed), including through rowid aliases.
  -- what is measured (D7). A metric is also a page with the same id (D27), as a person is: its title is
  -- its preferred name, one series forever under stable id and retained aliases; its body is what the owner
  -- writes about it, and [[Weight]] in the journal reaches it after renaming. The unit
  -- gives every stored value its meaning, so it never changes (metrics_unit_fixed). Seeded with Mood (D6).
  -- A category is a page: a metric is filed in one by a part-of link from its page (D26).
  -- A habit is a unitless metric with a reading of 1 or 0 a day (D24), NOT a category.
  -- A daily metric (daily = 1) is one the writer asks for a reading of on every day (D31); the mark may change.
  -- A metric registered by mistake is tombstoned, never deleted (D11); its readings stay life data.
  id          INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL DEFAULT 'metric' CONSTRAINT metrics_entity_type CHECK (entity_type = 'metric'),
  unit        TEXT NOT NULL DEFAULT '' CONSTRAINT metrics_unit CHECK (instr(unit, char(0)) = 0 AND unit = trim(unit)
                           AND length(CAST(unit AS BLOB)) <= 32),   -- 'kg', 'bpm', 'h'; '' for a count or a 0/1 habit; a scale names its range ('1-5', '0-10')
  daily       INTEGER NOT NULL DEFAULT 0 CONSTRAINT metrics_daily CHECK (daily IN (0, 1)),  -- 1: the writer asks for a reading of it on every day (D31); a day with no reading still says nothing (D24)
  FOREIGN KEY (id, entity_type) REFERENCES entities(id, entity_type)
) STRICT;
CREATE TRIGGER metrics_unit_fixed BEFORE UPDATE OF unit ON metrics
  WHEN NEW.unit IS NOT OLD.unit
BEGIN
  -- changing the unit would silently reinterpret the whole series
  SELECT RAISE(ABORT, 'metrics.unit is fixed: it defines what every stored value means; register a new metric instead');
END;
CREATE TRIGGER metrics_identity_fixed BEFORE UPDATE ON metrics
 WHEN NEW.id IS NOT OLD.id
BEGIN SELECT RAISE(ABORT, 'metrics identity and ownership are immutable'); END;

BEGIN IMMEDIATE;
INSERT INTO entities(id, entity_type, preferred_name_key, body, created_at, updated_at, source) VALUES
  (1, 'metric', 'mood', '1-5; attached to its day page via measurements.captured_with_id when posted', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'schema');
INSERT INTO entity_names(entity_id, title, name_key) VALUES (1, 'Mood', 'mood');
INSERT INTO metrics(id, unit) VALUES (1, '1-5');
COMMIT;

CREATE TABLE files (
  -- The owning id is immutable (files_identity_fixed), including through rowid aliases.
  -- a file the owner keeps (D9): a recording, a PDF, a scan, a photo, a video. A file is also a page with the same
  -- id, as a person is (D20): its title is its handle (![[2026-10-04 Lake.jpg]] in a day page is a wikilink to it),
  -- its body the file's text — a transcript, the text of a PDF or a scan, a caption — so search finds it.
  -- The ORIGINAL is never stored in life.db and never managed by its writer: it stays outside (a photo library), or
  -- is deleted once its text is kept here. sha256 names the original: one page per original, whatever writer sends
  -- it — look it up first (WHERE sha256 = :sha256) and link the page found; files_sha256 refuses a second row.
  -- preview is the retained current picture, replaceable or clearable as a correction, not a version archive: a JPEG, its long edge at most 1600 px (the writer scales it), at most 1 MB,
  -- with no metadata (a photo's GPS is the location history D21 leaves out); NULL where the text is the point (a
  -- recording, a PDF). A video keeps one frame. sha256 and mime never change
  -- (files_original_fixed); a missing preview may be added later. Snapshots preserve only sampled prior states. Never deleted: tombstone the entity (D11).
  id          INTEGER PRIMARY KEY,
  entity_type TEXT NOT NULL DEFAULT 'file' CONSTRAINT files_entity_type CHECK (entity_type = 'file'),
  sha256      TEXT NOT NULL CONSTRAINT files_sha256 CHECK (instr(sha256, char(0)) = 0 AND length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),   -- of the original's bytes, lowercase hex
  mime        TEXT NOT NULL CONSTRAINT files_mime CHECK (instr(mime, char(0)) = 0 AND length(mime) <= 127 AND mime GLOB '[a-z]*/[a-z0-9]*'
                           AND mime NOT GLOB '*[^a-z0-9/.+-]*' AND mime NOT GLOB '*/*/*'),   -- of the original, lowercase: 'audio/mp4', 'image/heic', 'application/pdf'
  preview     BLOB CONSTRAINT files_preview CHECK (preview IS NULL OR (substr(preview, 1, 3) IS x'FFD8FF'
                           AND length(preview) <= 1048576)),   -- a JPEG; IS, not =: substr of an empty blob is NULL
  FOREIGN KEY (id, entity_type) REFERENCES entities(id, entity_type)
) STRICT;
CREATE UNIQUE INDEX files_sha256 ON files(sha256);   -- one page per original
CREATE TRIGGER files_original_fixed BEFORE UPDATE OF sha256, mime ON files
  WHEN NEW.sha256 IS NOT OLD.sha256 OR NEW.mime IS NOT OLD.mime
BEGIN
  -- the hash and the type name the original; another original is another file
  SELECT RAISE(ABORT, 'files.sha256 and mime name the original and never change: another original is another file');
END;

CREATE TRIGGER files_identity_fixed BEFORE UPDATE ON files
 WHEN NEW.id IS NOT OLD.id
BEGIN SELECT RAISE(ABORT, 'files identity and ownership are immutable'); END;

CREATE TABLE measurements (
  -- one row per data point (the FxLifeSheet shape). The table is append-only, enforced by triggers: never
  -- UPDATE or DELETE. A correction is a new row whose supersedes_id names the row it corrects, at most one
  -- per row (chain: correct the correction); a correction with a NULL value RETRACTS the row it corrects.
  -- A correction retains its metric (measurements_supersede_metric).
  -- measurement_values selects current unretracted leaves, with no lifecycle filtering. A recorded-time cutoff
  -- selects eligible chain leaves from measurements, never by filtering the current view; clock order is not commit order.
  -- day is when the value was true, created_at when it was written
  -- down, taken_at + tz when and where it was measured (tz: IANA zone; NULL = unknown).
  -- Imports: INSERT ... ON CONFLICT(source, import_key, metric_id) WHERE import_key IS NOT NULL DO NOTHING;
  -- never OR IGNORE (it silently skips CHECK / NOT NULL violations).
  -- A value is held to its metric: a whole number inside the range a scale's unit names (measurements_scale_range,
  -- D6); a NULL value (a retraction)
  -- is never held, so a wrong reading of any metric can be retracted.
  id               INTEGER PRIMARY KEY,
  metric_id        INTEGER NOT NULL REFERENCES metrics(id),
  day              TEXT NOT NULL CONSTRAINT measurements_day CHECK (length(day) = 10 AND day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' AND date(day) IS day),  -- local date the value refers to
  taken_at         TEXT CONSTRAINT measurements_taken_at CHECK (taken_at IS NULL OR length(taken_at) = 24 AND taken_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z' AND substr(taken_at,12,2) BETWEEN '00' AND '23' AND strftime('%Y-%m-%dT%H:%M:%fZ', taken_at) IS taken_at),
  tz               TEXT CONSTRAINT measurements_tz CHECK (tz IS NULL OR (instr(tz, char(0)) = 0 AND length(tz) BETWEEN 1 AND 64 AND tz NOT GLOB '*[^A-Za-z0-9_/+-]*')),
  value            REAL,                        -- numeric only, by design (D7); NULL only on a correction: it RETRACTS the row it supersedes
  created_at       TEXT NOT NULL CONSTRAINT measurements_created_at CHECK (length(created_at) = 24 AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z' AND substr(created_at,12,2) BETWEEN '00' AND '23' AND strftime('%Y-%m-%dT%H:%M:%fZ', created_at) IS created_at),
  source           TEXT NOT NULL CONSTRAINT measurements_source CHECK (instr(source, char(0)) = 0 AND length(source) BETWEEN 1 AND 64 AND source NOT GLOB '*[^a-z0-9_:.-]*'),   -- the writer (aminte_meta.source)
  import_key       TEXT CONSTRAINT measurements_import_key CHECK (import_key IS NULL OR (instr(import_key, char(0)) = 0 AND length(CAST(import_key AS BLOB)) BETWEEN 1 AND 512)),   -- importer's dedup key, unique per (source, metric); 1 to 512 bytes, no NUL
  captured_with_id INTEGER REFERENCES entities(id),      -- provenance: the page (a day page) this reading was captured with
  supersedes_id    INTEGER REFERENCES measurements(id),  -- optional: corrects an earlier row
  CONSTRAINT measurements_not_self CHECK (supersedes_id IS NULL OR supersedes_id <> id),
  CONSTRAINT measurements_first_has_value CHECK (value IS NOT NULL OR supersedes_id IS NOT NULL),   -- a first reading has a value; only a correction may retract
  CONSTRAINT measurements_value_finite CHECK (value IS NULL OR abs(value) <= 1.7976931348623157e308)   -- finite: rejects ±Infinity (a NaN arrives as NULL)
) STRICT;
CREATE INDEX measurements_series ON measurements(metric_id, day);
CREATE UNIQUE INDEX measurements_import
  ON measurements(source, import_key, metric_id) WHERE import_key IS NOT NULL;
CREATE UNIQUE INDEX measurements_one_correction
  ON measurements(supersedes_id) WHERE supersedes_id IS NOT NULL;  -- one correction per row; also serves measurement_values
CREATE INDEX measurements_day ON measurements(day);                -- day view
CREATE INDEX measurements_capture ON measurements(captured_with_id) WHERE captured_with_id IS NOT NULL;
CREATE TRIGGER measurements_no_update BEFORE UPDATE ON measurements
BEGIN
  SELECT RAISE(ABORT, 'measurements are append-only: correct by inserting a row with supersedes_id');
END;
CREATE TRIGGER measurements_no_delete BEFORE DELETE ON measurements
BEGIN
  SELECT RAISE(ABORT, 'measurements are never deleted: correct by inserting a row with supersedes_id');
END;
CREATE TRIGGER measurements_supersede_metric AFTER INSERT ON measurements
  WHEN NEW.supersedes_id IS NOT NULL
BEGIN
  -- a correction must correct an EXISTING row of the SAME metric (IS NOT, not <>: a dangling
  -- supersedes_id yields NULL, and NULL <> x is NULL = pass). supersedes_id is set only at INSERT
  -- and must name an existing row, so correction chains can never form a cycle.
  SELECT RAISE(ABORT, 'supersedes_id must reference a measurement of the same metric')
   WHERE (SELECT metric_id FROM measurements WHERE id = NEW.supersedes_id) IS NOT NEW.metric_id;
END;
CREATE TRIGGER measurements_scale_range BEFORE INSERT ON measurements
  WHEN NEW.value IS NOT NULL
BEGIN
  -- a scale names its range as its unit, lo-hi of ASCII digits with lo < hi (metrics_range_order, D6): every value is
  -- a whole number inside it. As in metrics_range_order, one bound is exact as a REAL and the other is exact or more
  -- than 10^15, so the two REALs compare as the two numbers do; round() is a core function, not a math one
  SELECT RAISE(ABORT, 'a scale value is a whole number inside the range its unit names (D6)')
   WHERE EXISTS (SELECT 1 FROM metrics m WHERE m.id = NEW.metric_id
                   AND m.unit GLOB '[0-9]*-[0-9]*' AND m.unit NOT GLOB '*[^0-9-]*' AND m.unit NOT GLOB '*-*-*'
                   AND CAST(substr(m.unit, 1, instr(m.unit, '-') - 1) AS REAL) < CAST(substr(m.unit, instr(m.unit, '-') + 1) AS REAL)
                   AND (NEW.value <> round(NEW.value)
                        OR NEW.value NOT BETWEEN CAST(substr(m.unit, 1, instr(m.unit, '-') - 1) AS REAL)
                                             AND CAST(substr(m.unit, instr(m.unit, '-') + 1) AS REAL)));
END;
CREATE VIEW measurement_values AS
  -- the canonical read rule: rows nothing has corrected, minus retractions
  SELECT me.*
    FROM measurements me
   WHERE me.value IS NOT NULL
     AND NOT EXISTS (SELECT 1 FROM measurements x WHERE x.supersedes_id = me.id);

CREATE TABLE capture_keys (
  -- The keys of the captures this file has added, so that a capture a client sends again changes nothing (D28). A
  -- capture appends a paragraph to a day page and may record a Mood reading. It has no row of its own that could hold
  -- a key, and two equal captures can be two true facts, so the content cannot tell a retry from a repeat. The sender
  -- gives the key, once for one capture, as for any row it may send twice (aminte_meta.source). Read the key
  -- first: a row back means the capture was added before, so change nothing and answer with the day of page_id.
  -- Otherwise add the capture, record its Mood reading with the same import_key, and insert this row, all in one
  -- BEGIN IMMEDIATE transaction. The key is unique per source (UNIQUE below), and 1 to 512 bytes without NUL
  -- (capture_keys_import_key): an empty key would make every later capture look added before.
  -- Append-only, as measurements are: a key that changed or went away would let its capture be added again
  -- (capture_keys_no_update, capture_keys_no_delete). page_id is the day page the capture was added to; recorded_at is
  -- when the row was written (aminte_meta.instants).
  id          INTEGER PRIMARY KEY,
  source      TEXT NOT NULL CONSTRAINT capture_keys_source CHECK (instr(source, char(0)) = 0 AND length(source) BETWEEN 1 AND 64 AND source NOT GLOB '*[^a-z0-9_:.-]*'),   -- the writer (aminte_meta.source)
  import_key  TEXT NOT NULL CONSTRAINT capture_keys_import_key CHECK (instr(import_key, char(0)) = 0 AND length(CAST(import_key AS BLOB)) BETWEEN 1 AND 512),   -- the sender's key, unique per source; 1 to 512 bytes, no NUL
  page_id     INTEGER NOT NULL REFERENCES entities(id),   -- the day page the capture was added to
  recorded_at TEXT NOT NULL CONSTRAINT capture_keys_recorded_at CHECK (length(recorded_at) = 24 AND recorded_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z' AND substr(recorded_at,12,2) BETWEEN '00' AND '23' AND strftime('%Y-%m-%dT%H:%M:%fZ', recorded_at) IS recorded_at),   -- the write time (aminte_meta.instants)
  UNIQUE (source, import_key)
) STRICT;
CREATE TRIGGER capture_keys_no_update BEFORE UPDATE ON capture_keys
BEGIN
  SELECT RAISE(ABORT, 'capture keys are append-only: a changed key would let its capture be added again (D28)');
END;
CREATE TRIGGER capture_keys_no_delete BEFORE DELETE ON capture_keys
BEGIN
  SELECT RAISE(ABORT, 'capture keys are never deleted: a removed key would let its capture be added again (D28)');
END;

CREATE TABLE link_kinds (
  -- the CLOSED registry of link kinds: a link's kind must be registered first (FK), and a kind's
  -- structure (allowed endpoint entity types) is fixed at registration and enforced
  -- by a trigger on every link. Registering a kind is a deliberate INSERT, so a typo cannot create
  -- one. from_types / to_types: NULL = any entity type, else a comma list of entities.entity_type values
  -- ('person,metric'), matched token by token: a misspelt token admits nothing, so it never widens a kind,
  -- but the other tokens of its list still admit their types.
  -- an unreferenced kind may be deleted or replaced by the owner; a used one is refused by the links FK, which is
  -- RESTRICT because REPLACE deletes the row first and would otherwise change its structure past the trigger
  kind       TEXT PRIMARY KEY CONSTRAINT link_kinds_kind CHECK (instr(kind, char(0)) = 0 AND kind = lower(kind) AND length(kind) > 0 AND kind NOT GLOB '*[^a-z0-9_-]*'),
  from_types TEXT CONSTRAINT link_kinds_from_types CHECK (from_types IS NULL OR (instr(from_types, char(0)) = 0 AND from_types NOT GLOB '*[^a-z,]*' AND from_types NOT GLOB ',*'
                         AND from_types NOT GLOB '*,' AND from_types NOT GLOB '*,,*')),
  to_types   TEXT CONSTRAINT link_kinds_to_types CHECK (to_types   IS NULL OR (instr(to_types, char(0)) = 0 AND to_types   NOT GLOB '*[^a-z,]*' AND to_types   NOT GLOB ',*'
                         AND to_types   NOT GLOB '*,' AND to_types   NOT GLOB '*,,*')),
  note       TEXT CONSTRAINT link_kinds_note CHECK (note IS NULL OR instr(note, char(0)) = 0)
) STRICT;
INSERT INTO link_kinds(kind, from_types, to_types, note) VALUES
  ('wikilink', 'page,person,metric,file', 'page,person,metric,file', 'extracted from [[body]] on save (an embed ![[...]] is one); body is the truth'),
  ('part-of',  NULL,        'page',         'anything but a day page → the page of a category it is filed in (Ferritin → Iron → Biomarkers, D26); a day page is never filed, which links_endpoint_types checks; walk it with a recursive CTE');
CREATE TRIGGER link_kinds_structure_fixed BEFORE UPDATE OF from_types, to_types ON link_kinds
  WHEN NEW.from_types IS NOT OLD.from_types OR NEW.to_types IS NOT OLD.to_types
BEGIN
  -- changing a kind's structure would leave edges that violate it, or half-edges; register a new kind
  SELECT RAISE(ABORT, 'a link kind''s structure (endpoint types) is fixed at registration; register a new kind instead');
END;

CREATE TABLE links (
  -- one graph for everything: wiki backlinks and what is filed where. kind='part-of' files
  -- a thing in a plain page, never a day page: held by links_endpoint_types (D26).
  -- Rows are hard-deleted (the one such table, D11) and immutable otherwise (delete and
  -- re-insert). Backlinks need only to_id. Cycles are not prevented (D8).
  -- Edit revisions (aminte_meta.edit_revisions): a link is an edit of the page it leaves (from_id) and never of the
  -- page it points at, so another writer's open edit of that page stays current. A wikilink is derived from the
  -- body (D19), whose change already advanced its page, so adding or removing one advances nothing; its note is
  -- still an edit.
  -- source names the writer, as on entities; written at insert, never changed.
  id         INTEGER PRIMARY KEY,
  from_id    INTEGER NOT NULL REFERENCES entities(id),
  to_id      INTEGER NOT NULL REFERENCES entities(id),
  kind       TEXT NOT NULL REFERENCES link_kinds(kind) ON DELETE RESTRICT,  -- closed registry; a used kind is never deleted or replaced
  note       TEXT CONSTRAINT links_note CHECK (note IS NULL OR instr(note, char(0)) = 0),
  created_at TEXT NOT NULL CONSTRAINT links_created_at CHECK (length(created_at) = 24 AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9]Z' AND substr(created_at,12,2) BETWEEN '00' AND '23' AND strftime('%Y-%m-%dT%H:%M:%fZ', created_at) IS created_at),
  source     TEXT NOT NULL CONSTRAINT links_source CHECK (instr(source, char(0)) = 0 AND length(source) BETWEEN 1 AND 64 AND source NOT GLOB '*[^a-z0-9_:.-]*'),   -- the writer (aminte_meta.source)
  UNIQUE (from_id, to_id, kind)
) STRICT;
CREATE INDEX links_to ON links(to_id, kind);   -- backlinks, all or of one kind (from_id is served by the UNIQUE index)

CREATE TRIGGER links_fixed BEFORE UPDATE ON links
  WHEN NEW.id IS NOT OLD.id OR NEW.from_id IS NOT OLD.from_id OR NEW.to_id IS NOT OLD.to_id OR NEW.kind IS NOT OLD.kind
    OR NEW.created_at IS NOT OLD.created_at OR NEW.source IS NOT OLD.source
BEGIN
  -- only note may change; to change anything else, delete and re-insert
  SELECT RAISE(ABORT, 'links are immutable: delete and re-insert');
END;
CREATE TRIGGER links_endpoint_types BEFORE INSERT ON links
BEGIN
  -- the registry is closed even on a connection that forgot PRAGMA foreign_keys=ON; an unknown
  -- endpoint id counts as type '?' and so is rejected for typed kinds
  SELECT RAISE(ABORT, 'link kind is not registered in link_kinds')
   WHERE NOT EXISTS (SELECT 1 FROM link_kinds k WHERE k.kind = NEW.kind);
  SELECT RAISE(ABORT, 'link endpoint type not allowed for this kind (see link_kinds.from_types / to_types)')
   WHERE EXISTS (SELECT 1 FROM link_kinds k
                  WHERE k.kind = NEW.kind
                    AND ((k.from_types IS NOT NULL
                          AND instr(',' || k.from_types || ',',
                                    ',' || coalesce((SELECT entity_type FROM entities WHERE id = NEW.from_id), '?') || ',') = 0)
                      OR (k.to_types IS NOT NULL
                          AND instr(',' || k.to_types || ',',
                                    ',' || coalesce((SELECT entity_type FROM entities WHERE id = NEW.to_id), '?') || ',') = 0)));
  -- two rules link_kinds cannot say, since a day page and a plain page are both 'page': a category is a plain
  -- page, never a day page; and a day page is never filed in a category: a day is found by its date (D26). A link never changes its endpoints (links_fixed), a day page never stops being one,
  -- and a page with a part-of link never becomes one (entities_day_identity, entity_names_day_insert)
  SELECT RAISE(ABORT, 'a category is a plain page, never a day page (D26)')
   WHERE NEW.kind = 'part-of' AND coalesce((SELECT is_journal FROM entities WHERE id = NEW.to_id), 0);
  SELECT RAISE(ABORT, 'a day page is never filed in a category (D26)')
   WHERE NEW.kind = 'part-of' AND coalesce((SELECT is_journal FROM entities WHERE id = NEW.from_id), 0);
  -- a period page (D30) is neither a category nor filed
  SELECT RAISE(ABORT, 'a category is a plain page, never a period page (D30)')
   WHERE NEW.kind = 'part-of' AND coalesce((SELECT period IS NOT NULL FROM entities WHERE id = NEW.to_id), 0);
  SELECT RAISE(ABORT, 'a period page is never filed in a category (D30)')
   WHERE NEW.kind = 'part-of' AND coalesce((SELECT period IS NOT NULL FROM entities WHERE id = NEW.from_id), 0);
END;
CREATE VIEW ghost_pages AS
  -- Empty live plain objects older than 30 days without graph or measurement-capture references,
  -- including historical facts; never a typed identity. Owner tombstones them.
  SELECT e.id, n.title, e.created_at
    FROM entities e JOIN entity_names n ON n.entity_id = e.id AND n.name_key = e.preferred_name_key
   WHERE e.entity_type = 'page' AND e.body = '' AND e.deleted_at IS NULL
     AND e.created_at < strftime('%Y-%m-%dT%H:%M:%fZ','now','-30 day')
     AND NOT EXISTS (SELECT 1 FROM links l WHERE l.to_id = e.id OR l.from_id = e.id)
     AND NOT EXISTS (SELECT 1 FROM measurements m WHERE m.captured_with_id = e.id);

CREATE TRIGGER entity_names_touch_insert AFTER INSERT ON entity_names
 WHEN NEW.name_key IS NOT (SELECT preferred_name_key FROM entities WHERE id = NEW.entity_id)
BEGIN
 -- the owned preferred name inserted with its entity is part of creating it, not an edit; an alias added
 -- later is one, and so is a rename's new spelling (not yet the preferred one when it is inserted)
 UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id = NEW.entity_id;
END;
CREATE TRIGGER entity_names_touch_update AFTER UPDATE OF title ON entity_names WHEN NEW.title IS NOT OLD.title BEGIN
 UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id = NEW.entity_id;
END;
CREATE TRIGGER people_touch AFTER UPDATE ON people
 WHEN NEW.name IS NOT OLD.name OR NEW.birth_day IS NOT OLD.birth_day OR NEW.death_day IS NOT OLD.death_day
BEGIN
  UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id = NEW.id;
END;
CREATE TRIGGER metrics_touch AFTER UPDATE OF daily ON metrics WHEN NEW.daily IS NOT OLD.daily
BEGIN
  UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id = NEW.id;
END;
CREATE TRIGGER files_touch AFTER UPDATE ON files WHEN NEW.preview IS NOT OLD.preview
BEGIN
  UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id = NEW.id;
END;
CREATE TRIGGER entities_touch AFTER UPDATE OF body, day, preferred_name_key, deleted_at, entity_type ON entities
  WHEN NEW.body IS NOT OLD.body OR NEW.day IS NOT OLD.day OR NEW.preferred_name_key IS NOT OLD.preferred_name_key
    OR NEW.deleted_at IS NOT OLD.deleted_at OR NEW.entity_type IS NOT OLD.entity_type
BEGIN
  UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id = NEW.id;
END;
CREATE TRIGGER entities_revision_monotonic BEFORE UPDATE OF revision ON entities
 WHEN NEW.revision < OLD.revision
BEGIN SELECT RAISE(ABORT, 'edit revisions never decrease'); END;
CREATE TRIGGER links_touch_insert AFTER INSERT ON links WHEN NEW.kind <> 'wikilink' BEGIN
 UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id = NEW.from_id;
END;
CREATE TRIGGER links_touch_delete AFTER DELETE ON links WHEN OLD.kind <> 'wikilink' BEGIN
 UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id = OLD.from_id;
END;
CREATE TRIGGER links_touch_note AFTER UPDATE OF note ON links WHEN NEW.note IS NOT OLD.note BEGIN
 UPDATE entities SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), revision = revision + 1 WHERE id = NEW.from_id;
END;
CREATE TRIGGER entities_endpoint_types BEFORE UPDATE OF entity_type ON entities
  WHEN NEW.entity_type IS NOT OLD.entity_type
BEGIN
  SELECT RAISE(ABORT, 'type change would invalidate a retained link endpoint')
   WHERE EXISTS (
     SELECT 1 FROM links l JOIN link_kinds k ON k.kind = l.kind
      WHERE (l.from_id = OLD.id AND k.from_types IS NOT NULL
             AND instr(',' || k.from_types || ',', ',' || NEW.entity_type || ',') = 0)
         OR (l.to_id = OLD.id AND k.to_types IS NOT NULL
             AND instr(',' || k.to_types || ',', ',' || NEW.entity_type || ',') = 0));
END;
CREATE TRIGGER entities_provenance_fixed BEFORE UPDATE OF source, import_key, created_at ON entities
  WHEN NEW.source IS NOT OLD.source OR NEW.import_key IS NOT OLD.import_key OR NEW.created_at IS NOT OLD.created_at
BEGIN
  -- provenance is captured at insert, and a changed key would let a re-run import the row again;
  -- the WHEN clause lets full-row updates through
  SELECT RAISE(ABORT, 'entities.source, import_key and created_at are written at insert and never changed');
END;

CREATE TRIGGER entities_no_delete BEFORE DELETE ON entities
BEGIN
  -- no hard deletes (D11): an entity and its domain rows are tombstoned, never removed; under
  -- PRAGMA recursive_triggers=ON these triggers also stop REPLACE from deleting a row. Only links rows are deleted.
  SELECT RAISE(ABORT, 'entities are never deleted: set entities.deleted_at (tombstone)');
END;
CREATE TRIGGER people_no_delete BEFORE DELETE ON people
BEGIN SELECT RAISE(ABORT, 'people are never deleted: tombstone the entity (entities.deleted_at)'); END;
CREATE TRIGGER metrics_no_delete BEFORE DELETE ON metrics
BEGIN SELECT RAISE(ABORT, 'metrics are never deleted: tombstone the entity (entities.deleted_at)'); END;
CREATE TRIGGER files_no_delete BEFORE DELETE ON files
BEGIN SELECT RAISE(ABORT, 'files are never deleted: tombstone the entity (entities.deleted_at)'); END;

CREATE TRIGGER entities_promote_from_page BEFORE UPDATE OF entity_type ON entities
  WHEN NEW.entity_type IS NOT OLD.entity_type AND OLD.entity_type IS NOT 'page'
BEGIN
  -- promotion (D20): only a plain page changes its type, to a person, a metric or a file; a typed entity
  -- never changes its type. The foreign key of a typed row also refuses the write, and this trigger keeps the
  -- plain sentence of the rule
  SELECT RAISE(ABORT, 'only a plain page is promoted: a person, a metric or a file keeps its type (D20)');
END;

CREATE TRIGGER metrics_range_order BEFORE INSERT ON metrics
  WHEN NEW.unit GLOB '[0-9]*-[0-9]*' AND NEW.unit NOT GLOB '*[^0-9-]*' AND NEW.unit NOT GLOB '*-*-*'
   AND CAST(substr(NEW.unit, 1, instr(NEW.unit, '-') - 1) AS REAL) >= CAST(substr(NEW.unit, instr(NEW.unit, '-') + 1) AS REAL)
BEGIN
  -- a scale names its range as its unit, lo-hi of ASCII digits with lo < hi (D6), and measurements_scale_range holds
  -- its values to that range. A unit of that shape with lo >= hi (5-1, 3-3) is not a range, so it is refused here; a
  -- unit of another shape (kg, '', x-y, 1-2-3) is not touched. A unit never changes (metrics_unit_fixed), so the rule
  -- needs no UPDATE trigger. A unit is at most 32 bytes (metrics_unit), so one bound has at most 15 digits and is
  -- exact as a REAL; the other is exact too, or more than 10^15 as a REAL: the two REALs compare as the two numbers do
  SELECT RAISE(ABORT, 'a scale names its range as its unit, lo-hi with lo less than hi, as in 1-5 (D6)');
END;

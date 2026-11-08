# Imports

**Imports** — the path for data that already exists elsewhere (a journal archive, a health export,
lab results). An importer is a program, or an agent working with the owner in a conversation; either one writes
through the writer's operations (an agent through its tools, [D14](../decisions/D14-ui-and-tools.md)), never around
them. The steps below are for a program that loads rows itself; every step was executed on 1 000 synthetic rows:

1. **A snapshot first.** Rows are never deleted, so a bad import is undone by restoring the copy taken before
   it; otherwise it can only be retracted row by row (a NULL-value correction for a measurement, a tombstone for an
   entity). Take the copy before every import, and run a new importer's first run on such a copy instead of
   `life.db`: `sqlite3 -readonly -cmd "PRAGMA trusted_schema=OFF" life.db "VACUUM INTO '/tmp/trial.db'"` — a
   read-only connection may make the copy (*executed*). A writer's snapshot ([take a snapshot](../cookbook/take-a-snapshot.md))
   is the same copy with its restore check.
2. **Load the rows into a scratch database, never into `life.db`** (`sqlite3 scratch.db ".import
   --csv weights.csv staging"`), then insert in one `BEGIN IMMEDIATE` transaction per batch, on the writer's own connection set up as [connection setup](connections.md) requires (the `sqlite3` shell sets none of it: `foreign_keys` is off there):

```sql
ATTACH 'scratch.db' AS s;
BEGIN IMMEDIATE;
INSERT INTO measurements(metric_id, day, taken_at, tz, value, created_at, source, import_key)
SELECT (SELECT e.id FROM entity_names n JOIN entities e ON e.id=n.entity_id WHERE n.name_key = 'weight' AND e.entity_type = 'metric'), day, NULLIF(taken_at, ''), NULLIF(tz, ''), value,
       strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'import:scale', id
  FROM s.staging WHERE true
ON CONFLICT(source, import_key, metric_id) WHERE import_key IS NOT NULL DO NOTHING;
COMMIT;
DETACH s;
```

   Three traps, each executed. **`WHERE true`** is required: in `INSERT … SELECT … FROM … ON CONFLICT`
   SQLite reads the `ON` as a join's, and answers "a JOIN clause is required before ON" (or `near "DO":
   syntax error`). **The conflict target repeats the index's `WHERE`**: the unique
   index is partial, and without it SQLite answers "ON CONFLICT clause does not match any PRIMARY
   KEY or UNIQUE constraint". **Never `CAST(value AS REAL)`**: it turns `'abc'` and `''` into `0.0`
   and `'12.5kg'` into `12.5`, silently — a plain insert into the STRICT column converts `'12.5'` and
   rejects `'abc'` and `''`. A CSV empty field is `''`, not NULL, so optional columns go through
   `NULLIF(…, '')`; a `''` in `taken_at` fails its CHECK. A fourth, for links: **never `INSERT OR
   REPLACE` into `links`** — it deletes the row and writes it again
   under a new id (executed). Use
   `ON CONFLICT(from_id, to_id, kind) DO NOTHING`.
3. **Identity and time.** `source` names the importer (`import:<name>`), `import_key` is the source's
   own id, `day` / `taken_at` / `tz` say when it happened, `created_at` is when you imported it — on
   every table that has it, `created_at` is the write time (`aminte_meta.instants`). An imported page, person, metric or file
   carries its key on `entities` ([import a row once](../cookbook/import-a-row-once.md)); a capture keeps its key in
   `capture_keys` ([capture](../cookbook/capture.md#a-capture-sent-again-changes-nothing)). **The key must come out the same on every run**: the source's own id
   (a Health Connect record's id; a note's path in its vault). A source without ids gets a key built from
   fields it never changes (a note's file name, the day of a reading) — and a change to one of them then
   looks like a new row. **A key is 1 to 512 bytes without NUL** (`entities_import_key` and the same CHECK on
   `measurements` and on `capture_keys`): the empty string is refused, not stored —
   it would enter the unique index, and every later row of the source would be skipped as imported before.
   A source key that can be longer is hashed by the importer, the same way on every run: the key becomes `sha256:`
   followed by the 64 lowercase hexadecimal digits of the SHA-256 of the key's exact bytes (71 bytes in all). A key
   that is 1 to 512 bytes without NUL is stored as it is, so exactly 512 bytes is not hashed and 513 is; a key with a
   NUL byte is hashed too, never refused or cut. Hash the whole key, never a part, and a stored hashed key put through
   the same rule is unchanged. An importer builds every key from fields the source never changes, so none is empty.
   A CSV empty field is `''`, so an optional key goes through `NULLIF(…, '')` like the other optional columns.
   The key deduplicates within one `source` only: the same reading from two
   sources is two rows, for the app to match and the owner to retract one. A file is the exception: it is found by
   the hash of its original, `files.sha256`, whatever its source, and kept once ([keep a file](../cookbook/keep-a-file.md)).
   An imported checkbox is prose, like any other text of a body.
4. **What a failure does.** `ON CONFLICT … DO NOTHING` handles the named duplicate key; it is not
   validation of a skipped payload. A malformed day or impossible value can still raise before the
   duplicate is skipped. A duplicate with a dangling reference can be skipped without testing that
   reference; a genuinely new row with the same dangling reference fails (executed). Validate input
   before insertion when a changed duplicate payload must be diagnosed.

   **On any statement or commit error, the writer stops and explicitly rolls back the whole batch.**
   SQLite's ordinary constraint `ABORT` undoes the failing statement, including its trigger effects,
   but leaves earlier statements and the transaction active (executed). Do not continue to `COMMIT`
   after an error. Fix the data and run the batch again.
5. **Check afterwards:** the four checks of [integrity checks](integrity-checks.md), per-source counts (`SELECT source, count(*),
   min(day), max(day) FROM measurements GROUP BY source`), and **run the importer a second time — it
   must insert nothing.**
6. **Before the freeze**, a file is rebuilt from its sources: a new `schema.sql`, then the imports run again
   ([D13](../decisions/D13-migrations-and-freeze.md)). An import that the owner makes by hand or with an agent, in a
   conversation, cannot be run again by a program, so the freeze comes before the first such import the owner keeps.
   The [docs index](../README.md) records the current freeze status; the
   [freeze checklist](../process.md#before-the-freeze) governs the first write that a rebuild would lose.


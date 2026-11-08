# 0040 — An empty import key makes later rows vanish as imported before

- **Date:** 2026-10-07
- **Status:** resolved
- **Seen in:** a schema review with synthetic probes against a fresh file built from the schema, SQLite 3.53.4

## What happened

`import_key` is plain `TEXT` on `entities`, `sessions`, `tasks`, `task_occurrences` and `measurements`, with no
CHECK. The DDL accepted the empty string, a key containing NUL and a key of 100,000 bytes. The empty string is the
dangerous one: it is not NULL, so it enters the partial unique index on `(source, import_key)`, and the second row
that a source sends with an empty key matches the first. `ON CONFLICT … DO NOTHING` then reports "imported before"
and skips the row with no error: the data is lost silently. A CSV empty field is the empty string, and the
[imports contract](../contract/imports.md) already says optional columns go through `NULLIF(…, '')`; a sender that
forgot it for the key lost every row but the first.

The same gap, at lower stakes, was open on `people.name` (empty, padded, NUL, any length), `metrics.unit` (padded,
NUL, any length), `links.note` and `link_kinds.note` (NUL) and `tasks.label` (only `length(trim(label)) >= 1`, so a
padded label was accepted).

## Reproduce

Apply [schema.sql](../schema/schema.sql) to a fresh file. Insert two `measurements` rows for metric 1 with
different days and values, `source = 'import:synthetic'` and `import_key = ''`, each with
`ON CONFLICT(source, import_key, metric_id) WHERE import_key IS NOT NULL DO NOTHING`. Both statements succeed and
one row is stored. Insert a `tasks` row whose label is `' padded '`, a `people` row named `''`, and a `links` row
whose note contains `char(0)`: all succeed.

## Rules involved

[`aminte_meta.source`](../schema/schema.sql) (`import_key` is unique per source), [imports](../contract/imports.md)
steps 3 and 4, [import a row once](../cookbook/import-a-row-once.md), [titles and wikilinks](../contract/titles-and-wikilinks.md)
(the 240-byte limit that names share) and [issue 0022](0022-nul-suffixes-bypass-text-checks.md) (NUL in constrained
text).

## Resolution

Named CHECKs: `entities_import_key`, `sessions_import_key`, `tasks_import_key`, `task_occurrences_import_key` and
`measurements_import_key` (NULL, or no NUL and 1 to 512 bytes); `people_name` (no NUL, trimmed, 1 to 240 bytes);
`metrics_unit` (no NUL, trimmed, at most 32 bytes, the empty unit stays); `links_note` and `link_kinds_note` (no NUL);
and `tasks_label` also requires `label = trim(label)`. The imports contract says a key is 1 to 512 bytes and that an
importer hashes a longer source key. The `schema-safeguards` suite probes each table with accepted and refused
values at the bounds, reproduces the incident (an empty key is refused instead of dropping the second row) and
keeps a mutant for every table and every conjunct. No fixture inserted a value the new CHECKs refuse.

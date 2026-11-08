# 0048 — The writer opens a file built from another DDL and fails only at the integrity check

- **Date:** 2026-10-08
- **Status:** resolved
- **Seen in:** the 2026-10 import: `life.db` initialised by a build of the morning, the vault applied to its trial copy
  by a build of the evening, after `schema.sql` had gained columns and tables (`is_journal`, the task tables)

## What happened

`Open` checks `application_id` and `user_version`, and `user_version` counts migrations after the freeze
([D13](../decisions/D13-migrations-and-freeze.md)): before it, every build writes `1`, so a file built from any earlier
`schema.sql` opens without a word. *apply a vault plan* wrote 556 pages into such a file (its inserts named only
columns both DDLs have); the first operation to touch a new column, the integrity check, failed with an SQL error
("no such column") and HTTP 500, read by the model as a defect of the writer. The file had to be rebuilt and the
import redone from the plan.

Expected: a writer that refuses a file of an unsupported version ([issue 0036](0036-writer-opens-unsupported-schema-versions.md))
should also refuse, before the freeze, a file whose DDL is not the one it embeds — a hash of the canonical DDL in
`aminte_meta`, or an equally cheap check — with the one sentence that says what to do: rebuild it from the schema and
replay ([imports](../contract/imports.md)). After the freeze `user_version` carries this.

## Reproduce

1. Build the writer at a commit before a `schema.sql` change; `aminte init --db x.db`.
2. Build it at the commit after; `aminte do integrity-check --db x.db`: an SQL error, not a refusal to open.

## Rules involved

- [D13](../decisions/D13-migrations-and-freeze.md) — rebuild before the freeze, migrate after; `user_version`
- [connection setup](../contract/connections.md) — what a writer checks when it opens a file
- [integrity checks](../contract/integrity-checks.md)

## Resolution

Resolved by the schema check of [integrity checks](../contract/integrity-checks.md) ("The schema — a file that no
longer has the DDL"): the writer builds a fresh file from `schema.sql` and lists every object the file lacks, has
beyond the DDL or holds with another text, by name. `integrity-check` on the file of this incident answers `changed table
entities` and `missing table ...` by name, and the owner rebuilds and replays as
[D13](../decisions/D13-migrations-and-freeze.md) says, instead of reading an SQL error as a defect of the writer. The refusal at `Open` that this issue asked for was declined in the same page: a refusal there would
lock the owner out of the data, so the check is one the owner asks for. After the freeze `user_version` carries it.

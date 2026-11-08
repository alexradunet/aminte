# D13 — Migrations: numbered plain SQL + `PRAGMA user_version`; freeze-and-migrate.

**Status:** accepted

- **Decision.** No ORM, no migration framework, no down-migrations. **Until the freeze there are no
  migrations:** [schema](../schema/README.md) is edited in place and test databases are recreated; `user_version` stays 1. After
  the freeze (next bullet): numbered plain-SQL files, `db/migrations/0002_*.sql`, … applied in order, progress
  in `PRAGMA user_version` [R20](../research/references.md#r20)[R21](../research/references.md#r21)[R22](../research/references.md#r22); additive only — new tables, columns and indexes, a column rename
  (recorded in its migration), and replacing a named CHECK (the named-CHECK bullet below); never a dropped table or column. `PRAGMA application_id = 0x4C494645` ('LIFE') lets
  `file(1)` and future tools recognize the database [R1](../research/references.md#r1).
- **Compatibility.** Here additive means data-preserving, not that every prior query or write remains valid.
  A column rename can break an old query; a tightened CHECK can refuse a write that used to succeed; adding
  a column changes `SELECT *` and inserts without explicit column lists (executed by the `evolution` suite). Readers and writers check
  `application_id` and supported `user_version`, select explicit columns, and refuse unsupported versions
  until their queries and writes are validated against that version. A migration records any compatibility
  break as well as preserving existing data. The frozen version's full DDL and contract remain the reference
  for interpreting files at that version. A writer can run the file (SQLite 3.51.3 or newer) and still be unable to
  migrate it (a migration needs 3.53, `aminte_meta.sqlite`): that writer must not migrate; it refuses a file whose
  `user_version` is below the one it needs, and a writer of 3.53 or newer migrates the file first. A new `entity_type`
  is a migration, so a reader meets a type it does not know only in a file of a newer `user_version`, which it
  refuses; a link kind is a row of `link_kinds`, so a reader reads the kinds from that registry and holds no list of
  its own.
- **Column order is frozen with the file** (a migration can only append a column), so it is chosen before the
  freeze: `entities.body`, the one large text, is the last column, so that reading `deleted_at` or `entity_type`
  does not walk the overflow pages of a long body (the preview of `files` is last for the same reason). A column
  added after the freeze follows the body.
- **The freeze.** The freeze is the first write to the canonical `life.db` that the owner keeps and that a rebuild
  would lose: a row that no program can write again from its source. A capture, a correction, a tombstone, anything
  typed into the file, and an import made by hand or with an agent in a conversation are such writes. Before it, a
  file holding only what programs can write again is rebuilt (a new `schema.sql`, then the imports run again,
  [imports](../contract/imports.md)) instead of migrated. At the freeze, the commit of `schema.sql` that made the file is recorded in the status line of
  [the docs index](../README.md). After it every change is a numbered migration under `db/migrations/`, starting at
  `0002_`, run on a copy first (below); `schema.sql` never changes without one.
- **One full DDL after the freeze.** `schema.sql` stays the full current DDL: each migration also edits `schema.sql` in
  the same commit, so a new file is still that one file applied verbatim. A suite proves the two agree: the frozen
  `schema.sql` (at the recorded commit) with every migration applied in order has the same `sqlite_master` as the
  current `schema.sql` applied to an empty file. The suite is added with the first migration; before the freeze there
  is nothing to compare.
- **Every CHECK is named, so every rule can change without a rebuild.** Widening an enum (a new
  entity type, a new link endpoint), letting partial dates into `birth_day` or loosening the title rules is a
  two-statement transactional migration — `ALTER TABLE … DROP CONSTRAINT <name>; … ADD CONSTRAINT
  <name> CHECK (…)` (SQLite ≥ 3.53 [R55](../research/references.md#r55)) — **only because the CHECK has a name**: an unnamed CHECK
  cannot be dropped (`no such constraint`), and adding a looser second CHECK does not relax the first
  (both apply). `ADD CONSTRAINT` checks the existing rows, so tightening is as safe as loosening. All
  executed on a populated database, with integrity and foreign-key checks clean after. The alternative
  is SQLite's 12-step table rebuild, with FTS triggers, composite FKs and tombstone triggers to recreate.
- **Down-migrations** are rejected as a category. A migration runs on a *copy* first (`VACUUM INTO`,
  as for an importer, [imports](../contract/imports.md)) and the four checks of [integrity checks](../contract/integrity-checks.md) must pass on the copy before it touches
  `life.db`.
- **Alternatives.** *Freeze at the first import* — rejected: an import that a program can run again leaves the file
  rebuildable, so freezing then would lock in a schema that no data depends on yet. *Freeze `schema.sql` as `0001_init.sql` and make a new file by
  replaying every migration on it* — rejected: today's schema would exist only as the sum of the files, so no single
  file would show it, and the comments inside its `CREATE` statements would keep the frozen text.
- **Sources.** [R1](../research/references.md#r1)[R20](../research/references.md#r20)[R21](../research/references.md#r21)[R22](../research/references.md#r22)[R55](../research/references.md#r55).

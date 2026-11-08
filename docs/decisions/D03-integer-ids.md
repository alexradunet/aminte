# D3 — IDs: `INTEGER PRIMARY KEY`; UUIDs rejected.

**Status:** accepted

- **Decision.** Every entity, fact and join table is keyed by `INTEGER PRIMARY KEY` (a rowid
  alias). The registries — `aminte_meta`, `link_kinds` — keep their natural key
  (`key`, `kind`): an integer surrogate would only hide the name. No `AUTOINCREMENT`
  (extra CPU/IO/bookkeeping, "usually not needed" [R4](../research/references.md#r4)). No UUIDs.
- **Alternatives.** UUIDv7/v4 TEXT keys: benchmarked *slower* (random TEXT keys scatter
  inserts across the B-tree) and larger; their only real advantage — collision-free IDs for
  multi-device merge — buys nothing while sync is a non-goal ([non-goals](../architecture/non-goals.md)) [R26](../research/references.md#r26)[R27](../research/references.md#r27).
- **An id is a permanent reference.** Nothing but `links` rows is deleted, so an entity id is never
  reused, and a named entity also has its retained owned names ([D20](D20-named-pages.md)). Typed extension
  ownership is guarded separately from foreign-key validity (`people_identity_fixed`,
  `metrics_identity_fixed`, `files_identity_fixed`): a foreign key alone would allow details
  to move to another identity of the same type. Stable ownership keeps the name, prose, details and edit token
  about the same thing.
- **Trade accepted.** If merging two databases ever becomes real, integer IDs can collide.
  Mitigation then: re-key with one script, or add an `entities.uid` column — additive after the
  freeze up to unique and `NOT NULL` (`ADD COLUMN`, a backfill, a unique index, `ALTER COLUMN uid SET
  NOT NULL`; executed); a backfilled uid loses nothing, since nothing outside pointed at the old rows.
- **Multiple devices: a hub and its clients.** The hub is one live `life.db` on a machine the owner
  holds; it may move to another (a copy, then the checks of [integrity checks](../contract/integrity-checks.md)), never run in two places at once.
  Everything else is a *client* of it — the UI, the owner's own apps, AI agents, a phone — writing
  through the app's API. That is multiple processes or connections, not multiple divergent
  databases: WAL + `busy_timeout` + `BEGIN IMMEDIATE` serialize concurrent writers to one file
  safely, and the "single writer" rule (principle 3) means *single writing application*, not single
  process. A phone keeps a read-only copy ([connection setup](../contract/connections.md)); offline it queues only *new* rows and replays them
  through the API, so nothing can conflict. A replay, like a re-run importer, must insert nothing
  twice: the client gives each new row an `import_key`, the key facts and entities have ([import a row once](../cookbook/import-a-row-once.md)).
- **Rejected: devices that each hold a copy and merge (CRDTs).** cr-sqlite, the SQLite extension for
  it, allows no checked foreign keys, no UNIQUE constraint but the primary key and no CHECK across
  columns in a merged table [R75](../research/references.md#r75) — the composite FKs, the unique `name_key` and the paired CHECKs
  this schema rests on.
- **Sources.** [R4](../research/references.md#r4)[R26](../research/references.md#r26)[R27](../research/references.md#r27)[R75](../research/references.md#r75).

# Aminte — database architecture

**Status:** freeze candidate. Until the first kept write that a rebuild would lose — the freeze — [schema.sql](schema/schema.sql) is edited in place, and the canonical file is rebuilt (a fresh file from `schema.sql`, then the imports run again) rather than migrated ([D13](decisions/D13-migrations-and-freeze.md)). The 2026-10 imports of a real notes folder have already taught [D5](decisions/D05-pages-and-day-pages.md), [D22](decisions/D22-events.md), [D23](decisions/D23-no-tasks.md) and [D24](decisions/D24-habits.md); the owner's word gave [D29](decisions/D29-plan-lines.md) and [D30](decisions/D30-period-pages.md); the owner's import with an agent comes after the freeze ([imports](contract/imports.md)). The checklist before the freeze: [process](process.md#before-the-freeze).

**Scope of the project:** A lifetime personal database — a life log and its backup: a journal of day pages, notes and wiki pages, people,
health metrics and the files the owner keeps, as their text and a small picture ([D9](decisions/D09-binary-files.md)).
A span of life is a plain page ([D22](decisions/D22-events.md)), and a plan lives outside the file
([D23](decisions/D23-no-tasks.md), [D29](decisions/D29-plan-lines.md)); a week, a month, a quarter and a year are pages with a key ([D30](decisions/D30-period-pages.md)). Money and a location track remain deferred ([D18](decisions/D18-money.md), [D21](decisions/D21-location-history.md)). It is one SQLite file,
plus a custom UI for data entry and daily use.
Other devices are clients of the one writing application ([D3](decisions/D03-integer-ids.md)); everything else (view generators, AI
features, sync or merge between copies of the database) is explicitly out of scope.

These pages are the contract of `life.db`: a writer in any language implements them and nothing else.
Each rule has **one home**: a table's rules are the constraints, triggers and comments of its `CREATE`
statement in [schema.sql](schema/schema.sql); the conventions the DDL cannot hold (the wikilink grammar,
connection settings, imports) are in the [storage contract](contract/README.md); the [decisions](decisions/README.md) say
*why*, citing constraint names instead of restating them; the [cookbook](cookbook/README.md) shows the SQL in use.
*Executed* means that a suite in `tests/` runs the claim against the schema ([tests/README.md](../tests/README.md) lists the suites).

## Map

**Architecture** — what the database is for and how it is shaped.

- [Goals and design principles](architecture/goals-and-principles.md)
- [Entity model](architecture/entity-model.md) — entities, the graph, facts and registries
- [Who may link what](architecture/link-rules.md)
- [The life of a page](architecture/page-lifecycle.md)
- [Product concepts](architecture/product-concepts.md) — each product feature and the mechanism behind it
- [Non-goals and deferred work](architecture/non-goals.md)

**Schema** — the canonical DDL.

- [schema.sql](schema/schema.sql) and [its overview](schema/README.md)

**Storage contract** — what the DDL cannot hold; every writer's obligations.

- [Overview](contract/README.md): [titles and wikilinks](contract/titles-and-wikilinks.md),
  [integrity checks](contract/integrity-checks.md), [connection setup](contract/connections.md),
  [threat model and the 2075 test](contract/threat-model.md), [imports](contract/imports.md)

**Decisions** — why each rule is the way it is.

- [Decision log](decisions/README.md): D1–D30, one record each, among them [D29](decisions/D29-plan-lines.md) (no plan lines) and [D30](decisions/D30-period-pages.md) (period pages)

**Cookbook** — the canonical reads and writes, every block executed.

- [Query cookbook](cookbook/README.md)

**Guides** — for people building on the schema.

- [Building a writer](guides/building-a-writer.md)

**Research** — the evidence behind the decisions.

- [Research](research/README.md): [references](research/references.md), [prior-art survey](research/prior-art.md)

**Process** — how the schema changes.

- [How a change happens](process.md): [issues](issues/README.md) → [proposals](rfcs/README.md) → decisions → schema → [plans](plans/README.md)

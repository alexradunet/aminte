# 081 — Planning on every surface: tasks, occurrences and deadlines in the catalog

> Dated implementation record. The schema is untouched: `tasks` and `task_occurrences` and the
> [planning contract](../contract/planning.md) already exist, and `internal/core` implements them. This plan puts
> them on the wire.

## Status

- **Date / baseline:** 2026-10-08, `adea9b8` (master).
- **Priority:** P1. **Effort:** M. **Risk:** LOW for data (the core operations and their tests are unchanged);
  MED for the catalog (new actions reach the CLI, the MCP tools and the browser at once).
- **Status:** DONE.
- **Resolves:** the owner's decision of 2026-10-08 that every capability is on every surface
  ([AGENTS.md](../../AGENTS.md), "Every capability on every surface"). Planning was the one capability core
  carried that no surface reached ("No task UI … is exposed", README).

## Why and boundaries

The planning contract is the owner's reminder intent, preserved independently of any interface. A phone client,
the CLI, a model over MCP and the browser must all be able to read what is due, capture a task and an outcome, and
edit or stop one, through the same handler and the same core operations. Nothing here sends a notification, routes
to a device or infers completion: the contract's non-goals stand.

Out of scope: a notification sender, a dedicated HTML view beyond the generic one (the generic view draws every
property, link and action already), a project workflow, cross-task search, and `snapshot` in the catalog (the
next plan).

## The change

| what | where |
|---|---|
| **Wire shape.** `Task`, `TaskSpec`, `TaskOccurrence`, `OccurrenceChanges` and `TaskReminder` get JSON names (`snake_case`, absent when empty where the value is optional). Two readers: `Tasks(ctx, includeDeleted)` lists definitions by id; `Deadlines(ctx, from, through, includeDeleted)` merges every live task's window through `TaskOccurrences`, ordered by due day, key, task, bounded by the same 10,000. | `internal/core/tasks.go` |
| **Resources.** `GET /tasks` (class `tasks`; `include_deleted`), `GET /tasks/{id}` (class `task`: the definition, its occurrences over `from`..`through`, default 30 days back to 90 days on, each with its resolved reminder), `GET /tasks/{id}/occurrences/{key}` (class `occurrence`: a persisted row, or a virtual slot of a live recurring task), `GET /deadlines` (class `deadlines`: every task's occurrences in the window, with `state` filter `open`/`done`/`skipped`/`all`, default `open`). The root links `tasks` and `deadlines`. | `internal/api/tasks.go`, `handler.go` |
| **Actions** (the catalog, hence the CLI, MCP and the browser): `create-task` (label, project title, cadence, reminder defaults, a one-off's `due_day`, `import_key`), `edit-task` (version; label, project, reminder defaults), `stop-task` (version, `through`), `tombstone-task`, `revive-task`, `capture-occurrence` (task version; `key` = a slot day or `once`; due day, state, completed instant or `now`, reminder mode and instant, import key), `edit-occurrence` (both versions; the same fields), `tombstone-occurrence`, `revive-occurrence`, `deadlines` (GET with `from`, `through`, `state`). Each is offered only where legal: no edit on a tombstoned task, `revive` alone there; `capture-occurrence` on a live task; an occurrence's edits need a live task. All writes run as one `core.Tx` method each. | `internal/api/siren.go`, `tasks.go` |
| **Shortcuts.** `aminte tasks` (the list) and `aminte due [--from D --to D]` (the deadlines); everything else is `aminte do <action>`. | `cmd/aminte/main.go` |
| **Docs.** README's planning bullet rewritten; `API.md` gains the four classes and the shapes; AGENTS.md carries the rule (already written). | `README.md`, `API.md` |

## Tests (with their subject; synthetic databases only)

1. `internal/core`: `Tasks` and `Deadlines` against hand-counted rows: a one-off, a monthly series with one
   materialized and one done slot, a tombstoned task; the window bound.
2. `internal/api`: a whole life through the client, in-process and remote: create a one-off and a series, read the
   task and its virtual slots, materialize one, mark one done with `now`, reschedule, stop the series, tombstone
   and revive; stale versions are 409 `stale_version`; a bad key 422; actions are offered only where legal.
3. `internal/mcp`: the task tools exist with the catalog's fields and `get` follows a task link.
4. `cmd/aminte`: `tasks` and `due` print the entities; `do create-task` persists.
5. `internal/api`: the generic HTML view renders `/tasks`, `/tasks/{id}`, an occurrence and `/deadlines` with their
   forms; the reference test covers the new classes.

## Done criteria (all met; checks on Windows: gofmt, go generate, go vet, go test -count=1 ./... green; staticcheck and govulncheck not run)

1. Every planning operation of core is an action or resource of the catalog; none is reachable on one surface only.
2. The tests above pass; the existing planning tests of core and the contract suites are unchanged.
3. README, API.md and AGENTS.md say so once each.
4. Baseline `go generate ./... && go vet ./... && go test ./...` green and `gofmt` clean on Windows; the commit says
   which checks ran; this plan marked DONE in that commit.

# 0067 — The code says one rule in several places, and keeps what plan 089 removed

- **Date:** 2026-10-12
- **Status:** resolved
- **Seen in:** the owner's request for a review of the whole codebase for inconsistencies, cohesion and redundant
  code; checked against the code at `ef29fe6` by reading every non-test Go file and by `x/tools/cmd/deadcode`

## What happened

`go vet` and `staticcheck` are clean, and every test passes. The review still found three kinds of defect. Each row is
a fact of the code at `ef29fe6`.

**Dead code.** `deadcode -test ./...` names three functions nothing calls (`Tx.MetricID`, `Tx.FillPersonDays`,
`Tx.ReadingByKey`). `FileIn.RequireCaptureDayAgreement` has no setter since plan 089 and two dead branches read it.
Two checks in `photo.itemData` cannot run. Two template helpers, three CSS classes and two struct fields are never
read. Twelve more functions are reachable only from tests (`Store.Counts`, `Tx.File`, `Tx.MaterializeTaskOccurrence`,
the context-less `client.Get/Do/DoFiles/Catalog`, `mcp.New`, the CLI's `do`). The root holds an empty `untitled.md`
and a 28 MB `aminte.exe~`. Comments cite `docs/plans/033` and `.gitignore` cites `docs/guides/importing.md`; neither
exists.

**One rule, several copies.** The same check is written again where it is needed instead of called:

| rule | copies |
|---|---|
| "a flag is 0 or 1" | `api.flag` exists; five handlers write the check inline and three do not check |
| an api error of 422 or 404 | 37 `&core.Error{Status: …}` literals; core has unexported constructors |
| a time-zone label | three identical validators in core |
| an ordered window of exact days | seven copies with six messages |
| a valid title | seven copies with two messages |
| "this page is a day page" | five spellings in SQL and Go, where the schema has `is_journal` |
| the relocation ancestors query | twice, word for word |
| tombstone and revive | four UPDATE pairs |
| the integrity check's scan | eleven identical closures |
| a stale version | one code (`stale_version`) on pages; plain `conflict` on tasks, sessions and periods; the api then relabels every uncoded 409 of a task as stale |
| the owner-only list | the catalog's `Owner` flags and three route wrappers by hand; three tests with their own lists, one naming `replay`, which is not an action |
| the write path | 22 `Store` one-liners for pages and readings; 12 `Store.Do` closures in the api for sessions, periods and tasks |
| the command list of the CLI | the usage, the switch, `commandConsumesContext` and a test |
| the default `life.db` | three places in `main.go` |
| a month grid | twice in Go, twice in the templates |
| the mood list, the step buttons, "On this day", "Due" | inline copies across the templates |

**Behaviour that differs by place.**

| what | where it differs |
|---|---|
| `aminte init PATH` | PATH is ignored when `$AMINTE_DB` is set; `o.dbExplicit` is set and never read |
| every flag is global | `--dry-run` is honoured by `file` only; `habits --day D` shows today; `day --public` is ignored, where the README promises a refusal |
| a failed restore check | exit 1 from `aminte snapshot`; 200 from the API, so `do snapshot` and the browser report success |
| "today" | the CLI's `capture`, `habits`, `done` compute it on the client; `day` and MCP `get_day` ask the server |
| an id | MCP alone turns `1e6` into an integer; the API's parsers refuse it |
| the agentic template | tells an agent to use the CLI, which writes as `cli`, so the owner-only refusal and the `agent:NAME` provenance are bypassed; its shell lines omit `--db` |
| the MCP instructions | say "a page title never changes" while `rename` is a tool; every `version` field is described as the page's |
| a client error | prints 200 bytes of a non-Siren body: the bytes of a picture for `get /pages/1/preview` |
| errors swallowed | `metricOf`, `unitless`, `recordAction` drop a database error and draw a page with missing actions |
| a timeout of `query` | reported as 422 `invalid`, since the cause is flattened by `%v` |
| the connection check | a failed PRAGMA read is 0, which is the value `trusted_schema` wants; a reader is detected by `strings.Contains(dsn, "mode=ro")` |
| the context | `db.Copy`, `Snapshot`, `Init`, `Open` take none; a lifetime `VACUUM INTO` cannot be cancelled |
| a tombstoned metric | accepted by `MetricUnit`, refused by `Record` |
| lifecycle statuses | an entity's missing id is 409 and "already in that state" 409; a task's or session's is 404 and a no-op |
| an implicit project page | gets today's day and shows in the day view; a wikilink target gets none |
| owner-only | 403 from the api, 422 from core's `relocateReading` |
| a check-in | the period check and the write run in two transactions |
| plain-text errors | `picture`, the local guard and the public listener answer `http.Error`, not the error entity |
| N+1 reads | `habitsOn` reads 14 times; `dueIn` once per task; `Deadlines` one snapshot per task; the shell reads the due count again on every page |

## Reproduce

1. `go run golang.org/x/tools/cmd/deadcode@v0.52.0 -test ./...` at `ef29fe6`: three functions.
2. `AMINTE_DB=/x/life.db aminte init /y/life.db`: `/x/life.db` is created or refused; `/y` is never touched.
3. On a fresh file: create a task, read it, edit it twice with the first version: the second answer is `409 stale_version`
   on a task but `409 conflict` on a session edited the same way.
4. `aminte get /pages/1/preview` on a file page with a picture: stderr shows the picture's bytes.
5. `grep -c '&core.Error{' internal/api/*.go`.

## Rules involved

- [AGENTS.md](../../AGENTS.md): "Prefer the smallest understandable solution", "One home per concept", "Keep CLI,
  browser and MCP adapters thin", "Every capability on every surface", "Contexts and ownership", "Errors are part of
  the behavior", "Check errors"
- [README](../../README.md): "One handler, three surfaces", "Provenance per surface", "Owner-only actions",
  "Optimistic saves"
- [API.md](../../API.md): the error codes; `stale_version` "for a save with an old version"
- [connections](../contract/connections.md): the pragmas read back and refused if wrong

## Resolution

Resolved by [plan 098](../plans/098-the-codebase-made-cohesive.md): the dead code removed; one helper for each rule
above, in core or in the api, and every copy replaced by a call; the `Store` wrappers as the one write path; the
defects of the third table fixed with a reproducer each; `handler.go` and `main.go` split by concern, the CLI driven
by one command table; the docs kept true to the code.

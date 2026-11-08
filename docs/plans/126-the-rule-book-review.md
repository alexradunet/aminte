# 126 — The rule-book review: the gaps against the five books, and the actions that close them

> Dated implementation record. Tests use temporary folders and synthetic data only. The owner's live `life.db` is
> never opened by a test or by an agent.

## Status

- **Date / baseline:** 2026-10-17, `01221e5` (master, after the engineering rule books entered AGENTS.md).
- **Priority:** P1 for batch 1, P2 for batch 2, P3 for batch 3 and the docs. **Effort:** L in all.
  **Risk:** LOW for batches 1 and 3 (small diffs, behavior-preserving refactors); MEDIUM for batch 2 (new checks and
  two new owner-only actions).
- **Status:** DONE. Every action (1–19) is done; the open points are in the record. The owner approved the plan
  on 2026-10-17 ("retry", then the approval of the plan file).
- **Resolves:** the owner's request of 2026-10-17: "do a review of the entire codebase based on the new rules we
  applied. Let's find all the gaps and opportunities".

## Context

Commit `01221e5` made five rule books the engineering guidance of this repo: A Philosophy of Software Design, Code
Complete, Refactoring, Release It! and Designing Data-Intensive Applications. AGENTS.md maps each task type to its
books. The owner asked for a review of the whole codebase against these books: the gaps and the opportunities.
AGENTS.md and `docs/` win over a book, so a deliberate decision of the repo is not a finding.

## Method

Four read-only passes, one per row of the map in AGENTS.md, each on the mini file of its books:

| row | books | scope |
|---|---|---|
| new code | APoSD, Code Complete | all non-test Go |
| existing code | Refactoring | all non-test Go, duplicated test helpers |
| failure handling | Release It!, DDIA | serve, main, db, snapshot, integrity, imports, MCP, client, photo |
| schema and contract | DDIA | `schema.sql`, `docs/contract`, the decisions, the write path |

Every finding below was verified against the code before it was kept. The ones that AGENTS.md or a comment already
settles were dropped: the `linkKinds` form filter, `isScale`, the `Store` over `Tx` pair, the double token check.

**The code is in good shape.** The write path, the connection setup, the untrusted-file handling and the error
hygiene are strong (see "Keep" at the end). The findings are gaps at the edges, not a weak core.

## Findings

Severity is the gain for the maintainer or the owner, not the size of the fix.

### A. The repo's own rules, not met (AGENTS.md wins over every book)

| # | finding | where | severity |
|---|---|---|---|
| A1 | **No check that the live file has the schema of `schema.sql`.** `Open` checks `application_id` and `user_version` only. Plans 124 and 125 changed the live file with one-off programs; a binary and a file can disagree on a trigger and nothing says so. No non-test Go reads `sqlite_schema`. | `internal/db/db.go:196-209` | HIGH |
| A2 | **"A comparison of a resent payload" is in AGENTS.md as Go's job, and it does not exist.** `saveBody` answers 409 on a stale version even when the body is identical. A retried `capture` after a lost reply appends the paragraph twice and records a second mood. `import-a-row-once.md` says a row typed on the hub "cannot arrive twice". | `internal/core/pages.go:11-19`, `internal/core/journal.go:20-25`, `docs/cookbook/import-a-row-once.md:7` | MEDIUM-HIGH |
| A3 | **Derived data has no repair on any surface.** The integrity page says the FTS repair is a raw `INSERT ... 'rebuild'` on a writer connection; D19 calls link drift "detectable and repairable", but `Integrity` has no link-vs-body check and the catalog has no rebuild or resync action. "Every capability on every surface" is broken for the repair. | `docs/contract/integrity-checks.md:44`, `docs/decisions/D19-wikilink-save-contract.md:25`, `internal/core/integrity.go:30-55` | MEDIUM-HIGH |
| A4 | **Go refusals that repeat a CHECK (issue 0068 class).** `CheckSource` is the regex of `entities_source`, on three paths. `preview.Check` repeats the JPEG magic and the 1 MiB limit of `files_preview` (the long-edge check is Go's, SQLite cannot decode). `keep` refuses a MIME that `NormalMIME` cannot parse, which `files_mime` refuses too. Nuance: a read has no CHECK, so a source check of the header on a read is boundary validation; decide and say so in the comment. | `internal/core/core.go:125-132`, `internal/core/tx.go:24`, `internal/api/serve.go:432`, `cmd/aminte/main.go:379`, `internal/preview/preview.go:76-91`, `internal/core/files.go:68-76` | MEDIUM |
| A5 | **The day rule and the writer disagree.** `aminte_meta.days` says a day is "written by the app from the local calendar of the device that captured it (never a server's)". `Store.Today()` resolves `today` in the server's zone for every surface, by design (`core.go:29-33`). | `docs/schema/schema.sql:24`, `internal/core/core.go:62` | MEDIUM |
| A6 | **D07 cites a trigger that does not exist**: `measurements_supersede_scope` is not in `schema.sql`. "Current truth only." | `docs/decisions/D07-measurements.md:13` | LOW |

### B. Release It!: the service and the snapshot

| # | finding | where | severity |
|---|---|---|---|
| B1 | **SIGTERM is not handled.** `signal.NotifyContext(ctx, os.Interrupt)` catches SIGINT only; the unit sets no `KillSignal`. Every `systemctl restart` kills the process at once: no drain, no `PRAGMA optimize`, an in-flight upload cut. The drain that `serveHTTP` and its process test promise never runs in production. | `cmd/aminte/main.go:343`, `cmd/aminte/serve.go:69-98`, `/etc/systemd/system/aminte.service` | HIGH |
| B2 | **The nightly snapshot can fail silently and then block the day.** A snapshot that fails its restore check is kept under the normal `life-DATE.db` name (by design). The script `/usr/local/bin/aminte-snapshot` skips when that name exists, so a failed or truncated file counts as today's snapshot and toward the "newest 14". The `life-DATEThhmmss.db` fallback never matches the prune glob. The oneshot unit has no `TimeoutStartSec` and no `OnFailure`. The script and the units are not in the repo. | `internal/api/maintenance.go:21-24`, `internal/db/db.go:399-415`, the script and units outside the repo | MEDIUM |
| B3 | **No request deadline anywhere.** The server sets `ReadHeaderTimeout` and `IdleTimeout` only; the only `WithTimeout` is the drain. The 504 path of `errorEntity` cannot fire from the server. The integrity check runs its FTS step on the single writer connection with no bound, so a long check blocks every write. | `cmd/aminte/serve.go:160`, `internal/core/integrity.go:88`, `internal/db/db.go:185` | MEDIUM |
| B4 | **`SQLITE_BUSY` is a generic 500.** Nothing maps a busy error; the CLI and the service write the same file. A busy at `BEGIN IMMEDIATE` applied nothing, so a retry is safe, and the client cannot tell. `Close` joins a failed `PRAGMA optimize` into the CLI exit code, which turns a done command into exit 1 (and stops the snapshot script under `set -e`). | `internal/core/core.go:104-121`, `internal/db/db.go:307-317` | LOW-MEDIUM |
| B5 | **Readers: unbounded pool, no `busy_timeout`.** Only `W` has `SetMaxOpenConns`; the read-only DSN sets `query_only` and `trusted_schema` only. Check `connections.md` before changing: the contract may say readers need none under WAL. | `internal/db/db.go:131-143`, `:186` | LOW-MEDIUM |
| B6 | **Diagnosis at 2 a.m.** No version or `user_version` in the start line, no stop line, no health route, no `recover` at the handler boundary (a panic drops the connection without a `logFault` line), a wrong token on `/login` leaves no log line. | `cmd/aminte/serve.go:161`, `internal/api/public.go`, `internal/api/serve.go` | LOW |
| B7 | **The remote client has no response timeout and reads the body whole.** Dial and TLS are bounded; headers and the body are not (`io.ReadAll`). An MCP tool call on a hung server hangs with the agent. | `internal/client/client.go:44-52`, `:246` | LOW |

### C. Designing Data-Intensive Applications: the contract

| # | finding | where | severity |
|---|---|---|---|
| C1 | **`name_key` is derived, cannot be recomputed, and nothing checks it.** `entity_names_fixed` refuses a change; `Integrity` never compares a stored key with `text.TitleKey(title)`. When the Unicode tables move, stored and computed keys drift and no tool sees it. The check is a writer gap; the recompute path is a schema gap (the trigger). | `docs/schema/schema.sql:113-115`, `internal/core/integrity.go` | MEDIUM |
| C2 | **The integrity checks miss the rules only triggers hold.** The typed-link query checks `link_kinds` only, not the three rules of `links_endpoint_types` (an `at` from a day page; `part-of` never from or to a day page); `measurements_scale_range` is `BEFORE INSERT` only and has no check query. Each check runs in its own read snapshot while the service writes, so the result is not one view. `integrity-checks.md` says the typed-edge query "independently checks" the registry, which overstates it. | `internal/core/integrity.go:30-55`, `docs/contract/integrity-checks.md` | MEDIUM |
| C3 | **A clock step back refuses tombstones.** `entities_deleted_after_created` refuses a `deleted_at` earlier than `created_at`; a restore onto a host with a wrong clock answers 422 and no page says how to recover. | `docs/schema/schema.sql:73` | LOW |
| C4 | **Claims not marked executed.** `connections.md` says a long-held reader makes the WAL grow "without bound"; no suite runs it. Nothing sets `journal_size_limit` or watches the WAL of the long-lived service. | `docs/contract/connections.md:46-47` | LOW |
| C5 | **An old reader's path is only "check `user_version`".** Nothing says what a reader does with an unknown `entity_type` or link kind; the migration floor (3.53) sits above the runtime floor (3.51.3) with no sentence about a writer that runs the file but cannot migrate it. | `docs/decisions/D13-migrations-and-freeze.md`, `aminte_meta.evolution` | LOW |

### D. A Philosophy of Software Design, Code Complete, Refactoring: the structure

| # | finding | where | severity |
|---|---|---|---|
| D1 | **`Properties any` is an untyped bag with temporal coupling.** `paged` creates the map as a side effect; callers assert it back (`// paged made them`). Four `e.Properties.(...)` assertions panic on a wrong shape or a reordered line. | `internal/api/request.go:182-195`, `internal/api/browse.go:248`, `internal/api/pages.go:44,179`, `internal/api/measurements.go:193` | MEDIUM |
| D2 | **The `limit+1` read-ahead leaks to every list.** Ten call sites pass `limit+1` and trust `paged` to trim; a new list that passes `limit` loses its `next` link silently. `browseRows` has five near-identical read-then-page blocks. | `internal/api/browse.go:307-365`, `internal/api/days.go:115`, `internal/api/pages.go:167` | MEDIUM |
| D3 | **`parseFileForm` mixes every phase and has a side channel.** 119 lines: multipart streaming, byte budgets, SHA-256, MIME sniffing, preview, reconciliation, the dry-run flag; a four-value return with two closures that repeat it; the same message in three places. It writes `r.PostForm` so that `pageAnswer` can read `body` or `text` later: the generic answer layer knows one action's field names. | `internal/api/files.go:62-181`, `internal/api/serve.go:376-379` | MEDIUM |
| D4 | **Shotgun surgery on the browse kind.** The kind strings are switched on in `core/browse.go`, `api/browse.go` (18 times `"media"`), `view/browse.go` and `view/shell.go`: a new kind needs edits in four files and about seven places. | `internal/api/browse.go:22-49,251-365,393-403`, `internal/api/view/browse.go:149`, `internal/api/view/shell.go:55-60` | MEDIUM |
| D5 | **Error codes have no owner.** `codeOf` maps status to a code string; `alertOf` switches on the same strings; `"exists"` and `"cross_origin"` are inline; `core.Error` uses bare `422`, `404`, `409` where the API uses `http.Status*`. A renamed code drifts silently to the empty alert. | `internal/api/serve.go:306-326`, `internal/api/view/page.go:407-418`, `internal/core/core.go:80-101` | MEDIUM-LOW |
| D6 | **The catalog's positional literals.** Twenty-five entries end in `}, false, false},`: the owner-only guarantee hangs on an unlabeled sixth position. 0/1 flags are `"number"` fields with `Options: {"0","1"}`. | `internal/api/catalog.go:60-125` | MEDIUM-LOW |
| D7 | **`answer` changes mode by the dynamic type of `w`** (`caught`), and `refusedOnItsPage` re-dispatches a synthetic GET through the whole mux, guards included. Documented, but no caller of `answer` can see it. | `internal/api/serve.go:201-226`, `:350` | MEDIUM-LOW |
| D8 | **`cmd/aminte` option bag.** `opts` has about 30 fields; `prepare` mutates it in place; `runContext` special-cases `cmd.name != "serve"`; `dbPath` carries an `init`-only message. | `cmd/aminte/main.go:33-41`, `:376-397`, `:444` | LOW-MEDIUM |
| D9 | **`keep` and `ownDay`.** 116 lines in numbered phases; `ownDay` takes six parameters, two booleans; `f.unknownCaptureDay` and `own` encode one fact twice. | `internal/core/files.go:66-199` | LOW-MEDIUM |
| D10 | **`formOf`** is 125 lines on `opts ...string` key-value pairs, with four repeated comma splits and six local maps; a typo in an option name is silently kept. | `internal/api/view/forms.go:160-285` | LOW-MEDIUM |
| D11 | **Smaller repeats.** The previous/next `text.IsDay` guard appears eight times (`days.go`, `months.go`, `weeks.go`, `trends.go`); `"agent:"` is spelled in `main.go:116` and tested in `serve.go:438`; `/browse?kind=...` hrefs are literals in `browse.go` and `days.go`; `db.DB` exports `W` and `R` so "every write is BEGIN IMMEDIATE" is by convention (`integrity.go:88` uses `W` directly, with a reason); `mcp.tool` marks the built-in `get` by a nil action and a name test. | various | LOW |

### Keep (strengths every pass named)

- `mount` panics at start when the catalog and the handlers disagree: fail-fast for "every capability on every surface".
- `db.Write`, `core.alone` and `refused` give one deep write API: `BEGIN IMMEDIATE`, rollback, the CHECK's sentence as the 422.
- Every writer connection reads its pragmas back and refuses on a mismatch; the version floor is checked before the writer activates.
- Untrusted files: `DecodeConfig` and a pixel budget before decoding, streaming SHA-256, capped parts.
- Snapshots: `VACUUM INTO` through a read-only connection, a reserved name, never overwritten, a restore check that leaves the file as written.
- Logs carry the route pattern and the cause, never a path, a body or a reading.

## The actions

Small, separate commits. A structural change never shares a commit with a behavior change (Refactoring). A schema
change follows the process; none is proposed here without an issue first.

### Batch 1: operations and correctness (small diffs, the highest gain)

1. **SIGTERM** (B1): add `syscall.SIGTERM` beside `os.Interrupt` in `cmd/aminte/main.go:343`. Extend the process
   test in `cmd/aminte/serve_process_test.go` with a SIGTERM case. Ship: build to `~/.local/bin/aminte`, restart
   `aminte.service`, report the log line (AGENTS.md).
2. **Busy is a retry** (B4): in `core.refused`, map `SQLITE_BUSY` to a 503 with code `busy` and let `errorEntity`
   set `Retry-After`. Add the code to `codeOf` and `alertOf`. Log the failed `PRAGMA optimize` of `Close` and do not
   fail the command for it (no data is lost by a skipped optimize; say so in the comment).
3. **D07** (A6): remove `measurements_supersede_scope` from `docs/decisions/D07-measurements.md:13`; the
   `document` suite checks nothing here, so read the sentence and keep it true.
4. **Start line** (B6): print `version` and `user_version` in the serving line of `cmd/aminte/serve.go:161`, and a
   stop line after `serveHTTP` returns. Add a `recover` in `serve` that calls `logFault` and answers the 500 entity.
5. **Request bound** (B3): a `ReadTimeout` is wrong for uploads, so give the multipart route its own deadline with
   `http.ResponseController.SetReadDeadline`, and every other route a request context deadline (one constant, named,
   in `serve.go`). Give `Integrity` a deadline and say in `integrity-checks.md` that the FTS step holds the writer.
6. **Snapshot lifecycle** (B2): copy the script and the two units into the repo under `deploy/` and link them from the
   README install section; add `TimeoutStartSec=30min` and an `OnFailure=` unit; make the script test the exit
   status, not the file; prune `life-*.db`. In `TakeSnapshot`, rename a snapshot that fails its restore check to
   `life-DATE.failed.db` so a restore routine never picks it (D25 "trusted once its restore check passes" still
   holds; update the sentence in `internal/api/maintenance.go:21`).

### Batch 2: the repo's own rules (writer changes, each with a test first)

7. **Resent payload** (A2): in `saveBody`, when the version is stale and the body equals the live body, answer 200
   with the live version (the AGENTS.md comparison). Correct `import-a-row-once.md:7`. For `capture`, open an issue
   in `docs/issues/` that records the retry case; a request key is a schema question and waits for the process.
8. **Repair on every surface** (A3): add the owner-only action `rebuild-search` (the FTS `'rebuild'` on `W`) to the
   catalog, and a `links` check in `Integrity` that extracts the wikilinks of every live body with the existing
   `text` extraction and compares them with `links` rows (Go holds the algorithm, AGENTS.md allows it). A
   `resync-links` action follows the same shape. Update `integrity-checks.md` and the README action list; the
   `apitest` parity suites pick the actions up.
9. **Delete the repeated refusals** (A4): remove the magic and size parts of `preview.Check` (keep the edge check),
   remove the `NormalMIME == ""` refusal in `keep`, and let the CHECK answer. Decide `CheckSource`: keep it for the
   read path with a comment that says a read meets no CHECK, or move it to `isOwner` and let writes meet
   `entities_source`. Run the existing mutants; they already witness the CHECKs.
10. **Integrity covers the triggers** (C1, C2): add the three `links_endpoint_types` rules and the scale-range rule
    to `idChecks`, and a Go check that compares every `entity_names.name_key` with `text.TitleKey(title)`. Run the
    SQL checks in one read transaction so the result is one view. Update `integrity-checks.md` to match: the page
    is the home of the SQL, the Go cites it.
11. **Schema drift** (A1): in `Integrity`, build a fresh file from the embedded `schema.sql` in a temp dir and
    compare the normalized `sqlite_schema` rows (`type, name, tbl_name, sql`, whitespace folded) with the live
    file; report the differences as a list. Not at `Open`: a refusal there locks the owner out. The owner's current
    practice (compare the whole schema by hand after a live change) becomes a check on every surface.

### Batch 3: structure (behavior-preserving refactors, one per commit, when a change touches the area)

12. One `readPage` helper that owns `limit+1` and the trim; `paged` returns the page and the props instead of
    mutating `e.Properties` (D1, D2). Then a typed `browseProps`.
13. Split `parseFileForm` into `readMultipart`, `verifyOriginal`, `previewOf`; return one `fileForm` struct; give
    `frame` a `Submitted` field instead of writing `r.PostForm` (D3).
14. A `browseKind` table (chip words, new action, read function, tombstonable) keyed by kind (D4).
15. Error codes as constants in `core`, used by `codeOf`, `alertOf` and `errorEntity` (D5).
16. Named fields in the catalog literals and a `flagField` helper (D6).
17. Later, when the files are touched: `neighbourLinks`, `formOptions`, per-command flag structs, `ownDay` as a
    small value (D8 to D11).

### Docs only

18. Settle A5: either `aminte_meta.days` says "or the hub's zone when the client sends `today`", or the catalog
    requires the day from the client. The first is a one-line schema comment change and follows the process.
19. D10 or D11: the recovery step after a clock step back (C3). `connections.md`: mark the WAL growth claim as
    external or add a probe (C4). D13: one sentence on a writer that runs the file but cannot migrate it (C5).

## Done criteria

- Baseline after every commit: `go generate ./... && go vet ./... && go test ./...`.
- Batch 1: the process test for SIGTERM; a test that a `SQLITE_BUSY` from a second writer answers 503; a test that a
  snapshot with a failed check lands under `.failed.db`.
- Batch 2: a test per check with a deliberately damaged temp database (an `at` link from a plain page written with
  the trigger dropped, a `name_key` set by hand, a dropped trigger for the drift check); a parity test that
  `rebuild-search` is refused to `agent:*` and absent from the MCP tools; a save-body test for the identical resent body.
- Batch 3: `go test -count=1 -shuffle=on ./...` after each refactor; no test changes except renames.
- Shipping a service change: `go build -o ~/.local/bin/aminte ./cmd/aminte`, a restart of `aminte.service`,
  the service's own log line, as AGENTS.md asks.

## Record

- 2026-10-17: the review ran as four read-only passes; the findings were verified against the code; the owner
  approved the plan. No code has changed yet.
- 2026-10-17: actions 1–11, 18 and 19 are done, in small commits merged on `master`. The choices made where the
  plan gave one:
  - Action 9: `CheckSource` is gone. A write meets `entities_source`; an owner-only action checks the name and the
    prefix at the boundary (`requireOwner`), because a read meets no CHECK and "Agent:x" must not pass as the owner.
  - Action 18: the first option. The `days` row says the hub gives the day when the client sends `today`
    ([issue 0086](../issues/0086-today-is-the-hubs-day.md)). The capture retry is [issue 0085](../issues/0085-a-resent-capture-appends-twice.md), open.
  - Action 6: the script and the units in `deploy/` were written new, because the installed copies were not in
    the work folder. They hold `OWNER` placeholders; the owner compares them with the installed copies first.
  - Action 10: `schema_drift` is a finding and is not part of `ok`; `link_drift` is part of `ok`, like the
    full-text check. A live file with link drift therefore fails the restore check of a snapshot until the owner
    runs `resync-links`.
  - B5: no change. [Connections](../contract/connections.md) asks nothing more of a reader under WAL.
  - Open: a process killed between the copy and the restore check leaves an unchecked file under a good name; a
    fix copies under a temporary name first and changes D25, so it waits for the owner.
  - Not done here: shipping. The work ran in a cloud container with no service. The owner builds to the install
    path, restarts `aminte.service` and reads the new start line, which names the version and `user_version`.
- 2026-10-17: batch 3 (actions 12–17) is done, as behavior-preserving refactors, one per commit. The choices:
  - Action 15: the error codes are constants in `siren`, not `core`, because the view may import `siren` and
    never `core` (`TestTheViewImportsNoStore`). The writer prefixes `agent:` and `import:` live there too.
  - Action 14: the "New" action of each kind and the browse hrefs are in `siren`, for the same reason.
  - Action 13: `parseFileForm` still writes `r.PostForm` in one commented line. The redirect, `backOf` and the
    refused form read it; a handler has no other way to give it to `serve` without a change of its signature.
  - Action 17: `trends.go` keeps its own bound (one lower bound, not a pair of neighbours); `db.DB` keeps `W` and
    `R` exported; templates still pass form options as pairs, as a template cannot build a struct.
  - Action 12: the JSON keys of a list's properties now come in the order of the struct, not sorted. The names, the
    values and the presence are the same.
- 2026-10-17: shipped. On the owner's machine the baseline, `staticcheck` (2026.2.2) and `govulncheck` (v1.9.0) were
  clean; the owner built `/home/cico/.local/bin/aminte` (the previous binary kept as `aminte.prev`) and restarted
  `aminte.service` at 17:07; its start line reads "version 0.1.0, user_version 1". At 17:09 the owner installed the
  script and the units from `deploy/`, with this machine's user and paths, and started the snapshot unit once: the
  snapshot passed its restore check, the script found 8 good snapshots and pruned nothing.

Skills: a-philosophy-of-software-design, code-complete, refactoring, release-it,
designing-data-intensive-applications. Conflict: Code Complete says "validate input at trust boundaries"; AGENTS.md
says a Go check that repeats a CHECK is a defect. AGENTS.md won (finding A4), with the read-path nuance named.

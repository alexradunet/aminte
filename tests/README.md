# tests/ — the validation suites of the docs

The [docs](../docs/README.md) mark a claim *executed* when a suite in this folder runs it. The suites are Go tests
(package `tests`, in the module at the repo root), so the rule in [AGENTS.md](../AGENTS.md) — *after changing DDL,
re-run the checks* — is one command. **Nothing here is a migration and nothing touches `life.db`**: every suite reads
`docs/schema/schema.sql` (and the contract and cookbook blocks it needs) from `docs/`, builds throwaway databases in
a temporary folder and discards them.

```
go test ./tests                                  # every suite and every mutant
go test -short ./tests                           # the suites without the mutants
go test ./tests -run 'TestSuites/pages' -v       # one suite, with its count of expectations
go test ./tests -run 'TestMutants/.*facts' -v    # the mutants of one suite
LIFELOG_MERMAID=1 go test ./tests -run TestMermaidRender   # + render every mermaid diagram (needs mmdc + a Chromium)
```

The manually triggered [Windows validation workflow](../.github/workflows/windows-validation.yml) runs generation,
vet and the complete baseline, including mutants, on `windows-2025` using the Go version in `go.mod`.
It uses read-only repository permissions and does not publish a release.

Needs Go ≥ 1.27 and nothing else: SQLite is the driver's own build (`modernc.org/sqlite`, pure Go, SQLite 3.53.4 with
FTS5); the `writers` suite checks the floor of `lifelog_meta.sqlite` on it. The schema-evolution probes execute `ALTER TABLE …
DROP CONSTRAINT` on disposable files and need 3.53; they are not a migration runner. The full-file labels of
`doc-save-contract` need the FTS5 of that build to write its pending terms at a `SAVEPOINT` or a `RELEASE`; each label
fails loudly when the fault does not come. A writer needs only 3.51.3. The rendering test also needs
`npm i -g @mermaid-js/mermaid-cli` and a Chromium; point `MMDC` / `PUPPETEER_EXECUTABLE_PATH` at them if they are
not on `PATH`.

The suites are grouped by **subject**, not by when a rule was added. Each states its expectation in every label;
`-v` prints `<name>: X/Y met expectations`, and every expectation not met is a test failure. A suite that stops on
the document (a broken block makes a later step impossible) reports that as one failed expectation.

| file | suite | subject |
|---|---|---|
| `dates_test.go` | `dates` | instants, local days, the round-trip CHECKs and why `IS`, the zone (lifelog_meta.instants and .days, D10) |
| `schema_safeguards_test.go`, `schema_bounds_test.go` | `schema-safeguards` | NUL-free constrained text; the bounds of import keys, person names, units and notes, with the empty-key incident; a tombstone never precedes creation; `entities.body` last; the atomic Mood seed; no `OR IGNORE`/`OR REPLACE` in a trigger; immutable typed-detail identities and entity creation evidence; rowid-alias atomicity; Unicode search distinctions; cleanup excludes retained capture references |
| `schema_adversarial_test.go` | `schema-adversarial` | Skipped duplicate validation limits; statement ABORT versus caller transaction rollback |
| `graph_model_test.go` | `graph-model`, `FuzzGraphTransitions` | Independent directed graph model, NULL/empty notes, self-links, retry/edit/delete sequences, which endpoints a change advances (the page a link leaves, none for a wikilink's insert or delete), savepoint and transaction rollback, typed batch refusal and read-only reopen |
| `temporal_model_test.go` | `temporal-model` | Recursive category reads (cookbook/metrics-by-category) across tombstones, revivals, an alternate path and a cycle; an independent correction-chain model at clock cutoffs (cookbook/metric-series) |
| `storage_resilience_test.go` | `storage-resilience` | FTS term-position equality across trigger reordering, rejected edits, rollback, reopen and rebuild; WAL reader body/index snapshot isolation and checkpoint release |
| `connection_fixture_test.go` | `TestSchemaFixtureUsesLiteralFilename` | Literal URI-sensitive filenames in isolated schema fixtures and read-only reopen |
| `identity_fixture_test.go` | `TestCanonicalIdentityFixtureOwnershipAndRollback` | The canonical identity fixture: a named entity owns its name, and a refused insert rolls the whole identity back |
| `id_guards_test.go` | `id-guards` | file-backed declared/rowid/_rowid_/oid identity guards; forbidden combined edits, no-ops, revisions, FTS and reopen controls |
| `name_grammar_test.go` | `name-grammar` | documented raw/NFC addressability vectors under mutation; canonical key and bracket-storage probes, separate from production pipeline tests |
| `identity_test.go` | `identity` | the supertype and its composite FKs, ids by `RETURNING`, `source` on every row, no hard deletes, `updated_at` and the edit revisions: creation is not an edit, a link advances only the page it leaves (schema.sql, D8, D10, D11, D12) |
| `names_test.go` | `names`, `name-ownership` | mandatory deferred preferred ownership; direct reserved names and immutable owner/key/id; actual alias/preferred/spelling/body FTS maintenance and journal-name reservations |
| `named_test.go` | `named` | a person is a page: one id, promotion of a plain page only, cookbook/person, cookbook/days-that-name and cookbook/everything-about run literally, two Sams (D20) |
| `files_test.go` | `files` | a file is a page: one id, the hash, type and preview of the original and their CHECKs (`IS` for the JPEG bytes: `substr` of an empty blob is NULL), what never changes, never deleted, the graph (an embed, a caption, `part-of`, refused redirect), a ghost promoted, cookbook/keep-a-file as a writer runs it (D9) |
| `pages_test.go` | `pages` | every page titled, the day rule, filename-safe titles, normalized owned keys and their vectors, lookups, FTS, why not a collation (contract/titles-and-wikilinks, D5) |
| `renames_test.go` | `renames` | cookbook/rename-a-page run literally: id, prose, day, provenance, typed detail and incident links retained; direct aliases, owned-name selection, case-only rename and no-effect refusals; writer parity (contract/titles-and-wikilinks, D5) |
| `links_test.go` | `links` | the closed kind registry, endpoint types, immutability, `part-of`: a day page is never a category and never filed, and a filed page or a category never becomes a day page, with a name or with none yet (D8, D26) |
| `facts_test.go` | `facts`, `measurement-query-plan` | metrics and measurements: a scale's unit is a range in order and holds its values (D6); append-only, supersede, retract, finite values, never `OR IGNORE`/`OR REPLACE` (D7); categories as pages and cookbook/metrics-by-category (D26); the independent query-plan suite retains the 20,000-reading index-use and count probe plus retained-reference access paths and the day and backlink lookups (`entities_day`, `links_to`) |
| `journal_test.go` | `journal` | `entities.is_journal`, the one stored definition of a journal day page, with its SQLite premises (a CHECK and a trigger's NEW/OLD read a generated column); `entities.period` and the period pages of D30 (the four key shapes, the weeks that exist, the identity rules shared with a day page); the day page and capture (cookbook/capture) and a capture sent again with the same key (D28), the cookbook/day-view day view (a page is "(edited)" by its revision, not by comparing clocks), the days that name someone (cookbook/days-that-name), observed habits and birthdays, with no task, plan or recurrence rows (D5, D15, D22, D23) |
| `writers_test.go` | `writers` | the `BEGIN IMMEDIATE` race with real concurrent connections, pragmas, read-only readers and their `trusted_schema=OFF`, a hardened connection (contract/connections) |
| `integrity_test.go` | `integrity`, `physical-integrity` | semantic ownership, correction cycles, journal names, FK and FTS damage on small files; an independent fixture of 3,000 synthetic pages for zeroed pages, truncation, live-index damage and changed body bytes |
| `imports_test.go` | `imports` | the import block of contract/imports on 1 000 CSV rows, and its traps |
| `snapshots_test.go` | `snapshots` | cookbook/take-a-snapshot on a live file: `VACUUM INTO` through a read-only connection while the writer writes, its target, the restore check left byte for byte, the restore and the old `-wal` beside it (D25) |
| `evolution_test.go` | `evolution` | named CHECKs, widening, partial dates, the tokenizer switch, reader compatibility, a generated column that leaves the file once the partial index that names it is dropped, comments inside statements (D13, D17, architecture/non-goals) |
| `streak_test.go` | `streaks` | cookbook/streaks run literally on hand-checked days: a run of 3 ending today after a gap and an earlier run of 12 is 3; the run ends yesterday when today has no words; an empty page, a tombstone and an earlier run do not count; the look back stops at 3660 days |
| `cookbook_test.go` | `cookbook` | measurement lifecycle and recorded-time cutoffs; every cookbook block prepares and runs, on a plain and on a hardened connection; every block that only reads runs on a reader (`mode=ro`, `trusted_schema=OFF`) |
| `document_test.go` | `document` | the 2075 test, the `lifelog_meta` rows name only what the file has, the stored `CREATE` texts name only registered link kinds, the rules live in the file, the tree holds together (one record per decision, every relative link and anchor resolves, every page reachable from `docs/README.md`), the totals in `schema/README.md` |
| `diagrams_test.go` | `diagrams` | the eight mermaid diagrams say what the DDL says (keys, relationships, the link map, the correction story, the capture key) |
| `wikilinks_test.go` | `doc-save-contract` | the save contract as the docs print it: the vector table of contract/titles-and-wikilinks, cookbook/save-a-body run literally (and equal to a writer's own save after 400 random edits), a full file at `SAVEPOINT` or `RELEASE` that fails the save (and the links a save loses when it goes on), cookbook/backlinks |
| | `save-contract` | the save contract through a writer's own save against the DDL: invalid targets, the `SAVEPOINT` backstop, set equality, ordinary REDIRECT prose, revival, 400 random edits against a rebuild, 4 concurrent writers, every vector |
| | `title-fuzz` | writer acceptance is a subset of the DDL's filename checks over 60 000 generated strings; DB-only names independently fail reference addressability |
| `mutants_test.go` | `TestMutants` | 284 broken copies of the docs tree, one rule each; a completed owning suite must fail the mutant's explicit rule witness |
| `render_test.go` | `TestMermaidRender` | optional: every diagram renders |
| `kit_test.go`, `suites_test.go` | | reading the tree (a page, the cookbook blocks by recipe key, an overlay of broken files for a mutant); fresh databases and the insert conventions (entity first, `RETURNING`, named entities); the runner |

**The vectors have one home per subject**: the table of [titles and wikilinks](../docs/contract/titles-and-wikilinks.md)
and [exact time](../docs/contract/exact-time.md).
The wikilink suites read them from the page (with the boundary cases the line under the table states) and run them through the
writer's extraction (`text`) and its save (`internal/core`); `text` has its own tests against the
same table. Every save path here runs against the DDL under test, so a mutant of `schema.sql` reaches it.
Boundary probes roll back every case, including unexpected success under a mutant.

**What the suites take from the writer.** The suites keep their own kit (`kit_test.go`), not the writer's
`dbtest`: they build every database from the `schema.sql` under test, and a mutant changes that file, where the writer
embeds the file as it is. They import four packages of the writer, each for one reason, and no other:

| package | why |
|---|---|
| `text` | the rules that the contract gives a writer to compute: the title predicate, `title_key`, the wikilinks and the `#tags`; and the writer's checks of a day and an instant |
| `internal/core` | the writer's own save (a body, a capture, a kept file), run on the DDL under test |
| `internal/db` | the type that `core.Store` writes through, around the suite's own connection |
| `internal/photo/phototest` | the synthetic JPEG of a file's preview: the DDL checks that a preview starts with the bytes of a JPEG |

The suites do not reach the API, the CLI or the MCP server: what they check is the docs.

The graph model runs three deterministic 160-operation sequences in the baseline. Its separate fuzz target accepts
minimizable byte-encoded operations, caps each input at 64 transitions, and creates a fresh hardened file for every
input. Seeds run normally; a campaign is explicit:
`go test ./tests -run '^$' -fuzz '^FuzzGraphTransitions$' -fuzztime=30s -parallel=2`.
The temporal model compares 40 correction chains at 17 clock cutoffs with independent insertion-order histories.
These bounded models supplement the contract's literal vectors and rule mutants; agreement does not prove every
possible input or SQLite version correct.

**Fixture ownership.** Semantic fact and integrity mutants run their small behavior fixtures; the baseline
`measurement-query-plan` and `physical-integrity` suites own the large query-plan and byte-corruption fixtures.
Every original probe still runs in the baseline. Exact-time vectors share one baseline and roll back a savepoint
after each column probe, including a successful write under a mutant, so no case changes the next one's setup.

**When the docs change.** A DDL change: run everything. A new rule that spans tables: a `lifelog_meta` key and a row
in the 2075 table of contract/threat-model (`document` fails until both exist); a table's own rule: a comment in its
`CREATE` statement. A changed cookbook block or diagram: the suites that extract it will tell you. If a suite must
change because the docs legitimately changed, change it in the same edit and say so in the commit message — a suite
loosened to pass proves nothing. Add a mutant to `mutants_test.go` for a new rule, so the suite that owns it is shown
to notice when it breaks. Each mutant names an exact relevant expectation label in the registry; the runner retains
failed labels separately from diagnostic details and credits only that named failure in a normally completed suite,
after a clean baseline. Missing witnesses, unrelated failures and stopped suites receive no credit.
`TestMutantWitness` checks these false-credit controls, including an unchanged mutation.

The writer ordering probes use a zero-timeout lock refusal while the first transaction is open, then an explicit
commit signal before the second writer resolves the existing row. Reader progress is tested with a known open
snapshot and a 30-second deadlock guard, not a speed threshold. These probes do not measure automatic busy waiting.

**What is not here:** power loss and real data have never been tested.

The Linux storage-wrapper regression lives beside its implementation in
[`internal/db/io_failure_linux_test.go`](../internal/db/io_failure_linux_test.go).
Run `go test ./internal/db -run '^TestFilesystemWriteFailure$' -count=1` to exercise
child-local file-size limits that produce actual `SQLITE_IOERR_WRITE` errors, followed by
transaction rollback, reopen/retry and failed snapshot cleanup. This runs in the Linux baseline;
it does not simulate disk exhaustion, sync failures, torn writes or power loss.

## The coverage map

The coverage map shows which statements the tests of every package reach, the process of `lifelog` included. It is a
map, not a score ([AGENTS.md](../AGENTS.md)). The tests of `cmd/lifelog` run the CLI in-process; only
`TestExecutableServeDrainsActiveRequest` runs the program, because an interrupt needs a process. That test builds the
program with `go build -cover` and gives it the coverage folder of its test binary as `GOCOVERDIR`. So one folder
gets the coverage of every test binary and of the process, and `covdata` merges it:

```
dir=$(mktemp -d)
go test -cover -coverpkg=./... ./... -args -test.gocoverdir=$dir
go tool covdata textfmt -i=$dir -o $dir/cover.txt
go tool cover -func=$dir/cover.txt            # or -html=$dir/cover.txt
```

## Synthetic personas and scale

When adding generators, use these agreed workloads. They exercise existing capabilities, not new schema requirements
or a promise of performance. Generate from scratch, never from private exports or supposedly anonymized records.

| persona | behavior to exercise |
|---|---|
| daily journaler | decades of day pages, people, links, search and edits |
| measurement-heavy user | dense readings, habits, corrections, retractions and historical queries |
| mixed-media keeper | notes, long transcripts, document text and selected photo previews |
| messy importer | overlapping imports, duplicates, Unicode, malformed inputs, interruption and retries |

- **Small:** representative cases of all four personas for routine correctness tests. **Lifetime:** a 50-year mixed
  workload with 20 measurements/day, 3 files/day and about 4,000 imported notes in addition to day pages. **Stress:**
  explicitly increased density, long text, high-degree links and import bursts; record the parameters, not just "large".
- Make the logical dataset reproducible: fixed calendar anchor, seeds, generator version, operation order, payload-size
  distributions and known answers. Include gaps, bursts, popular and rare links/terms, corrections and backdated data,
  not just uniform append-only rows. On failure report the scenario, seed and operation; reduce it to a small regression.
- Exercise end-to-end workflows and persisted results, not just successful bulk insertion. Keep a simple independent
  oracle for expected queries/state. Label accelerated SQL loaders and compare manageable equivalent scenarios against
  writer-built fixtures. Do not claim a direct-SQL load measures application import throughput.
- Keep generators, small fixtures and regression inputs in git, not generated databases. Build databases in temporary
  or explicitly selected ignored scratch storage with cleanup. Keep large preview-heavy runs opt-in and report disk
  requirements; use valid synthetic payloads with representative sizes. Metadata-only runs are useful but must be
  labeled: tiny repeated blobs do not establish realistic preview/storage scale.

## Synthetic scale runner

`tools/lifescale` builds fresh file-backed databases through the production writer and emits a JSON report.
The normal baseline includes only the small fixture and its independent known answers; larger profiles are opt-in.

```
go run ./tools/lifescale -profile small -seed 2075 -samples 5
go run ./tools/lifescale -profile lifetime -seed 2075 -samples 5 -keep -storage "describe CPU, disk and filesystem"
go run ./tools/lifescale -profile stress -seed 2075 -samples 5 -keep -storage "describe CPU, disk and filesystem"
go run ./tools/lifescale -profile small -previews -seed 2075
```

Progress goes to stderr; redirect stdout to a JSON file outside the repository to retain measurements.
For recorded comparisons, build the runner with `go build -o /tmp/lifescale ./tools/lifescale` and run that binary
(choose an appropriate executable path on Windows). Build metadata records the revision and dirty state;
`go run` leaves the revision unavailable rather than identifying an unrelated checkout.
The runner creates a uniquely named subdirectory under the system temporary directory (or `-dir`), cleans it
afterward by default, and prints its location. `-keep` retains the synthetic database, snapshot and restored copy.
It never opens an existing user database. Ctrl-C cancels construction and normal cleanup still runs.

Profiles have fixed parameters: **small** is 14 days, four readings/day, one file/day and eight imported notes;
**lifetime** is 1970-01-01 through 2019-12-31 (18,262 days), 20 readings/day, three files/day and 4,000 notes;
**stress** uses the same calendar, 80 readings/day, six files/day, 16,000 notes and four times the text sizes.
Recorded dates include gaps, bursts and later writes of older facts. The fixture includes a journal, person
references, a popular topic, rare search terms, a habit, Unicode notes, keyed retries, value corrections,
retractions and a malformed batch with a valid prefix that must roll back. Notes arrive in batches of 50.
The JSON records generator version, seed, payload-size schedule and a digest of the ordered logical inputs;
write-clock timestamps are intentionally excluded from reproducibility.

Default runs are explicitly **metadata-only**: files have unique synthetic text originals and no previews.
Allow several GB of free space for lifetime metadata runs and substantially more for stress, including the live
database/WAL plus snapshot and restored copies. These are planning allowances, not measured storage results.
`-previews` instead generates unique valid JPEG images at 640×480, 1024×768 and 1600×900, quality 75.
A lifetime preview run can require tens of GB per database and over 100 GB of free scratch space for all copies;
stress previews require more. Preview generation and decoding also increase run time. Small previews establish
correctness, not lifetime storage capacity. Do not run large preview profiles without choosing adequate scratch storage.

The report includes Go/SQLite/driver/revision, OS/architecture/CPU, operator-supplied storage details, writer
pragmas, Go allocation deltas, raw timing samples and database/WAL/SHM/copy sizes. Fixture construction is timed
separately from capture/save, day view, rare search, popular backlinks, historical metric series and keyed root
replay. The latter measures `core.Record` retries, **not** the throughput of an import. Capture/save includes
commit; each sample performs a real new capture/edit. Queries are warm after fixture validation. Snapshot and
copy/open restore each run once, regardless of `-samples`; restored contents and integrity are checked outside
timing. Compare repeated reports under the same environment, and read them by the rules of
[performance evidence](#performance-evidence-not-speculative-optimization).

## Performance evidence, not speculative optimization

- Measure important workflows at small and lifetime sizes: capture/save, day view, search/backlinks, historical
  measurements, imports and snapshot/restore. Check correctness as well as cost. Establish reproducible baselines
  before adding caches, indexes or concurrency; contract/schema changes still need the normal evidence and approval.
- Keep fixture construction and correctness-oracle work outside timed query operations. Prefer `for b.Loop()` for new
  Go benchmarks; report allocations. Fully consume results and check errors. Define whether preparation, commit,
  decoding or preview creation is included. Reset mutable state outside timing or measure an explicitly bounded growth
  trace; repeated iterations must not silently turn writes into no-ops or change the workload.
- Compare repeated samples with `benchstat`, not one run; profile with `pprof` before optimizing. Every benchmark runs
  with `go test ./... -run '^$' -bench . -benchmem -count=10`; none exists today (the import's went with it, plan 089).
- Record scenario/seed, revision, Go and SQLite/driver versions, OS, hardware/storage, connection/maintenance settings
  and cache conditions. Report latency, allocations, import throughput and storage (database, WAL/SHM and relevant
  temporary files). Go allocation counts are not total process memory. Label warm versus fresh-process runs; do not
  call a reopened database a cold-disk measurement without controlling the OS cache.
- Use query plans and work counts to investigate scaling, not brittle exact `EXPLAIN QUERY PLAN` text assertions.
  No universal wall-clock CI gate: introduce a hard budget only for an agreed usability requirement on a defined
  environment. The 50-year workload is a test envelope, not evidence that any implementation already meets it.

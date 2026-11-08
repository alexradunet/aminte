# 073 — Schema, reliability and validation repairs

- **Date / baseline:** 2026-10-07, `6ae8022d611907d56dcac5cecaaf02df0374b32d`.
- **Status:** DONE.
- **Authority:** owner requested “lets fix everything” after the schema-first review.
- **Scope:** the six original defects, literal-copy-path and initialization-statistics defects found during validation,
  recovery/scale evidence gaps, test-fixture and query redundancy, and stale guidance.

## Repairs and acceptance

1. Reserve snapshot filenames without following existing symlinks; prove no overwrite and ownership-safe cleanup.
   Keep relative filenames beginning `file:` literal when copying through SQLite.
2. Drain HTTP handlers before database shutdown, with a bounded lifetime and process tests.
3. Reject embedded NULs in constrained text; share measurement-zone validation across record and relocation.
4. Refuse place-point identity transfers through every rowid alias and preserve revisions on refusal.
5. Exclude retained session-kind and measurement-capture references from ghost cleanup.
6. Stop prepared imports at cancellation boundaries; prove rollback independently of callback scheduling.
7. Kill synthetic import subprocesses at durable publication/commit boundaries and recover in fresh processes.
8. Add reproducible small, lifetime and stress workloads with independent answers and explicitly labelled media costs.
9. Preserve every contract assertion while separating unrelated expensive mutation fixtures; remove obsolete entity self-joins.
10. Reconcile current guidance and add useful parsing/storage fuzz properties without dependencies.
11. Avoid collecting misleading planner statistics from the single seeded entity before bulk capture begins.

## Validation

Observe focused regression failures before fixes where practical. Run generation, vet and the full suite, fixed-seed
shuffle, Linux race checks, bounded fuzz campaigns and relevant scale/benchmark runs. Record exact results here.
Do not weaken mutant witnesses or drop coverage to meet a runtime target. Missing optional tools and unavailable
platform validation remain explicit. All data and subprocesses are synthetic and disposable.

### Implementation evidence

The six defects have regression tests that failed before their fixes. Transaction SQL now carries its owning
operation's context; a separate canceled-lookup regression also failed before that change. Import interruption
tests kill children at eight durable boundaries and verify recovery and retry in separate processes. These are
process-interruption checks, not power-loss simulations.

Final review also reproduced a literal-path failure in copying: `file:copy.db` was reserved as a filename, but
SQLite interpreted it as a URI and wrote `copy.db` instead. Making the destination absolute before reservation
and `VACUUM INTO` keeps both operations on the same literal path. The regression failed before the fix and now
verifies both the valid copied database and the unchanged alternate file.
`go test ./internal/db -run 'TestCopy|TestLiteralDatabasePaths' -count=1` passed. The colon-filename case applies
to Unix and skips Windows. The same focused tests passed with `TMPDIR=/var/tmp CGO_ENABLED=1 go test -race
./internal/db -run 'TestCopy|TestLiteralDatabasePaths' -count=1` after the fix.

The schema safeguard suite adds 14 rule mutants, for 295 total. Large query-plan and physical-corruption fixtures
have separate owning suites; date vectors use nested savepoints inside an immediate transaction. Existing mutant
witnesses and normal-completion controls remain intact. Duplicate entity self-joins and the unchecked duplicate
import-key lookup are removed. No dependency or toolchain changes were made.

### Focused checks

These commands used Go 1.27.1 on Linux amd64 with modernc.org/sqlite v1.60.1 (SQLite 3.53.4).
The shell prepended `/home/alex/.local/share/mise/installs/go/1.27.1/bin` to `PATH`. `TMPDIR=/var/tmp` is used where
snapshot tests need a destination outside a Git tree: this machine's `/tmp` has a `.git` marker.

- `go test ./internal/core ./tests -run 'TestMeasurementZone|TestSuites/schema-safeguards' -count=1`: passed.
- `GOMAXPROCS=1 go test ./internal/core -run '^TestTransactionOperationsStopAfterCancellation$' -count=1`: failed
  before context propagation, passed afterward; the complete core and importer packages then passed with
  `TMPDIR=/var/tmp go test ./internal/core ./internal/importer -count=1`.
- `GOMAXPROCS=1 go test ./internal/importer -run '^TestPreparedCancellationAfterObservedWriteAndPublicationRecovery$' -count=100 -shuffle=1791357917233312471`: passed after reproducing the old failure with that seed.
- `go test ./internal/importer -run '^TestImportProcessRecovery' -count=1 -v`: all eight boundaries passed.
- `TMPDIR=/var/tmp go test ./internal/db ./cmd/aminte -run 'TestCopy|TestInit|TestSnapshot|TestExecutableServeDrainsActiveRequest|TestServeShutdown|TestServeFailure|TestServeBinds' -count=1`: passed.
- `TMPDIR=/var/tmp go test ./cmd/aminte -run 'TestExecutableServeDrainsActiveRequest|TestServeShutdown|TestServeFailure' -count=10`: passed.
- `GOMAXPROCS=1 go test ./tests -run 'TestMutants/.*(facts|dates|integrity)|TestMutantWitness|TestSuites/document' -count=1 -parallel=1 -json`: passed; all 45 affected mutants were killed at their named witnesses.
- `go test ./internal/core -run '^$' -fuzz '^FuzzPeriodMembership$' -fuzztime=60s -parallel=2`: passed, 470,664 executions.
- `go test ./internal/importer -run '^$' -fuzz '^FuzzSourceJSONIdentity$' -fuzztime=60s -parallel=2`: passed, 66,068 executions.
- `TMPDIR=/var/tmp go test ./internal/importer -run '^$' -bench '^(BenchmarkResolveReadingKeys|BenchmarkReadingSourceCheck|BenchmarkBuildCorrectionProof)$' -benchmem -benchtime=200ms -count=5`: passed, 45 samples. Other development work overlapped; these are correctness-checked observations, not an optimization claim.

The first combined baseline caught two mutant interactions: a source mutation targeted the old constraint text,
and the title-NUL witness was rejected by the new key guard before it exercised the title guard. The mutation now
targets its column declaration, and the title witness uses a valid NUL-free alternate-writer key with its own
positive control. Both mutants fail their intended witnesses, and the complete baseline then passed.

Generation, vet and `TMPDIR=/var/tmp go test -count=1 ./...` passed, including all 295 mutants. The incident index
now retains resolved dated records consistently with [the process](../process.md); older proposed statuses are unchanged.
`TMPDIR=/var/tmp go test -count=1 -shuffle=1791357917233312471 ./...` also passed with the original failing seed.
`TMPDIR=/var/tmp CGO_ENABLED=1 go test -race -count=1 -timeout=20m ./...` passed; the contract package completed
in 716.739 seconds. The timeout accommodates instrumented SQLite work rather than dropping the contract suites.

The preview CLI check passed with `go run ./tools/lifescale -profile small -previews -seed 2075 -samples 5`
(plus a storage-description flag). It generated 14 distinct JPEG originals/previews totaling 6,783,325 stored preview
bytes. Snapshot and restored files were each 7,221,248 bytes. Exact content/identity checks, malformed-batch rollback,
journal rendering, capture/edit persistence and integrity all passed. The runner cleaned its scratch directory.
This is small-preview correctness evidence; the large profiles use metadata only.

The first lifetime and stress runs were interrupted before completion and are not passing scale evidence.
Their slowdown prompted a controlled diagnostic on a copied 172,566-reading fixture. Five rollback samples
per operation, repeated without/with/without an experimental capture-provenance index, showed no improvement:
capture remained about 1.7–2.6 ms, file insertion 0.9–1.1 ms and reading insertion 0.3–0.4 ms. Five committed
day batches took 10–14 ms. The diagnostic copy was on `/tmp` tmpfs, whereas the original construction ran on
`/var/tmp` btrfs on an encrypted SSD; these are not equivalent durable-storage timings. No index was added to
the schema. CPU-profile attempts were killed by SIGKILL, including outside the sandbox, before writing a profile;
the cause is unknown. Temporary repository probes were removed. These observations justify rejecting the
proposed optimization, not a performance claim.

The fresh run exposed the actual cause: initialization collected statistics for the one seeded entity and name.
After tens of thousands of inserts, those estimates still made core queries scan both tables. Closing a diagnostic
connection refreshed the copy's statistics, explaining why subsequent probes were faster. A controlled fresh-copy
comparison (19,865 entities, 112,339 readings) reproduced capture at 66–73 ms and file insertion at 30–35 ms;
after refreshing statistics on the same connection, five samples were 1.8–2.4 ms and 1.2–1.8 ms respectively.
Removing statistics on a disposable copy also produced indexed searches and comparable timings. These bounded
samples and query plans establish the failure mechanism; benchstat remains unavailable. The application fix
removes initialization-time optimization; normal connection-close maintenance stays in place. No schema index,
dependency or maintenance setting was added. The slow fresh runs were canceled normally and cleaned their
temporary directories before restarting with the fix.

`go test ./internal/core -run '^TestNewDatabaseReadingLookupUsesIndexes$' -count=1` failed before that change
with scans of both entity tables, then passed with indexed searches. It builds 200 pages through the writer and
keeps the connection open to expose the initialization state; it does not pin index names or entire plan text.
After the fix, `go generate ./...`, `go vet ./...` and `TMPDIR=/var/tmp go test -count=1 ./...` passed again
(contract package: 41.897 seconds). The final affected-package check,
`TMPDIR=/var/tmp CGO_ENABLED=1 go test -race -count=1 -shuffle=1791357917233312471 ./internal/db ./internal/core ./internal/scaletest`,
also passed. The earlier whole-repository race and shuffle checks are supplemented by these checks of the final changes.

Final scale-runner review tightened the day-view oracle to reject duplicate reading IDs, clarified that preview
embeds can create days without journal captures, and removed working-directory Git introspection. A built runner
records its own VCS build metadata; `go run` reports an unavailable revision. Focused scale tests passed.

### Completed large workloads

The final runs use a binary built with `go build -o /tmp/aminte-review-mqVUI5uI/lifescale ./tools/lifescale`.
It records baseline revision `6ae8022d611907d56dcac5cecaaf02df0374b32d`, dirty build state, Go 1.27.1,
modernc.org/sqlite v1.60.1 / SQLite 3.53.4, Linux amd64, and AMD Ryzen 7 7840HS. Scratch storage is btrfs on
the encrypted SSD `/dev/mapper/root`; default writer settings are recorded in each JSON. Both profiles use seed
2075, generator version 1, no previews, and warm queries. The profiles and validation checks overlapped, so raw
timings are observations under contention, not an isolated benchmark comparison.

`/tmp/aminte-review-mqVUI5uI/lifescale -profile lifetime -seed 2075 -samples 5 -dir /var/tmp`
(plus the recorded `-storage` description) passed. Construction took 188.580 seconds: 18,262 days,
365,240 root readings, 21,485 value corrections, 11,782 retractions, 54,786 files and 4,000 imported notes.
The logical digest is `b1bf076df011f4591427dbc931d33c62713531db436cb45fee42b1284f9eb502`.
All independent answers, rollback, nine workflows, integrity and restored-state checks passed. The live database
was 407,474,176 bytes with 13,015,112 WAL bytes; snapshot 403,365,888 and restored file 403,374,080 bytes.
The runner cleaned its scratch directory. Full report: `/tmp/aminte-review-mqVUI5uI/lifetime-final.json`.

The same invocation with `-profile stress` passed. Construction took 606.437 seconds: 1,460,960 root readings,
85,939 value corrections, 47,128 retractions, 109,572 files and 16,000 imported notes, with four times the text sizes.
The logical digest is `e604db2dc8678d37ffb735b9d278b8b16926b7dbafdc909d937fd04bba9d353c`.
All independent answers, rollback, nine workflows, integrity and restored-state checks passed. The live database
was 3,268,595,712 bytes with 193,916,072 WAL bytes; snapshot 3,253,223,424 and restored file 3,253,231,616 bytes.
The runner cleaned its scratch directory. Full report: `/tmp/aminte-review-mqVUI5uI/stress-final.json`.

Staticcheck, govulncheck and benchstat are not installed, so those checks were not run. No tools were installed.
Windows execution, power loss, real private imports and large JPEG-preview profiles are outside this validation run.

## Boundaries

Edit canonical DDL in place; no migrations, freeze, real-data replay, dependency additions or publication.
Follow the [change process](../process.md) for the observed schema incidents. This work does not change the
acceptance status of [071](071-audit-followups.md), [072](072-stable-names-candidate.md) or
[RFC 0006](../rfcs/0006-stable-names-and-life-periods.md).

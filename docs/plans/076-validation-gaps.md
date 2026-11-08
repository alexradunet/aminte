# 076 — Exercise remaining validation gaps

- **Date:** 2026-10-07
- **Status:** DONE
- **Baseline:** `6f2bfe8` (committed and pushed).
- **Authorization:** owner requested continuing the remaining validation gaps after plan 075, while retaining commit/push authorization.
- **Scope:** pinned static analysis, full race checks, minimum SQLite compatibility, actual Windows execution, lifetime/stress evidence and bounded storage failure probes. No migrations or private data.

## Completion criteria

Run the checks the available environment supports; fix reproducible findings with regressions. Use reviewed pinned development tools in isolated scratch storage and leave application dependencies unchanged unless a finding requires an explained update. Record commands, tool/engine versions, results and the boundary of every simulated failure. Real power-loss proof requires a suitable separate test environment and must not be inferred from process interruption, capacity limits or I/O errors.

Keep large generated databases outside git on explicitly selected scratch disk. Lifetime previews need a justified storage/time budget; metadata-only workload results must be labeled. Windows execution must use Windows, not a cross-compiled executable. Any CI workflow added for this purpose is explicit and minimal.

## Work and evidence

### Static analysis and storage failures

Reviewed development tools were installed in `/tmp/aminte-analysis-tools`, outside application dependency files:
Staticcheck **2026.2.1 / v0.8.1** and govulncheck **v1.8.0**, using Go **1.27.1**.
The [Staticcheck release notes](https://staticcheck.dev/changes/2026.2/) establish Go 1.27 support;
the [pinned release](https://github.com/dominikh/go-tools/releases/tag/2026.2.1) and
[govulncheck source tag](https://go.googlesource.com/vuln/+/refs/tags/v1.8.0) provide reproducible tool versions.
No application dependency or persistent tool configuration changed.

All 17 Staticcheck findings were fixed: unused private wrappers and assignments, redundant checks and repeated type assertions.
Both independent NULL-key insert attempts remain in the identity test. Markdown traversal retains its node and line-break boundaries.
The final Staticcheck run passed without suppressions. Govulncheck's default text mode exited successfully with
“No vulnerabilities found”; this checks known reachable vulnerabilities, not arbitrary unknown defects.

The Linux `TestFilesystemWriteFailure` regression uses subprocess-local `RLIMIT_FSIZE` and an independent `EFBIG` control.
It requires the structured SQLite `SQLITE_IOERR_WRITE` result, then checks partial-transaction rollback, unchanged
body/names/revision/timestamp/FTS, reopen and successful retry. The snapshot case verifies partial-destination removal,
unchanged source, retry and restored contents. These cases passed before production changes; no storage-wrapper defect
was found. This is real filesystem write-error injection, not ENOSPC, xSync failure, torn writes or power loss.

### Compatibility and scale

SQLite **3.51.3** passed all **39 subject suites and 404 mutants** in an isolated module snapshot, with a narrow
driver adapter and explicit newer-engine evolution exclusions. The appendix preserves the reproduction and its limits.

A manual Windows workflow runs generation, generated-copy verification, vet and the full baseline on `windows-2025`.
Actions are pinned to reviewed commits with read-only repository permissions.
The [first Windows run at 5543bc1](https://github.com/alexradunet/aminte/actions/runs/37635367862)
passed generation, vet and every package except two CLI snapshot assertions. Both compared the caller's short parent
path (`RUNNER~1`) to the correctly resolved long path (`runneradmin`). Snapshot creation and restore succeeded;
the tests must compare the exact dated leaf and filesystem identity, preserving all literal-name sentinels and
symlink safety checks. No production path behavior changes.

The [full Windows rerun at e44b8f5](https://github.com/alexradunet/aminte/actions/runs/37644715040)
passed generation, generated-copy verification, vet and every package, including **39 suites and 404 mutants**.
The log confirms both repaired snapshot cases, all physical-destination symlink/junction cases and source-confinement
symlink/junction cases actually ran and passed. Environment: Go **1.27.1 windows/amd64**, hosted runner image
`windows-2025-vs2026` version `20260925.250.1`, current pinned SQLite 3.53.4 / driver v1.60.1.
The exact baseline command was `go test -v -count=1 -timeout=30m -parallel=4 ./...`;
contract tests took 904.660 seconds and importer tests 1125.706 seconds.

Expected Windows skips were the subprocess helper entry points, optional Mermaid rendering, the literal-colon filename
case (invalid on Windows), a trailing-space filename the filesystem cannot preserve, and the executable interrupt/drain
case because sending `os.Interrupt` to another Windows process is unsupported by that harness. The Linux baseline covers
the latter filesystem/signal cases and Linux-only write-error injection; this run does not prove Windows console shutdown.

The first Windows contract package passed all suites and mutants in 889.908 seconds; importer tests took
1096.961 seconds. The workflow now prints individual test results and skips, and allows 30 minutes per package
and 45 minutes per job to leave headroom over this observed Windows runtime. No test or mutant is removed;
timeouts guard stalled runs rather than impose an unagreed speed requirement.

### Scale workloads

The existing production-writer scale runner was built at `6f2bfe8`, Go 1.27.1, modernc.org/sqlite v1.60.1 / SQLite 3.53.4.
Its dirty flag comes from pre-existing untracked Python caches; tracked source was clean when built. Runs use seed 2075,
generator version 1, a fixed `1970-01-01` calendar anchor and automatically cleaned `/var/tmp` storage. Hardware:
AMD Ryzen 7 7840HS, Linux amd64, SKHynix HFS001TEJ9X115N NVMe, btrfs on encrypted `/dev/mapper/root`.
Connection settings were WAL, `synchronous=FULL`, foreign keys and recursive triggers enabled, `trusted_schema=OFF`,
4096-byte pages, a 5000 ms busy timeout and a 1000-page automatic checkpoint.

All four runs passed independent metric identities/values/days/order, exact file/preview totals, search/backlinks,
malformed-batch rollback, all four integrity groups and the same known answers after snapshot restore.

- **Lifetime metadata:** 18,262 days, 365,240 original readings, 21,485 corrections, 11,782 retractions,
  54,786 files and 4,000 notes. Build 251.034 seconds.
  Logical digest `b1bf076df011f4591427dbc931d33c62713531db436cb45fee42b1284f9eb502`.
- **Small previews:** 14 days, 56 original readings, four corrections, two retractions, eight notes and
  14 unique valid JPEGs containing 6,783,325 preview bytes. Build 0.801 seconds.
  Logical digest `db17e1a0f197a1b2d68e3cad84030bbcac19cb2cc61cfcb4d5698293cac586b2`.
- **Metadata stress:** 18,262 days with 80 original readings and six files per day; 1,460,960 original readings,
  85,939 corrections, 47,128 retractions, 109,572 files, 16,000 notes and four times the text sizes.
  Build 980.335 seconds.
  Logical digest `e604db2dc8678d37ffb735b9d278b8b16926b7dbafdc909d937fd04bba9d353c`.
- **Lifetime previews:** the lifetime workload above with 54,786 unique synthetic JPEGs at 640×480, 1024×768
  and 1600×900, quality 75. Payloads total 27,964,022,156 bytes, averaging approximately 510,423 bytes per file.
  Build 3,895.506 seconds; the full run finished at 15:29:13 UTC, approximately 69 minutes 45 seconds after launch.
  Logical digest `5689695f35c43a13f9ae51d0f70a606544d3e8846337e968daa6574b21492e7f`.

The logical digests cover ordered generated payloads, excluding write-clock timestamps. Build times include generation,
JPEG encoding where selected, writer transactions, corrections and independent-oracle bookkeeping. Other validation
ran alongside the metadata and small-preview runs. Early lifetime-preview construction overlapped race/fuzz checks;
the full local contract race finished at 14:35:45 UTC, leaving later construction and warm measurements with less
validation contention. These are workload characterization results, not isolated comparative benchmarks.

Warm workflow ranges below are minimum–maximum milliseconds across five samples. Queries run after construction
and validation; reopening a database does not establish a cold-disk measurement.

| Workflow | Small previews | Lifetime metadata | Metadata stress | Lifetime previews |
|---|---:|---:|---:|---:|
| Capture and commit | 2.608–9.647 | 2.194–15.155 | 2.980–7.410 | 2.902–5.166 |
| Save and commit | 2.057–9.739 | 1.302–1.457 | 1.944–5.154 | 1.286–1.687 |
| Day view | 0.517–0.791 | 0.572–0.838 | 1.898–5.159 | 0.625–0.678 |
| Rare search | 0.208–0.399 | 26.354–29.263 | 97.836–229.819 | 29.592–31.495 |
| Page with popular backlinks | 0.257–0.350 | 96.591–109.338 | 778.761–1464.459 | 103.042–108.265 |
| Historical metric series | 0.168–0.266 | 231.576–241.255 | 1551.441–3380.338 | 254.284–269.296 |
| Keyed core replay, up to 128 roots | 9.042–10.787 | 14.898–21.878 | 30.729–110.194 | 15.979–21.166 |

Representative Go allocation costs per sample show the cost of fully consuming large result sets. These counters
measure allocations during each operation, not live heap size or total process memory.

| Workflow | Lifetime previews: bytes / allocations | Metadata stress: bytes / allocations |
|---|---:|---:|
| Day view | 25,504–25,648 / 357–360 | 83,312–83,408 / 1,071–1,073 |
| Page with popular backlinks | 10,727,248 / 157,309 | 17,688,896–17,689,024 / 253,333–253,335 |
| Historical metric series | 91,001,616–91,001,648 / 618,370 | 359,265,728–359,265,904 / 2,473,437–2,473,439 |
| Keyed core replay, up to 128 roots | 491,488–492,768 / 11,790–11,803 | 491,504–492,016 / 11,787–11,792 |

Snapshot and restore timings are one sample each, in seconds. They include copy/open work; correctness and integrity
checks run outside the timed portion. Keyed replay measures core recording, not workspace/source-file import throughput.

| Operation | Small previews | Lifetime metadata | Metadata stress | Lifetime previews |
|---|---:|---:|---:|---:|
| Snapshot copy | 0.038 | 1.043 | 9.960 | 57.037 |
| Restore copy/open | 0.043 | 1.021 | 10.430 | 57.479 |

Recorded file sizes are bytes, before automatic cleanup. These are file lengths, not peak filesystem allocation or
an inventory of SQLite's transient internal files. Metadata-only runs make no preview-capacity claim.

| File | Small previews | Lifetime metadata | Metadata stress | Lifetime previews |
|---|---:|---:|---:|---:|
| `life.db` | 7,331,840 | 407,543,808 | 3,268,665,344 | 28,439,715,840 |
| `life.db-wal` | 4,532,032 | 13,015,112 | 193,916,072 | 16,142,192 |
| `life.db-shm` | 32,768 | 32,768 | 393,216 | 32,768 |
| `snapshot.db` | 7,282,688 | 403,435,520 | 3,253,293,056 | 28,434,440,192 |
| `restored.db` | 7,290,880 | 403,443,712 | 3,253,301,248 | 28,434,448,384 |
| `restored.db-wal` | 0 | 0 | 0 | 0 |
| `restored.db-shm` | 0 | 0 | 0 | 0 |

The lifetime-preview files above sum to 85,324,779,376 bytes. That run started with approximately 651 GiB free;
all scratch copies were removed automatically, and its process exited zero. No generated database or large log is
committed. Exact runner invocations from the repository root were:

```sh
/tmp/aminte-gap-lifescale -profile lifetime -seed 2075 -samples 5 -dir /var/tmp -storage 'AMD Ryzen 7 7840HS; Linux amd64; SKHynix HFS001TEJ9X115N NVMe; btrfs on encrypted /dev/mapper/root; warm writer-built fixture; concurrent repository validation' > /tmp/aminte-gap-lifetime.json 2> /tmp/aminte-gap-lifetime-progress.log
/tmp/aminte-gap-lifescale -profile small -previews -seed 2075 -samples 5 -dir /var/tmp -storage 'AMD Ryzen 7 7840HS; Linux amd64; SKHynix HFS001TEJ9X115N NVMe; btrfs on encrypted /dev/mapper/root; warm writer-built fixture; concurrent repository validation and stress fixture build' > /tmp/aminte-gap-small-previews.json 2> /tmp/aminte-gap-small-previews-progress.log
/tmp/aminte-gap-lifescale -profile stress -seed 2075 -samples 5 -dir /var/tmp -storage 'AMD Ryzen 7 7840HS; Linux amd64; SKHynix HFS001TEJ9X115N NVMe; btrfs on encrypted /dev/mapper/root; warm writer-built fixture; concurrent repository validation' > /tmp/aminte-gap-stress.json 2> /tmp/aminte-gap-stress-progress.log
/tmp/aminte-gap-lifescale -profile lifetime -previews -seed 2075 -samples 5 -dir /var/tmp -storage 'AMD Ryzen 7 7840HS; Linux amd64; SKHynix HFS001TEJ9X115N NVMe; btrfs on encrypted /dev/mapper/root; warm writer-built fixture; concurrent validation' > /tmp/aminte-gap-lifetime-previews.json 2> /tmp/aminte-gap-lifetime-previews-progress.log
```

The scratch binary can be rebuilt from the recorded revision with
`/home/alex/.local/share/mise/installs/go/1.27.1/bin/go build -o /tmp/aminte-gap-lifescale ./tools/lifescale`.
The four JSON reports retain individual timing samples, allocation counts, environment, settings, manifests and checks.
These runs establish the stated synthetic envelope on this machine; they are not universal capacity or power-loss proof.

### Bounded fuzz campaigns and progress diagnosis

At `5543bc1`, Go 1.27.1 / Linux amd64, both existing targets ran for 30 seconds,
sequentially with one worker and synthetic seeds. `FuzzRead` passed **2,054,237
executions** in 30.024 seconds; it checks parser crashes, not metadata semantics.
`FuzzSourceJSONIdentity` passed **1,188 executions** in 31.034 seconds, but its
counter stopped advancing after three seconds. That observation was investigated.

A fresh-corpus run with `GODEBUG=fuzzdebug=1` reproduced the stall at **24,374
executions**. Its log identifies a coverage-minimization task (`198cb250`,
`keepCoverage=true`, `crasher=false`) outstanding until the campaign deadline.
The local Go 1.27.1 sources, also available in the official
[fuzz coordinator](https://raw.githubusercontent.com/golang/go/go1.27.1/src/internal/fuzz/fuzz.go) and
[testing defaults](https://raw.githubusercontent.com/golang/go/go1.27.1/src/testing/fuzz.go), show that coverage
discoveries are minimized under the default 60-second budget and displayed
counts update when worker results arrive (`updateStats`, line 722).
With the same binary and assertions, disabling minimization passed **103,981
executions / 277 new interesting inputs** with continuous progress; limiting
minimization to 100 ms passed **73,925 executions / 164 new interesting inputs**,
also with continuing progress. Ordinary direct replay of the 164 retained inputs from that last run,
including 51 originals returned after interrupted minimization, plus six seeds
completed in 0.02 seconds: 146 cases passed and 24 were skipped by the existing
bounded-input/Unicode guards. The replay command passed. These controls locate the
reproduced stall in coverage minimization; no slow or hanging validator input was
reproduced. The original untraced run cannot identify its particular input.
No assertion, production code or permanent fuzz setting changed.

Successful commands (from the repository root unless `cd` selects another path):

```sh
GOCACHE=/tmp/aminte-analysis-tools/build-cache /home/alex/.local/share/mise/installs/go/1.27.1/bin/go test ./internal/photo -run '^$' -fuzz '^FuzzRead$' -fuzztime=30s -parallel=1
GOCACHE=/tmp/aminte-analysis-tools/build-cache /home/alex/.local/share/mise/installs/go/1.27.1/bin/go test ./internal/importer -run '^$' -fuzz '^FuzzSourceJSONIdentity$' -fuzztime=30s -parallel=1
GOCACHE=/tmp/aminte-analysis-tools/build-cache /home/alex/.local/share/mise/installs/go/1.27.1/bin/go test ./internal/importer -c -fuzz '^FuzzSourceJSONIdentity$' -o /tmp/aminte-source-json-fuzz.test
cd internal/importer
GODEBUG=fuzzdebug=1 /tmp/aminte-source-json-fuzz.test -test.run '^$' -test.fuzz '^FuzzSourceJSONIdentity$' -test.fuzztime=30s -test.parallel=1 -test.fuzzcachedir=/tmp/aminte-source-json-fuzz-debug-cache
GODEBUG=fuzzdebug=1 /tmp/aminte-source-json-fuzz.test -test.run '^$' -test.fuzz '^FuzzSourceJSONIdentity$' -test.fuzztime=30s -test.fuzzminimizetime=0 -test.parallel=1 -test.fuzzcachedir=/tmp/aminte-source-json-fuzz-no-min-cache
GODEBUG=fuzzdebug=1 /tmp/aminte-source-json-fuzz.test -test.run '^$' -test.fuzz '^FuzzSourceJSONIdentity$' -test.fuzztime=30s -test.fuzzminimizetime=100ms -test.parallel=1 -test.fuzzcachedir=/tmp/aminte-source-json-fuzz-bounded-min-cache
# The 164 cached corpus files were copied into this scratch directory's
# testdata/fuzz/FuzzSourceJSONIdentity/ before direct replay.
cd /tmp/aminte-source-json-replay-scevlf_r
/tmp/aminte-source-json-fuzz.test -test.run '^FuzzSourceJSONIdentity$' -test.timeout=20s -test.v
```

Logs: `/tmp/aminte-gap-fuzz-photo.log`, `/tmp/aminte-gap-fuzz-source-json.log`
and `/tmp/aminte-source-json-fuzz-{debug,no-min,bounded-min,replay}.log`.
Every command exited zero. Counts are engine-reported executions, not unique
admitted inputs; bounded and invalid-Unicode skips remain part of the target.
These short campaigns extend evidence without proving exhaustive input coverage.

## Validation and remaining limits

Passed on Linux:

- `go generate ./...` and `go vet ./...`; generated schema unchanged.
- `TMPDIR=/var/tmp go test -count=1 ./...`: all packages, including the new write-failure tests and 404 mutants;
  contract package 93.085 seconds.
- `TMPDIR=/var/tmp CGO_ENABLED=1 go test -race -count=1 -timeout=20m ./...` at `6f2bfe8`:
  all packages passed; contract package 930.209 seconds.
- The same full race command with the final Go edits passed every application package, including the new
  Linux write-failure tests; its contract package timed out as recorded below.
- The complete contract race retry at `5543bc1`, with `-parallel=4 -timeout=40m -json`, exited zero in
  **1148.387 seconds**. All 39 subject suites, 404 mutants, fixture/witness tests and three graph-fuzz seed
  cases passed. Only the standalone subprocess helper and opt-in Mermaid renderer skipped; no race was reported.
- `TMPDIR=/var/tmp go test -count=1 -shuffle=on ./...`: all packages passed with the final Go edits;
  contract package 162.732 seconds while concurrent validation and scale work ran.
- `staticcheck ./...` using v0.8.1: no findings; `govulncheck ./...` and final `govulncheck -test ./...`
  using v1.8.0 in text mode: no known vulnerabilities, exit zero.
- Write-failure tests repeated 20 times and under the race detector; all affected package tests and identity/wikilinks/files suites.
- SQLite floor matrix and supporting fixtures/seeds, as detailed below.
- Document suite and `git diff --check`.

After repairing the two Windows path-identity assertions, generation and vet passed again, the full Linux baseline
passed (contract package 97.146 seconds), and Staticcheck remained clean. Focused literal-path, destination-symlink
and physical-destination tests passed in 1.519 seconds. The two changed snapshot cases also passed under `-race`
in 3.042 seconds using `TMPDIR=/var/tmp`; production code was unchanged by this repair.

A second full race run with the final Go edits passed every application package but hit the 20-minute timeout
in the contract package. No data race or assertion failure was reported. Five subject suites remained, each active
for only 45–66 seconds when the package-wide alarm fired. Four goroutines were runnable in SQLite work; one waited
on the driver's shared allocation mutex while another was actively unlocking it. The concurrent-writer save case
had already completed its synchronization and reached per-vector fixture construction. The run overlapped shuffled
tests, the stress fixture and other host work; application package durations were approximately twice the earlier
run. This evidence supports computation and allocation contention rather than an observed deadlock. The timed-out
invocation remains an incomplete check, not a pass.

The successful retry reduced parallelism between independent subtests without changing concurrency inside the
tests, filtering cases or removing mutants. Its exact command was:

```sh
PATH=/home/alex/.local/share/mise/installs/go/1.27.1/bin:$PATH TMPDIR=/var/tmp CGO_ENABLED=1 go test -race -count=1 -parallel=4 -timeout=40m -json ./tests > /tmp/aminte-gap-contract-race.jsonl 2>&1
```

Together, the successful final-code application packages in `/tmp/aminte-gap-final-race.log` and this complete
contract retry provide race coverage for every tested package, including the new I/O regressions. Lifetime-preview
generation ran concurrently with the retry; it is not an isolated performance measurement.

The planned compatibility, analysis, race, fault-injection, fuzz and scale checks are complete, with the Windows
assertion defects repaired and verified on Windows. Independent evidence review checked all recorded scale tables,
digests and cleanup against the reports, and corrected the exact race finish time and fuzz replay skip counts.
Real power-loss behavior, sync faults and torn writes require a separate fault-capable environment and remain untested.
No real owner data was accessed or imported. Windows console shutdown and the minimum-engine Windows matrix remain
outside the exercised cases. This work cannot establish freedom from every bug or every form of file tampering.

### Minimum SQLite engine reproduction

The canonical schema was checked on Linux amd64 using Go 1.27.1 and actual
SQLite 3.51.3, source ID
`737ae4a34738ffa0c3ff7f9bb18df914dd1cad163f28fd6b6e114a344fe6d618`.
The isolated dependency pins were `modernc.org/sqlite v1.46.2`, `libc v1.70.0`,
and `memory v1.11.0`. The [upstream driver changelog](https://gitlab.com/cznic/sqlite/-/raw/v1.46.2/CHANGELOG.md)
and [SQLite release record](https://sqlite.org/releaselog/3_51_3.html) identify this
engine. Production dependency files were unchanged.

The floor driver predates `_defensive` DSN support, so the scratch-only adapter
below calls SQLite's `SQLITE_DBCONFIG_DEFENSIVE` before connection pragmas. Its
23 `lib/` engine files were SHA256-identical to the downloaded official module.
The engine probe checks source identity and required settings, including actual
FTS shadow-table refusal. This validates the SQLite engine with the adapter,
not an unmodified old driver or an application dependency downgrade.

SQLite 3.53-only evolution exercises are explicitly excluded: DROP/ADD
CONSTRAINT, enum/partial-date CHECK widening and ALTER COLUMN SET NOT NULL.
The 18 compatible evolution assertions remain. The 404 mutant definitions,
clean baselines and witness matcher are unchanged. Both evolution mutants keep
their owning witnesses: “every CHECK in schema is named” and “type change refuses
a retained incoming typed edge.”

The recorded run passed 39 suites / 8,941 expectations and all 404 mutants in
90.713 seconds. Fixture ownership/path tests, mutant-witness tests, and three
graph-fuzz seed cases also passed (0.284 seconds). This is not a generated fuzz
campaign. `TestCollationHelper` is run by the pages suite as a subprocess and
normally skips standalone invocation. Mermaid rendering was not rerun for an
engine-only matrix. The initial incomplete snapshot omitted linked untracked
issue pages; after copying the complete docs tree, the matrix above passed.

To repeat, use Go 1.27.1, Python 3 and git, with authorized access to the pinned
Go module archives. Save the following two scripts as `snapshot.py` and
`prepare.py` in a fresh temporary directory. Both scripts write only there.

```python
# snapshot.py
from pathlib import Path
import subprocess, shutil, json, hashlib, sys
base=Path(__file__).resolve().parent
root=Path(sys.argv[1]).resolve()
dest=base/'repo'
if dest.exists():
 raise SystemExit('refusing to overwrite an existing scratch repo')
names=subprocess.check_output(['git','ls-files','-z'],cwd=root).decode().split('\0')
for name in names:
 if not name: continue
 src=root/name
 if not src.is_file(): continue
 out=dest/name;out.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(src,out)
# Include untracked current process documents to keep index links valid.
shutil.copytree(root/'docs',dest/'docs',dirs_exist_ok=True)
(base/'snapshot.json').write_text(json.dumps({
 'commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root).decode().strip(),
 'schema_sha256':hashlib.sha256((root/'docs/schema/schema.sql').read_bytes()).hexdigest()
},indent=2)+'\n')
```

```python
# prepare.py
from pathlib import Path
import shutil, hashlib, json
base=Path(__file__).resolve().parent

def replace_once(text, old, new):
 if text.count(old) != 1:
  raise SystemExit('source drift: expected one occurrence of '+repr(old))
 return text.replace(old,new,1)

def omit_between(text, first, after):
 if text.count(first) != 1 or text.count(after) != 1:
  raise SystemExit('source drift: evolution boundary missing or duplicated')
 start,end=text.index(first),text.index(after)
 if start >= end:
  raise SystemExit('source drift: evolution boundaries out of order')
 return text[:start]+text[end:]

src=base/'modcache/modernc.org/sqlite@v1.46.2'; driver=base/'sqlite-driver'
if driver.exists():
 raise SystemExit('refusing to overwrite an existing scratch driver')
shutil.copytree(src,driver)
for directory in [driver, *[p for p in driver.rglob('*') if p.is_dir()]]:
 directory.chmod(0o755)
p=driver/'sqlite.go';p.chmod(0o644);s=(src/'sqlite.go').read_text();needle='func applyQueryParams(c *conn, query string) error {\n'
s=replace_once(s,needle,needle+'\tif err := applyFloorDefensive(c, query); err != nil { return err }\n');p.write_text(s)
(driver/'floor_defensive.go').write_text('''package sqlite

import (
 "fmt"
 "net/url"
 "strconv"
 "modernc.org/libc"
 "modernc.org/libc/sys/types"
 sqlite3 "modernc.org/sqlite/lib"
)

// Scratch compatibility adapter only. SQLite engine sources remain v1.46.2.
// The test suites use the later driver's _defensive DSN option. Apply the same
// SQLite C API before pragmas; this is not a shipped-driver compatibility test.
func applyFloorDefensive(c *conn, query string) error {
 q, err := url.ParseQuery(query)
 if err != nil { return err }
 values, found := q["_defensive"]
 if !found { return nil }
 if len(values) != 1 { return fmt.Errorf("_defensive must occur once") }
 enabled, err := strconv.ParseBool(values[0])
 if err != nil { return err }
 if !enabled { return nil }
 bp := libc.Xmalloc(c.tls, types.Size_t(16))
 if bp == 0 { return fmt.Errorf("defensive adapter allocation failed") }
 defer libc.Xfree(c.tls, bp)
 rc := sqlite3.Xsqlite3_db_config(c.tls, c.db, sqlite3.SQLITE_DBCONFIG_DEFENSIVE,
  libc.VaList(bp, int32(1), uintptr(0)))
 if rc != sqlite3.SQLITE_OK { return fmt.Errorf("defensive config: %d", rc) }
 return nil
}
''')
p=base/'repo/go.mod';s=p.read_text()
if any(line.lstrip().startswith('replace ') for line in s.splitlines()):
 raise SystemExit('review existing module replacements before repeating')
for old,new in [('modernc.org/sqlite v1.60.1','modernc.org/sqlite v1.46.2'),
                ('modernc.org/libc v1.77.1','modernc.org/libc v1.70.0'),
                ('modernc.org/memory v1.12.1','modernc.org/memory v1.11.0')]:
 s=replace_once(s,old,new)
s+='\nreplace modernc.org/sqlite => '+str(driver)+'\n';p.write_text(s)
(base/'repo/tests/floor_engine_test.go').write_text('''package tests

import (
 "path/filepath"
 "strings"
 "testing"
)

func TestFloorEngine(t *testing.T) {
 d := realDocs()
 s := &S{name:"floor-engine",d:d,ddl:d.DDL(),dir:t.TempDir()}
 defer s.close()
 c := s.freshWith(F{Path:filepath.Join(s.dir,"floor.db"),Hardened:true})
 got := c.str("SELECT sqlite_version()")
 source := c.str("SELECT sqlite_source_id()")
 t.Logf("SQLite %s source %s",got,source)
 if got != "3.51.3" || !strings.Contains(source,"737ae4a34738ffa0c3ff7f9bb18df914dd1cad163f28fd6b6e114a344fe6d618") {
  t.Fatalf("wrong engine: %s %s",got,source)
 }
 if c.tryx("DELETE FROM entities_fts_data") == "OK" { t.Fatal("DEFENSIVE was not enabled") }
 if c.n("PRAGMA trusted_schema") != 0 { t.Fatal("trusted_schema must be OFF") }
 if c.n("PRAGMA synchronous") != 2 { t.Fatal("synchronous must be FULL") }
 if c.n("PRAGMA foreign_keys") != 1 || c.n("PRAGMA recursive_triggers") != 1 { t.Fatal("foreign_keys and recursive_triggers must be ON") }
 t.Log("Explicit floor exclusions: ALTER TABLE DROP/ADD CONSTRAINT and ALTER COLUMN SET NOT NULL probes require SQLite 3.53; compatible evolution assertions remain enabled.")
}
''')
official={p.relative_to(src/'lib'):hashlib.sha256(p.read_bytes()).hexdigest()
          for p in (src/'lib').rglob('*') if p.is_file()}
actual={p.relative_to(driver/'lib'):hashlib.sha256(p.read_bytes()).hexdigest()
        for p in (driver/'lib').rglob('*') if p.is_file()}
if len(official) != 23 or actual != official:
 raise SystemExit('engine file inventory or content differs')
(base/'engine-files-verification.json').write_text(json.dumps({
 'engine_file_count':len(actual),'unchanged':True},indent=2)+'\n')
print('Prepared scratch driver adapter; all 23 engine files are unchanged.')

# Keep all floor-compatible evolution assertions, excluding only syntax added in
# SQLite 3.53. The canonical schema requires 3.51.3; those future migration
# exercises intentionally have the newer engine requirement in tests/README.md.
p=base/'repo/tests/evolution_test.go'
original=base/'evolution_test.original.go'
if original.exists():
 raise SystemExit('refusing to overwrite original evolution source')
shutil.copyfile(p, original)
s=original.read_text()
s=replace_once(s,'\terr := func(r string) bool { return strings.HasPrefix(r, "ERR") }\n','')
s=omit_between(s,'\ttype nt struct{ name, table string }',
               '\t// ---- architecture/non-goals CJK search:')
cjk='\t// ---- architecture/non-goals CJK search: the tokenizer switch is one transaction on a derived index\n\tc = s.fresh()'
s=replace_once(s,cjk,cjk.replace('\tc =','\tc :='))
s=omit_between(s,'\t// ---- an entity uid is additive',
               '\t// ---- comments: inside a statement kept')
s=replace_once(s,'\tt = s.connect("")','\tt := s.connect("")')
s=replace_once(s,'func evolution(s *S) {','func evolution(s *S) {\n\t// FLOOR MATRIX EXCLUSION: DROP/ADD CONSTRAINT and ALTER COLUMN SET NOT NULL require SQLite 3.53.\n\t// Named CHECK inventory, reader compatibility, tokenizer, link-kind/type guards and comments still run.')
p.write_text(s)
```

The preparation script above adds fail-fast source-drift checks to the original
experiment. It was executed in a fresh scratch directory and reproduced all four
Go files (driver hook, defensive adapter, floor engine probe and evolution
exclusions) byte-for-byte against the passing experiment. The three dependency
pins and all 23 engine-file hashes matched; missing/duplicate replacement anchors
and missing/duplicate/reversed exclusion boundaries were independently rejected.
The full matrix was not rerun for these script-only checks.

Run these commands, substituting the fresh temporary directory and source path:

```sh
set -eu
FLOOR_DIR=/tmp/aminte-sqlite-floor-repeat
REPO_DIR=/path/to/aminte
GO=/path/to/go1.27.1/bin/go
python3 "$FLOOR_DIR/snapshot.py" "$REPO_DIR"
GOMODCACHE="$FLOOR_DIR/modcache" "$GO" mod download -json modernc.org/sqlite@v1.46.2 modernc.org/libc@v1.70.0 modernc.org/memory@v1.11.0
python3 "$FLOOR_DIR/prepare.py"
cd "$FLOOR_DIR/repo"
GOCACHE="$FLOOR_DIR/gocache" GOMODCACHE="$FLOOR_DIR/modcache" "$GO" test -mod=mod ./tests -run '^Test(FloorEngine|Suites|Mutants)$' -count=1 -v > "$FLOOR_DIR/matrix.log" 2>&1
GOCACHE="$FLOOR_DIR/gocache" GOMODCACHE="$FLOOR_DIR/modcache" "$GO" test -mod=mod ./tests -run '^(TestCanonicalIdentityFixtureOwnershipAndRollback|TestCollationHelper|TestSchemaFixtureUsesLiteralFilename|TestMutantWitness|FuzzGraphTransitions)$' -count=1 -v > "$FLOOR_DIR/additional.log" 2>&1
```

The original experiment used a local module proxy for already-cached project
modules; the commands above use the configured Go proxy. Preserve download
checksums, source snapshot/schema hash, engine identity and logs with the run.
Original schema SHA256:
`2db05691d5322796e339c16630148995a1857550beaf23e32f21449cb8e64ab8`;
source HEAD `6f2bfe8f676786046c708a12cc38a45c0867a2b9`.

Limits: Linux amd64, fresh-init canonical schema and synthetic contract cases;
no older engines, alternative compile options, Windows minimum-engine run or
full application-package matrix. SQLite 3.53-only evolution remains validated
by the ordinary current-engine baseline, not by this floor experiment.

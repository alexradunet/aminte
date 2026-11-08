# 071 — Integrate the 22 residual audit follow-ups

> Dated implementation record, not permission to operate on real data. Candidate delivery stays unstaged; only the owner can accept it. Historical briefs are not rewritten by this record.

## Status

- **Date / baseline:** 2026-10-05, `e1d79591b6f57235a2132d17ad5123fe3bd424d0`.
- **Priority:** P1/P2. **Effort:** L aggregate. **Risk:** HIGH for import evidence/identity, MED for surface integration.
- **Status:** REJECTED (2026-10-28): the import machinery it repaired was removed by [plan 089](089-no-import-process.md)
  ([RFC 0011](../rfcs/0011-no-import-process.md)); the candidate branch was never merged.
- **Scope:** all 22 owner-selected residual findings, implemented by nine isolated components and one serialized CLI stage, then integrated and reviewed in `implement/audit22-20261005-01`.
- **Related residual briefs:** 038, 043, 050, 055, 057, 058, 059, 065 and 066, available [at this record's baseline](https://github.com/alexradunet/aminte/tree/e1d79591b6f57235a2132d17ad5123fe3bd424d0/docs/plans). Their owner-review status is unchanged.

## Why and boundaries

A further audit found residual gaps after the preceding batch: unsafe evidence acceptance, ambiguous timed identities, lossy response numbers, incomplete feedback, cleanup failure paths and repeated preparation work. The owner authorized all 22 follow-ups, not a new schema or product direction.

Canonical and embedded schema, `go.mod`/`go.sum`, and `internal/api/authority.go`/`authority_test.go` remain byte-identical to baseline. Plan-063's CLI address-refusal and real-listener mount tests are unchanged; early `LoopbackAddress` refusal and `NetworkAuthority` mounting remain intact. No migrations, freeze, canonical rebuild/replay, real database/export/workspace/media inspection, or aminte MCP tool use occurred. Test databases, workspaces and JPEGs are synthetic disposable fixtures. The original checkout, master and unrelated worktrees were not edited. `aminte.exe~` belongs to the original checkout; its absence in isolated integration was confirmed expected, not evidence of an initial ignored-file inventory.

[029](https://github.com/alexradunet/aminte/blob/e1d79591b6f57235a2132d17ad5123fe3bd424d0/docs/plans/029-google-takeout.md) B/C still requires owner inventory, selection and approvals; [070](https://github.com/alexradunet/aminte/blob/e1d79591b6f57235a2132d17ad5123fe3bd424d0/docs/plans/070-trial-capture-pilot.md) still requires owner pilot/path/client approval. Neither this implementation nor passing tests opens those gates.

## Finding-to-implementation and evidence map

Paths below are repository-relative. Red/green claims are component-recorded executed synthetic regressions, not fresh baseline replays in integration. Integration reviewed actual patches, test sources and reports and reran the combined green suite. Inspected-only, injected and platform-specific evidence is explicitly distinguished.

| # | Disposition / files | Tests and effective evidence |
|---|---|---|
| 1 | Explicit DB presence, not equality with environment; workspace resolution before snapshot. `cmd/aminte/main.go`, new `cli_regression_test.go` | `TestWorkspaceDatabasePrecedence`: actual page lookup and opened synthetic snapshot verify explicit DB > workspace trial > environment, including explicit-equals-environment. Baseline 404/wrong snapshot red, green after fix; initial echoed-SQL fixture was strengthened before counting evidence. |
| 2 | Escape review display controls only. `internal/importer/approval.go`, `approval_test.go` | `TestReviewEscapesControlsWithoutChangingApproval`: baseline raw controls red; whole-body/diff display green, original hashes/approved copies and mutation refusal preserved. Newline/tab retained. `Changed` status output is outside this seam. |
| 3 | Submitted unit equals approved unit before unitless marker exception. `internal/importer/facts.go`, new `source_evidence_regression_test.go` | `TestReadingApprovedUnitBeforeMarker`: registered mg metric, changed unitless approval and mg submission; Check/Apply baseline accepted and persisted; green refusal preserves measurements/ledger. Existing database-unit check retained. |
| 4 | Reject repeated canonical timed identity before root load/write. `reading_key_resolver.go`, `reading_keys_test.go` in importer | `TestTimedReadingIdentityRefusedBeforeLookup`, `TestTimedReadingIdentityCheckApplyAtomic`: six identical/case/NFC/value/tz/with variants; baseline accepted or refused too late, green before lookups with atomic refusal on repeated attempts. `TestTimedReadingDistinctInstants` control; stored keys/history unchanged. |
| 5 | Associate cropped table quotes at actual numeric source positions, fail closed on repeated cells. `source_evidence.go`, new `source_evidence_regression_test.go` | `TestCroppedTableUnitEvidence`: three actual unsafe Check/Apply persistence reproductions red, green refusals. `TestCroppedTableQuantityControls`: valid cropped Markdown/header and quoted CSV controls. Existing `TestNumericSourceEvidence` unchanged. |
| 6 | Separate facts workflow instructions from direct tool capability. `docs/guides/importing.md`, `docs/contract/threat-model.md` | Prose/source inspection plus `TestSuites/document`; no runtime policy restriction added or invented red claim. Approval, direct metric creation and replay are owner-only; plural workspace registration is model-callable only for owner-approved rows. `TestWorkspaceRegistrationPolicy` pins catalog flags, API refusals/approval-gated success and actual SDK listing without invoking MCP tools. Status is diagnostic, not rollback or enforcement. |
| 7 | Release blocked resources before server close; return assertions out of DryRun. `internal/client/client_test.go`, `cmd/aminte/main_test.go`, importer `reading_keys_bench_test.go` | Cleanup ordering inspected; `TestCommandCancellation`, client queued-write tests, `TestReadingRootLookupCount`/`SeesLaterWrite` green. CLI cancellation focus repeated ten times by CLI component. No intentionally hanging old subprocess or injected cleanup-failure reproduction claimed. Assertions remain before normal release. |
| 8 | Unix-second chart arithmetic and earliest-reading all-range axis. `internal/api/html.go`, new `browser_regression_test.go` | `TestBrowserAllReadingsCoordinates`: follows actual All readings link; baseline both x=626.0 red, distinct modern points green, 90-day endpoint/empty/exclusive same-day controls. Query semantics unchanged. |
| 9 | Locate metrics status by header; replace raw status cell only. Importer `files.go`, `approval_test.go` | `TestApprovalUsesReorderedStatusColumn`: baseline approved note rather than status red; review/hash/stamp/registration green. `TestApproveRowsPreservesOtherCells` retains escaped pipes, whitespace and unrelated proposed cells. |
| 10 | Live habit day rows and recording suggestions. `internal/core/read.go`, `habits_test.go`; new API `live_day_view_test.go`; `docs/cookbook/day-view.md`; `tests/habits_test.go`, `mutants_test.go`, `README.md` | `TestTombstonedHabits`, `TestLiveDayView`, literal habits suite: baseline hidden Walk displayed red, green live/tombstone/revival; historical readings/periods/Series retained. New mutant requires exact witness `cookbook/day-view hides tombstoned habits`; total 191. |
| 11 | Recover only accepted bounded multipart transcript. API `files.go`, new `browser_regression_test.go` | `TestBrowserMultipartRecoversAcceptedBody`: title/preview/radius/conflict/aggregate overflow baseline lost transcript red; green escaped recovery with no files written. Unknown/original fields not retained. Synthetic HTTP, not browser automation. |
| 12 | Nil-aware field rendering, including zero. API `html.go`, `html/layout.html`, new `browser_fields_test.go`, `browser_regression_test.go` | `TestBrowserLocateZeroAxes`: both zero axes red then green; `TestBrowserFieldPresence` covers hidden/textarea/selected zero, nil and nonzero defaults. |
| 13 | Lossless client response numbers with trailing-document refusal; float conversion only for CLI coordinates. `internal/client/client.go`, new `numbers_test.go`; `internal/mcp/mcp_test.go`; CLI `main.go`, `main_test.go`, new `cli_regression_test.go`; API `files_test.go` | `TestClientExactNumbers` local/remote red float64 then green for 9007199254740993 and int64 bounds, nested values and read-to-action path/form. `TestClientRejectsTrailingJSON` control. `TestMCPExactResponseNumbers` added green after fix, not separately red. `TestFileCommandNumberCoordinates` red two distant groups collapsed, green; `TestCoordinateNumbersAndErrors` preserves exact IDs/refuses invalid/nonfinite coordinates. Exact Lisbon/Porto radius assertions adapted to json.Number("10000")/json.Number("6000") under explicit integration approval; no assertions removed. |
| 14 | Retain separate automatic day sync as `Kept.DaySync`. Core `files.go`, `places.go`, new `photo_sync_test.go`; new API `photo_feedback_test.go` | `TestPhotoDaySynchronizationFeedback`: baseline nil day sync red, green separate file/day created/revived/skipped feedback, dry rollback/no-op controls. `TestPhotoDaySyncJSONAndBrowserReceipt`: followed synthetic HTTP redirects green; injected discard-DaySync negative control failed and was restored. |
| 15 | NFC only after CommonMark decoding/parsing. `internal/text/text.go`, new `normalization_test.go`; API `markdown.go`, new `markdown_normalization_test.go`; core new `photo_sync_test.go` | `TestHasEmbedPostParseNFC`, `TestMarkdownPostParseNFC`, `TestPhotoEmbedNFDLongTitleDeduplication`: red NFD render/embed mismatch/duplicate append, green normalized 204/240/242-byte boundaries, aliases/escaping/U+1FEF/code/link precedence. Stored body bytes and title grammar unchanged. |
| 16 | Context-aware inventory plus narrow CLI signal interception. Takeout `inventory.go`, `inventory_test.go`; CLI `main.go`, new `cli_regression_test.go` | `TestInventoryContextCancellation`: deterministic traversal/JSON/CSV checkpoints and both sentinels; baseline context-ignoring shim red, green no report and error identity. `TestInventoryCommandCancellation` and `DuringTraversal`: actual dispatch red ignored context then green. `TestCommandInterruptPolicy` table inspected/tested; actual Windows Ctrl-C delivery not tested. Unsupported owner-local operations retain OS interrupt behavior. |
| 17 | MCP initialization warns source/file/page content is untrusted data. `internal/mcp/mcp.go`, `mcp_test.go` | SDK `TestMCPUntrustedDataInstructions`: four fragments absent red, delivered green. Defense in depth, not sandbox/tool authorization enforcement. |
| 18 | Resolve selected root, never follow descendant symlinks. Takeout `inventory.go`, `inventory_test.go` | `TestInventorySymlinkRoot`/`DoesNotFollowDescendantSymlinks`: platform/privilege-gated equivalence/confinement tests. Component baseline command passed, not a demonstrated red defect. Final integration verbose run skipped both for missing Windows symlink privilege; no executed symlink-success claim here. |
| 19 | Propagate identified query/Scan/rows.Err/Close errors. Core `integrity.go`, new `integrity_test.go` | `TestIntegrityReadFailures`: synthetic database/sql driver injects 15 failures across three streams, red lost errors/continued checks then green; stream closures/no later FTS verified. `TestIntegrityResultSemantics`/`Diagnostics` retain clean/FTS/nullable foreign-key/diagnostic semantics. Not actual corrupt-SQLite false-clean evidence. |
| 20 | Invocation-local source tokens/tables/offsets and bounded backward UTF-8. Importer `facts.go`, `source_evidence.go`, new `source_evidence_regression_test.go`, `source_evidence_bench_test.go` | `TestReadingEvidenceContextSnapshot`, `TestEvidenceBackwardRuneAndOffsets`, `TestEmptyAndMalformedEvidenceContext`; changed-source controls and shared immutable slices. `BenchmarkReadingSourceCheck` measures full static checks, not root lookup; measurements below. No cross-invocation cache or linear whole-import claim. |
| 21 | Prepare/resolve proof once per source/file, retain each root index. Importer `reading_key_resolver.go`, `reading_key_proof_test.go`, `reading_keys_bench_test.go` | `TestCorrectionProofPrepareResolveCount`: extracted original per-root implementation red 12/12, grouped green 4/4 per invocation with 12 entries. `TestCorrectionProofSharesCheckedFacts` actual trial transactions; existing `BindsCheckedFacts` unchanged. Target revalidation retained. `BenchmarkBuildCorrectionProof` below. |
| 22 | Remove stale several-category deferral and managed-original attribution. `docs/architecture/non-goals.md`, `docs/research/prior-art.md` | Prose inspection against D9/D26, document suite green before/after; no new behavior or invented runtime red. Historical records untouched. |

## Measurements and limits

Component measurements were Windows/amd64 on AMD Ryzen AI 9 HX 370, synthetic only. The source-check benchmark's pre-optimization run included correctness changes; the proof benchmark compared grouping with old evidence cost still present. Do not compare these as identical whole-pipeline baselines or extrapolate to real imports.

`BenchmarkReadingSourceCheck`, `-benchmem -benchtime=1x`:

| Readings | Before ns/op; B/op; allocs/op | After ns/op; B/op; allocs/op |
|---:|---|---|
| 10 | 2,899,200; 3,484,640; 9,677 | 700,800; 94,672; 793 |
| 100 | 2,447,380,900; 2,342,989,688; 819,701 | 879,600; 483,968; 6,287 |
| 500 | 120-second command timeout; no result | 3,521,900; 2,167,776; 31,088 |

After source optimization at `-benchtime=100ms`, 10/100/500 readings measured 103,045/1,397,377/7,991,147 ns/op, 47,875/437,336/2,131,596 B/op and 685/6,276/31,091 allocations/op.

`BenchmarkBuildCorrectionProof`, 30 facts, `-benchmem -benchtime=1x`:

| Roots | Before ns/op; B/op; allocs/op | After grouping ns/op; B/op; allocs/op |
|---:|---|---|
| 1 | 30,030,900; 20,081,816; 19,943 | 19,203,600; 20,086,784; 19,948 |
| 3 | 52,204,500; 60,315,824; 59,525 | 10,310,700; 20,102,168; 19,853 |
| 10 | 111,070,900; 201,413,352; 198,443 | 11,485,600; 20,031,448; 19,850 |

The initial 300-fact proof command exceeded its 120-second bound; only its one-root result completed (21,861,195,000 ns/op, 18,758,640,560 B/op, 1,713,359 allocations/op). The bounded comparison used 30 facts; no result is claimed for unfinished cases.

Final combined integration command:

```sh
go test -mod=readonly -count=1 ./internal/importer -run '^$' -bench 'BenchmarkReadingSourceCheck|BenchmarkBuildCorrectionProof' -benchmem -benchtime=1x
```

Passed. Proof 1/3/10 roots: 3,598,300/3,671,700/3,388,000 ns/op, 241,600/223,816/228,200 B/op, 2,532/2,422/2,437 allocations/op. Source 10/100/500 readings: 165,000/417,900/3,090,900 ns/op, 90,208/431,712/2,096,912 B/op, 695/6,278/31,087 allocations/op. Single-iteration timings are not statistical speedups; deterministic preparation counts and allocation scaling are the stronger evidence.

Remaining limits: conservative repeated-cell refusal, limited free-prose unknown-unit vocabulary, memory proportional to source bytes, remaining quote/row scans, cooperative cancellation between parser/OS operations, no cancellation rollback of committed writes, forgeable workspace approval files, direct-tool replay limits and instructions not enforcement. No real import, browser automation/exploitation, OS Ctrl-C delivery, power-loss, race/fuzz, advisory or Mermaid-render validation claimed.

## Delivery, recovery and validation

The first workflow `2bece5f0-d30c-496b-b7a2-cea0da87e240` failed after all nine components completed but before assembly: `emit.handoffs[0].outputPathMapping` was undefined. Parent verified manifests, patch hashes and heads; components were not rerun. Recovery attempts with wrong mission cwd/missing inline script launched no children. Recovery assembled from actual manifest patch paths after complete inspection, original-byte SHA256 validation and all non-index `git apply --check` checks, applying each once. The nine patches have disjoint 44-file ownership. Harness `captureWorktreeDiff` automatically staged component snapshots after workers reported empty indexes; those preserved artifact worktrees/indexes were left untouched. This is not staging in integration.

Assembly inspection encountered Windows Python cp1252 decoding/output errors; execution paused for supervisor disposition, then strict UTF-8 input/output inspection succeeded without rewriting captured patches. The initially approved Lisbon assertion adaptation exposed Porto's same representation mismatch in the full gate; assembly stopped and obtained explicit approval for that second exact-value adaptation. Final assembly gates passed. CLI then intentionally changed only `main.go`, `main_test.go` and new `cli_regression_test.go`; it verified all 44 component result blobs unchanged. Before this plan/index, integration had exactly 48 unstaged files. This plan/index initially added two, for 50 (37 tracked modifications, 13 untracked files); the approved review revision below also changes two previously untouched production files and adds one focused API test, for 53 (39 tracked modifications, 14 untracked files).

Combined diff/test/report review confirmed all 22 dispositions above; no additional source fix was made by final integration. Before plan/index edits, this exact gate passed:

```sh
go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly -count=1 ./... && git diff --check
```

CLI 8.785 s, API 25.528 s, importer 36.276 s, contract suites 33.788 s; all packages green. Focused `go test -mod=readonly -count=1 ./cmd/aminte -run '^TestServeAuthority'` passed (0.200 s). Verbose Takeout symlink tests skipped individually for unavailable privilege, as stated above. Original patch bytes and all 44 resulting Git blobs were reverified; schema/module/authority bytes and CLI authority test functions match HEAD, index empty. The same full gate is required again after this dated record/index; the final artifact records its actual output.

## Approved review revision (2026-10-05)

The first independent review workflow `8012b6d0-0672-4b35-9882-5e461a188a70` returned blocked-for-parent because file-only review exports failed. Structured reports survived and were recovered through the same native protocol; completed implementation was not rerun. Imports review `5a789f85-a8fe-415f-a563-7efa55a333cf` identified two inspected, not executed counterexamples; contract/tests review `012d5f5e-dd78-44b4-adcf-fa18e9ff4434` identified a false registration-policy statement. Surfaces review had no findings. Parent inspected and approved these three bounded revisions; status stays IN REVIEW (owner).

- **imports-1 (#5/#20):** `TestReadingEvidenceOriginalMarkdownSyntax` reproduced the exact U+1FEF-created-fence witness through Check/Apply: both accepted, measurements persisted and ledger changed. Removing normalization only in `newReadingEvidence` made direct context controls green but the end-to-end witness remained red. Execution exposed upstream `ReadSource` normalization too. Parent approved a private raw confined source path in `workspace.go`, sharing `sourcePath`/`os.Root.ReadFile` and 404 handling, used only by facts preparation in `apply.go` and reading-identity preflight's source-read call in `reading_key_resolver.go`. Public `ReadSource` and vault `readSource(root, rel)` stay NFC-normalized. No filename/confinement/key/proof/inspect/status/vault policy changed. Evidence now parses original Markdown/CSV, normalizes extracted cells and maps original byte boundaries through NFC segments and one whitespace-collapse map, constructed once per context. No per-prefix collapse or per-reading source retokenization. Check/Apply witness now refuses without persistence; preflight refusal assertion added green after fix. `TestReadingEvidenceOriginalSyntaxControls` covers genuine code fences, legitimate header quantities, U+1FEF and NFD/invalid-byte prefixes and NFD CSV offsets. `TestRawEvidenceSourceIdentityAndConfinement` verifies raw bytes versus normalized public reads, canonically equivalent filenames, missing/traversal refusal and an escaping-symlink case (privilege skip here). Existing 044/049 tests unchanged; junction case executed successfully, ordinary symlinks and trailing-space filename case skipped individually on this Windows platform.
- **imports-2 (#9):** `TestApprovalSeparatedMetricTableLayouts` reproduced two separated tables with different status-column orders: wrong second-table note, missed status, wrong review/stamp hash and only one registered metric. `approveRows` now resets its status index at the same non-table boundaries as `Metrics`, recomputing from each recognized header. Exact non-status bytes/escaped pipes remain intact. Green test checks review/hash, stamped body/copy, both approved parsed rows and registration. Initial fixture escape syntax and expected registration order were corrected separately; neither is counted as behavioral red evidence.
- **contract-6-registration-policy (#6):** corrected roles, direct-write explanation, gate/procedure and threat model to distinguish owner-only singular direct creation from approval-gated plural workspace registration. Runtime behavior remains unchanged. New `internal/api/registration_policy_test.go` checks draft refusal/no metric write, agent registration after owner approval using workspace provenance, singular direct creation/replay 403 and no replay target, API catalog Owner flags/approval absence and actual in-memory SDK tool listing (no MCP tool invocation). This test passed on existing runtime before prose correction; no fabricated prose-red claim. API/MCP production files unchanged.

Focused commands:

```sh
go test -mod=readonly -count=1 ./internal/importer -run '^TestReadingEvidenceOriginalMarkdownSyntax$'
go test -mod=readonly -count=1 ./internal/importer -run 'TestReadingEvidenceOriginal|TestRawEvidenceSource|TestReadingEvidenceContextSnapshot|TestEvidenceBackwardRuneAndOffsets|TestEmptyAndMalformedEvidenceContext|TestCroppedTable|TestNumericSourceEvidence'
go test -mod=readonly -count=1 -v ./internal/importer -run 'TestRawEvidenceSource|TestSource(Filename|Path|Confinement)'
go test -mod=readonly -count=1 ./internal/importer -run 'Test.*Vault|TestReadingKeyIdentity|TestLegacyReadingKeyCompatibility|TestCorrectionProof|TestTimedReading|TestRehearsal'
go test -mod=readonly -count=1 ./internal/importer -run 'TestApprovalSeparatedMetricTableLayouts|TestApprovalUsesReorderedStatusColumn|TestApproveRowsPreservesOtherCells|TestReviewEscapesControls|TestUnified'
go test -mod=readonly -count=1 ./internal/api -run '^TestWorkspaceRegistrationPolicy$'
go test -mod=readonly -count=1 ./internal/importer -run '^$' -bench BenchmarkReadingSourceCheck -benchmem -benchtime=1x
```

Witness first command red before revision, then combined evidence focus green (5.314 s); source path/confinement focus green (1.023 s with qualified skips), vault/key/proof/rehearsal focus green (8.338 s); approval focus green (0.359 s); API policy characterization green (0.229 s). Revised actual full-check benchmark 10/100/500 readings: 352,800/577,300/3,234,400 ns/op; 60,144/498,640/2,304,384 B/op; 797/6,297/31,112 allocations/op, passed 0.110 s. Same Windows/amd64 synthetic host, one iteration; no unsupported speedup/whole-import complexity claim.

Original patch hashes stay immutable, but not all original result blobs can match after approved revisions. Exact intentional original-component deltas are `source_evidence.go`, `source_evidence_regression_test.go`, `files.go`, `approval_test.go`, `reading_key_resolver.go`, `docs/guides/importing.md`, `docs/contract/threat-model.md` (seven of 44); untouched component result blobs remain required to match (37 of 44). Additionally, `workspace.go`/`apply.go` change only the approved raw-read seam, plan 071 records evidence, and one new API test pins existing policy. Final scope/protected-byte checks and full generation/vet/uncached-test/diff gate are recorded in the revision artifact; no schema/module/authority/CLI/API-numeric adaptation changes or staging authorized. Historical briefs and 029 B/C/070 gates remain unchanged.

## Owner-review checklist

- [x] All 22 selected findings have bounded implementations and truthful evidence/limitations.
- [x] Complete combined green gate before this record; original component blobs and preservation checks verified.
- [x] No real data activity, staging/commit/merge/push, historical-record replacement, schema/dependency changes or self-acceptance.
- [ ] Owner accepts or rejects the combined candidate after reviewing the final post-documentation validation artifact.
- [ ] Owner separately opens any 029 B/C or 070 gate; neither is performed by this plan.

Recommended next step: independent parent/owner review of the combined candidate. Leave status IN REVIEW (owner); acceptance, publication and any real-data operation remain separate authority decisions.

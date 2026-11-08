# 072 — Stable names and life periods implementation candidate

## Status and authority

- **Date:** 2026-10-06.
- **Baseline / branch:** `cfb7fe29b15d157baa1cccf78473ca0dc2b3696d` / `implement/rfc0006-sol61-20261006`.
- **Status:** REJECTED (2026-10-28): [RFC 0006](../rfcs/0006-stable-names-and-life-periods.md) is withdrawn and its issues
  are closed; the candidate branch was never merged. The record below is as it was: THREE COMPONENT HANDOFFS ACCEPTED BY PARENT; FRESH INTEGRATION INSPECTION COMPLETE; FINAL CANDIDATE GATES RECORDED EXTERNALLY; PARENT/OWNER REVIEW PENDING. Parent accepted identity checkpoint `20261006T150510Z-ce99b566`, temporal checkpoint `20261006T192421Z-ac1f0119` and import checkpoint `20261006T234055Z-0ef0a919` after independent review. Fresh integration is authorized only for inspection, reserved temporal hardenings, small justified seam fixes and validation. These dispositions are not owner/RFC/release acceptance; real-data/publication/freeze remain unauthorized.
- **Proposal:** [RFC 0006](../rfcs/0006-stable-names-and-life-periods.md), still draft; issues remain proposed.
- **Owner authorization:** “Let's implement this into an subagent using sol 6.1 on medium and then we review it.” This authorizes an uncommitted candidate, not acceptance, real-data operations or freeze.
- [Plan 071](071-audit-followups.md) remains IN REVIEW (owner), untouched.

## Temporal gate and serial ownership (2026-10-06)

Parent acceptance is recorded in external `artifacts/identity-core/parent-review-repair-review.md`; the complete identity result is archived in `review-repair-handoff.md`. No historical identity repair task is reopened. The retained Sol 6.1 medium worker owns only temporal-records in the same sole-writer worktree.

The first temporal continuation ended with `Codex error: The usage limit has been reached`, before any source adoption or supervisor request. Owner requested one same-protocol continuation; parent verified the unchanged identity checkpoint. Original `artifacts/temporal-records/proposal-01.md` remains byte-identical. Separate proposal02 corrects definite-membership conditioning on ordered pairs. Independent enumeration executes 12,960 exhaustive cases and nine hand-specified finite vectors; parent independently reproduced these. Only finite unqualified mathematics was proved at admission, not all proposed storage/time/lifecycle behavior.

Native reply `4688479c-fe2d-4cac-8747-a18bc422309b` via `contact_supervisor(reason: need_decision)` returned **APPROVED WITH ADJUSTMENTS**, controlling disposition `artifacts/temporal-records/parent-gate-01-decision.md`, retained run `5f8a627a-04d7-44da-ac50-61a60917ab2f`. This reference supersedes conflicting proposal01/02 text: periods use existing optional zero-or-more `part-of` categories, **no new is-a** (the protected RFC's existing-is-a assumption was mistaken); NULL unknown endpoints and conservative qualifiers; conditional finite membership and explicit ongoing query horizon; required exclusive session start with optional end; local/mixed ordering unresolved; unverified zone claims; numeric offset hours00–23/minutes00–59 excluding known -00:00, not current-world ±14 extrema; exact full-range elapsed milliseconds; explicit measurement scope/lifecycle and same-scope correction; atomic same-metric changed-scope relocation only for current nonretracted ordinary unkeyed owner chains, never an imported intent bypass. Missing-start sessions and imported/keyed relocation are explicit first-profile limitations. Other hand-checked expectations require permanent canonical/writer/mutant/parity proofs before the temporal handoff. No owner/RFC/release acceptance follows.

### First temporal computation checkpoint

Implemented the approved shared-core boundary/endpoint/elapsed computations and owning tests in `internal/core/temporal_time.go` / `temporal_time_test.go`. Finite membership uses the conditioned formula, checked independently by enumerating September endpoint pairs; qualifier/unknown/ongoing cases remain distinct, including required explicit horizon and a start after the horizon. Session evidence validation allows +14:01/+23:59, refuses -00:00/dual representation/local-with-known-offset/orphan evidence, and retains gap/fold clocks only as unresolved claims. Exact UTC elapsed uses integer UnixMilli differences, not time.Duration/Julian floats; hand-computed full range is 315569519999999ms, with year-zero leap and year9999 one-ms probes.

`time-focused.log` is green; `preparation-generate.log`, `preparation-vet.log` and `preparation-baseline.log` pass. The latter includes the unchanged 233 effective identity catalog; **no new temporal storage mutants are claimed**. No temporal DDL/current-truth contract/query/actions/client/storage adoption exists at this checkpoint. This is partial approved implementation, not a completed phase or parent acceptance. Remaining owned work: canonical period/session/scope rules, practical transactional capture/edit/correction/relocation and queries, docs/vectors/recipes/integrity/diagrams, effective temporal mutants, thin-adapter parity, risky-write/rollback/retry/reopen proofs and full final gates.

## Parent-review repair (2026-10-06)

Parent withheld identity acceptance at checked checkpoint `20261006T141128Z-c8204744`, despite genuine green full gates. Its file-backed synthetic probe showed all six rowid/_rowid_/oid ID updates succeeding on `links` and `entity_names`; declared id refused. Reproduced both supplied overlay probes before changes: `review-reproduced-red.log` records six semantic ID failures and both disabled name-touch triggers surviving all 24 owners. Permanent `id-guards` owner reproduced **15/27** before the fix (`id-guards-permanent-red.log`), including combined allowed+forbidden edits, not setup failures.

Changed only the two immutable-value trigger headers to unfiltered BEFORE UPDATE, retaining their existing NEW/OLD WHEN comparisons. SQLite alias updates now reach the same value invariant; unchanged ids, permitted notes and equivalent spellings still work. File-backed declared/id/rowid/_rowid_/oid probes check atomic refusal of standalone and combined edits, all entity/registry/link state and revisions, mirror identity, FTS checksum/search and reopen. Canonical revision witnesses now prove alias insert, equivalent spelling, already-owned preferred selection, conflict/no-op and rollback semantics; separate production tests remain. Added effective mutants for each touch rule, preferred-selection touch omission and reintroducing either filtered-trigger bypass.

New `name-grammar` owner executes the sole documented raw/NFC name-vector JSON under mutation, separate from production create/save/rename tests. It proves raw/NFC reference/embed/display key agreement and independent bracket-storage refusals. Effective mutants break NFC syntax-pair refusal, entity-escape refusal, intraword-underscore acceptance, normalized NFD keys and each bracket CHECK. Removed the identical name-delete mutant rather than counting it twice. Current catalog is **233**, with **26** owners; setup stops/no-op mutations/unrelated failures remain invalid kills.

Corrected touched core redirect-hop/stub/rename-maintains-redirect descriptions and errors; unsupported kind statuses and HTTP redirects remain. Day-view comment now says implements the recipe with registry/preferred display, not verbatim SQL. Protected authority.go and authority_test.go stay byte-identical, while approved canonical SQL fixtures elsewhere (including browser-origin api_test.go) changed without changing security policies/assertions.

Focused green: id-guards **28/28**, identity **95/95**, name-grammar **58/58**; exact logs `review-focused-green.log` and subsequent full gates. Supplied parent overlay probes both pass after repair (`review-parent-probes-green.log`). Whole expanded canonical mutants pass with negative kill controls (`review-mutants-first.log`), not merely production vector regressions. Final separate generation/vet/uncached full baseline/full fixed-seed shuffle/diff and preservation logs are `review-final-*`; all old red/checkpoint/handoff evidence remains archived. No new policy/importer feature, dependency/tooling, later phase, staging/commit, real/default data or freeze. Return only for parent independent re-review.

## Identity phase completion evidence (2026-10-06)

All remaining 13 unmutated owners and their literal recipes now execute actual entity/name construction and preferred display, stable-id alias rename, ordinary REDIRECT prose and direct typed details. The complete suite has **24** owning subjects (including independently owned names and deferred ownership), and the whole catalog has **223 effective mutations**, with green unmutated controls and exact intended witnesses; malformed SQL, stopped suites and no-op edits are never kills. Obsolete redirect/moving-prose mutations were replaced with direct owner/key/id reservation, preferred FK, combined-name FTS insert/update/delete-aggregate and journal identity guards. Added explicit mandatory/deferred/absent-owner, typed-person FK and BEFORE-vs-AFTER insertion guards. Deliberate damaged-journal selection isolates its semantic guard from independent alias/FTS protections; restoration is not claimed for that throwaway fault fixture. The temporary selected-owner harness is removed.

Current-truth storage references, lifecycle diagram, D5/D20 and touched D3/D4/D6/D7/D8/D9/D14/D18/D19/D22/D27 text, recipes, Q9/Q10/Q14/Q17/Q21 and suite/count guide now use canonical identity/registry/FTS concepts. Integrity's changed-value claim is explicitly structural-only, not an all-checks claim. Tags retain the existing extraction/rendering behavior and never rewrite prose; REDIRECT is ordinary text/tag, not a stub state. File/no-embed tests use valid addressable names; invalid incoming handles refuse new/ghost writes while an already-kept original uses its stored valid preferred name. CLI/API/client/core canonical test SQL is adapted without relaxing origin/authority/cancellation/request-bound/source registration/replay/reader safeguards. Habit rollback scans and recipe setup errors are checked. Full API now runs past its prior setup panic.

### Prototype guarantee retirement map

The retired isolated `internal/db/identity_prototype_test.go` and all its prior copies remain preserved in checkpoints. Its guarantees are now owned by actual canonical tests:

| Prototype guarantee | Canonical proof |
|---|---|
| non-NULL/deferred/composite preferred ownership, missing/other owner refusal | `name-ownership`, `names` and `TestNameOwnershipAndJournalReservations`; mandatory/deferred/absent-owner/composite-FK mutants |
| stable IDs, multi-alias/case/select-owned rename, reservations/tombstones/rollback | `renames`, `names`, `TestStableRenameKeepsIdentityAndOldReferences`, `TestStableNamesRevivalSelfAndRollback` |
| every uniqueness-target/no-op/spelling UPSERT, cross-owner targetless skip, old aggregate excluding inserted row | `TestCanonicalNamesSearchConflictPathsAndRebuild` plus AFTER-insert/aggregate/name maintenance mutants |
| body once, alias + body field queries, deterministic adjacent-alias phrase limitation | `TestCanonicalNamesSearchConflictPathsAndRebuild`; explicit `body:` no-alias negative and `all_names:` phrase positive |
| ranked limited one-row-per-id results, stable tie and body snippet | `TestCanonicalSearchRankLimitAndStableTie` |
| rebuild, independent FTS damage/check, bad-owner semantic diagnostics | `integrity`, `TestCanonicalNamesSearchConflictPathsAndRebuild`, `TestIntegrityDetectsDeliberatePreferredOwnerDamage` |
| raw + NFC reference/embed/display forms, bracket/markup/Unicode/calendar refusal and real save/rename pipeline | `TestReferenceNameContractProductionPipeline`, `TestReferenceGrammarRejectsUnaddressableNamesAtomically`, `pages`, `save-contract`, `doc-save-contract`, `title-fuzz` |

`prototype-production-mapping.log` records the focused actual-production proof before retirement. The whole `phase-final-baseline.log` covers canonical packages/transport suites and all mutations, including importer/MCP and prior receipt/parity/regression work. No prototype-only implementation remains as application proof.

### Exact final gates and boundaries

Windows/amd64, Go 1.27.1, unchanged modernc.org/sqlite v1.60.1. Separately logged `go generate ./...`, `go vet -mod=readonly ./...`, `go test -mod=readonly -count=1 ./...`, `go test -mod=readonly -count=1 -shuffle=1791286054253722900 ./...`, and diff/preservation checks (`phase-final-*` logs). Staticcheck/govulncheck/mmdc unavailable; no installs, Linux/race/rendering claims. Candidate review must retain these environment limits. All 14 immutable supplied inputs, dependencies, the protected authority.go/authority_test.go pair, AGENTS and plan071 remain byte-identical; approved canonical SQL fixtures in other security suites (notably API origin setup in api_test.go) were changed while authority/origin policies and positive/negative assertions were preserved. Index empty; canonical/generated equality and exact checkpoint hashes independently checked. No real/default database, other worktree, staging/commits, freeze or downstream work. Earlier failed/intermediate logs below remain historical, not the current gate disposition.

## Active identity transition (2026-10-06)

The canonical schema now absorbs body/day into `entities`, replaces `pages` with `entity_names`, uses owner-checked deferred preferred-name ownership and direct typed FKs, and adopts the approved single-document combined-name FTS. Stable-ID rename, aliases, raw-plus-NFC validity, ID-deduplicated/self-excluding saves, alias-aware reads and seeded-Mood-by-ID validation are actual production code. There is no compatibility pages table/view. Earlier checkpoint descriptions below are archived intermediate states, not current source claims.

A new native supervisor reply through contact_supervisor approved a precise refinement: a canonical journal-day identity has **only its canonical ASCII date registry name**, no additional aliases; spelling/preferred/day/type transitions must enforce this, and dated ordinary notes/files remain aliasable. Supervisor supplied the native day-policy reference `6fed6110-2364-4cbf-9bb5-361954ae1e27` in its checkpoint approval. The current `entity_names_day_insert`, `entity_names_day_update`, `entities_day_identity`, `entities_day_page` and `entities_day_page_plain` rules implement that refinement. `TestNameOwnershipAndJournalReservations` exercises construction, capture reuse, revival, direct SQL refusals and a dated-note positive control. `TestDatedFileRenameKeepsOriginalAndOldEmbed` is the file positive control: original hash/MIME/body/day/preview remain; both names resolve the same file, an old embed is not duplicated and re-saving it retains the endpoint.

Meaningful pre-fix red: `TestStableRenameKeepsIdentityAndOldReferences` returned replacement id 5 instead of id 2; `TestReferenceGrammarRejectsUnaddressableNamesAtomically` accepted `Lab [old]`. Exact red output: run-local `transition-red.log`. Both regressions now pass.

New canonical tests in `internal/core/names{,_storage,_search,_grammar}_test.go` pass: repeated/case-only/owned-alias rename, token/no-op/rollback, self and ID dedup, tombstone revival, owner-checked missing/foreign preferred commits, immutable name ownership/key/deletion, day reservations, Mood-after-rename values/habits/corrections, all actual FTS conflict/UPSERT paths, cross-alias phrase limit, ranked limits/ties/body snippet, spelling/body updates, rollback, rebuild and explicit index damage. Thirty language-neutral addressability vectors in the sole contract home feed actual writer create/save/re-save/resolve/repeated-rename/promotion tests; contextual delimiter/reference/emphasis limits stay explicit. `cmd/aminte/names_revision_process_test.go` now passes an actual built-executable stable-alias, >2^53/max-int string token, stale/timestamp refusal and persisted-state smoke test. Production file embed dedup resolves rendered embed names to IDs; its canonical renamed-file regression passes. Name counter exhaustion/no-op/rollback and deliberate foreign-preferred ownership damage are also covered on canonical storage.

Mechanical SQL adaptation is bounded to production core SQL literals (scratch script retained externally); it introduces explicit preferred-name joins, not a facade. Some redundant same-id entity joins remain to simplify during final query inspection. Importer Apply no longer refers to stub state; its alias-aware ordinary lookups use the shared core. Importer Find/look-alike enumeration now includes retained names and keeps one canonical discovery result per identity, including metrics and people's full names.

**Outstanding work before readiness:** all obsolete pages fixture SQL and redirect/unsafe-name expectations must be adapted against the actual new contract, preserving the revision/date/link/Mood proofs; canonical mutation witnesses/mapping and retirement of the isolated prototype; current-truth ADRs/cookbook/entity diagrams/metadata/integrity/name contract and totals; stable-name HTTP/HTML/MCP parity; production file import/alias/preview workflows; complete baseline/full mutants/shuffle/available analysis/diff and preservation gates. The initial broad feedback run failed, not passed: mostly old pages queries, redirect expectations and formerly accepted bracket/entity-encoded filenames. Those are recorded in `transition-feedback.log`; no test skips or compatibility layer were used. Current canonical table/FTS/view/trigger counts are 11/1/3/48; mutant coverage is not yet updated and **217 passing preparation mutants are not evidence for this source**.

### Compatibility continuation (2026-10-06)

All three independently reproduced importer defects now have permanent canonical regressions in `internal/importer/alias_continuity_test.go`: alias Find, unchanged reading reapply after metric rename, and unchanged vault reapply after owner rename. Owned-name discovery preserves preferred display, full-person-name matching, metric underscore matching, tombstone filtering and explicit look-alike/Distinct protection. Reading roots load their metric/captured-with owned normalized keys; source keys and original readings are never rekeyed. Alias-equivalent groups use invocation-local owner IDs only for grouping, never as persisted source identity. Repeated/case-only/owned-alias renames, equal-valued timeless readings, timed readings, captured-with NFC/aliases, correction retention and transactional timed-alias collision refusal are covered. All legacy ambiguity/correction/rebuilt-ID/replay controls pass. A new meaningful red caught an overly broad historical-namespace ordinal inference: mixed raw `ferritin|1` / `FERRITIN|2` roots were incorrectly accepted. The repair preserves the single-raw legacy path and permits combined aliases only with independently canonical key spellings; the new refusal and complete importer suite pass.

Native supervisor approved the writer-owned workspace identity receipt. It stores only source/logical note path/original exact title/day and a narrow completion marker: no database IDs, bodies, alias history, source copies, dependency, schema history or new agent/admin action. Preparation is re-read and verified under the workspace lock and `BEGIN IMMEDIATE`, after source/path/plan validation and source reads; a receipt failure prevents first entity writes. Uncommitted create preparations remain editable/retryable, bound entries are not replaced, missing/corrupt/source-conflicting evidence refuses, and unknown append completion is fail-closed. Receipt metadata is filesystem evidence, not cryptographic protection or a jointly atomic SQLite/filesystem commit.

Supervisor reply **`1fd9fa35-59a8-4e6b-9a28-0ab97391bb2d`** explicitly approves a new real-replay restriction: draft rehearsal remains supported on a disposable target without receipt/trial mutation, but real replay requires successful authoritative-trial application/evidence before creating or modifying its target. Supervisor reply **`4b9fd8b0-307d-44ad-85a0-408b46633043`** approves the necessary per-entry `applied` completion bit: entity creation alone cannot prove the later body/append pass completed. New entries begin false; true is atomically published only after all body/append work and plan persistence succeed. True belongs only to the same verified binding and is not owner approval, current-body/source equality or joint atomicity. Completion failure reports an error even when SQL has committed; unchanged retries repair metadata without duplicate appends. Real replay carries the validated plan snapshot and never derives authorization from fresh target absence. The application workflow in README documents this policy.

Tests prove absent/malformed/wrong-source/exact-case receipts, applied-plan owned-alias tamper refusal, prepared-but-uncommitted create edits, first receipt file failure, later body failure after entity creation, plan-persistence and completion-publication fault injection, completion interruption (not process/power-loss proof), partial/mixed plans, unchanged retry, append-once recovery, and owner later prose/rename compatibility. The narrow typed publisher seam substitutes only receipt storage in deterministic failure tests; normal execution uses existing atomic workspace persistence.

New API/MCP stable-name tests cover page/person/place/metric/file repeated/case-only/owned-alias renames and old-alias resolution over in-process and actual HTTP-backed clients/SDK calls. API checks retained prose/day/details/backlinks and ghost/tombstone/date/grammar conflict side effects. Browser typed rename form/303, preferred heading and old-reference ID rendering pass. This is added parity evidence, not a claim that all remaining browser/adapter cases or broad API tests passed.

Current validation: complete importer and MCP suites pass (including fixed shuffle seed **1791286054253722900**); focused API name/revision/browser shuffle passes that same seed; actual executable stable-name/exact-token smoke passes. Generation and vet pass. Full uncached baseline still fails in canonical contract/shared fixtures/mutants, obsolete cmd/client/core/API setup and old changed-policy cases; the broad API panic still hides later cases. No canonical mutant kills or full shuffle pass are claimed. Staticcheck/govulncheck/mmdc are unavailable; no tool installed. Windows/amd64 only, Go 1.27.1, modernc.org/sqlite v1.60.1; Linux/race/rendering remain unrun. Exact final logs and fresh checkpoint hashes are in the authoritative runtime packet.

Remaining work is still owned phase 1, not optional follow-up: shared legal entity/name constructors and cookbook interpreters first, all current-truth contracts/ADRs/metadata/diagrams/totals and effective mutants, remaining changed-policy/security-preserving fixture adaptation, full adapter/file preservation and canonical prototype mapping/retirement, and complete baseline/shuffle/available-analysis gates. Criterion-1 is **not satisfied**; downstream ownership remains stopped. All seeded inputs, dependencies, authority implementation/tests and plan 071 remain unchanged; index empty.

### Contract-root continuation (2026-10-06)

Priority roots are now changed, not deferred behind importer features. `tests/kit_test.go` constructs the actual entity and owned preferred name atomically in nested savepoints; typed fixtures wrap their extension in the same outer savepoint. The general arbitrary-identity fixture has one descriptive actual name, not a temporary alias. Direct DDL probes are explicitly separated from writer saves. New `TestCanonicalIdentityFixtureOwnershipAndRollback` proves actual spelling/body/day, exactly one name, duplicate-name owner rollback, typed details, outer-transaction rollback and journal ownership with FKs enabled. Remaining old split-table call sites still need semantic conversion, not a facade.

Canonical capture/save/rename/backlink SQL and owning interpreters now use the real registry. Stable rename retains identity/prose/day/provenance/extensions/exact incident link rows, supports case-only and owned-alias selection, and refuses another owner's ghost/tombstone/date or a deleted/journal identity without side effects. Save resolves/deduplicates by ID and excludes self aliases; literal REDIRECT text is extracted ordinarily. The generated title test now enforces writer acceptance as a subset of DDL acceptance and executes addressability forms for DB-only generated names: CommonMark cannot be a SQLite CHECK. This replaces the obsolete exact-mirror claim without weakening filename controls.

Unmutated roots pass: dates **246/246**, renames **56/56**, production save contract **25/25**, literal document save **13/13**, title-fuzz **3/3**, diagrams **219/219**. Regular/virtual/shadow enumeration now uses SQLite's table classification, not the obsolete pages_fts prefix. Entity model key/FK diagrams and schema object list/totals reflect actual entities/entity_names/direct extension ownership and 11 tables/1 FTS/3 views/48 triggers. Other current-truth ADRs/recipes/2075 answers remain unfinished.

`TestMutantWitness` now passes its unchanged canonical date mutation control. `TestCanonicalDateMutants` runs all **14 date mutations** from the existing catalog against a green owning suite and observes their intended assertions without setup stops. This is actual scoped effective coverage, NOT a full mutant gate or an updated effective total. The remaining catalog/owning suites are still red.

Exact current validation logs: `contract-generate.log` and `contract-vet.log` pass; `contract-baseline.log` fails; full uncached shuffle seed **1791286054253722900** fails (`contract-full-shuffle.log`); `contract-diff-check.log` passes. Complete importer/MCP continue passing in the full baseline (44.858s/6.011s). `contract-root-focused.log`, `contract-save-feedback.log`, `contract-date-mutants.log`, `contract-docs-feedback.log` record focused repair evidence; latest diagram run passed after correcting the preferred-owner FK edge and removing the obsolete redirect map edge. No static-analysis/render prerequisite was installed. Remaining security/application setup is not yet reached in priority order, and broad API panic still prevents a coverage claim.

Remaining owned work: finish identity/named/pages/files/facts/journal/links/integrity/import/evolution/snapshot/cookbook roots and their intentional-damage cases, align all current-truth text and 2075 questions, restore the full meaningful mutant catalog/prototype mapping, then clear application fixtures/policy tests and rerun complete gates. Criterion-1 remains **not satisfied**; no downstream transfer, staging, real data, dependency or freeze operation.

### Integrity-owner continuation (2026-10-06)

Corrected two parent findings first: the production addressability-vector reader normalizes CRLF/LF without changing its sole documented vectors; `TestCanonicalIdentityFixtureOwnershipAndRollback` now explicitly opens a file and reopens it read-only to prove owned prose/names/journal persistence and absence of rolled-back names/details (ids may be reused). Its prior memory-only wording was not file-backed evidence.

The literal integrity contract and owner now use actual preferred-name ownership and direct typed extensions. All corruption setup/file/stat/offset/root/close errors are checked. Zero-page damage uses the actual entities root. Index damage discovers the real unique name-key index via SQLite metadata, traverses its actual index b-tree (bounds/type/cycle checked), tests candidate damage until integrity detects a live effect, and verifies authoritative table key counts remain unchanged. Body-byte damage independently proves a changed live body with structural integrity still clean. A fully named plain entity is valid; deliberate missing-name damage is caught by FK and semantics, and named person/metric/file identities missing their extension are semantic-only faults. Typed-edge damage checks successful setup, and actual single-document FTS drift/rebuild/deleted-content probes are retained. Unmutated integrity is **19/19** green; no negative slice/setup panic remains there.

Identity/provenance/import recipe execution is now legal in transactions, with meaningful type/source/import-key duplicate controls and actual owned-name replacement refusal. The canonical import-once recipe stores body on entities and creates its owned name; source namespaces and null keys use independently named real identities. Identity is **90/90** green. Snapshot old-table probes now use actual entity/name state; snapshots are **23/23** green. The temporary converted-owner mutation harness now proves **19** effective mutations (14 dates plus five integrity) against green owners, without claiming the whole catalog; remove it once the complete catalog is healthy.

Current exact gates: generation/vet/diff checking pass (`integrity-final-{generate,vet,diff}.log`); full baseline and full fixed-seed shuffle **1791286054253722900** fail (`integrity-final-baseline.log`, `integrity-full-shuffle.log`). Complete importer/MCP remain green in full baseline (48.099s/5.639s). Portable production vector test passes. Focused proofs: `integrity-owner-feedback.log`, `integrity-identity-snapshot-green.log`, `integrity-mutants-feedback.log`; full unmutated residual roots in `complete-contract-feedback.log`. No full mutant pass/readiness is claimed.

Remaining owned roots: named/pages/files/facts/places/journal/habits/writers/imports/evolution/links/cookbook/document and all corresponding current-truth recipes/ADRs/2075/metadata, whole canonical mutation catalog/prototype mapping, then remaining application/security fixtures and full gates. No importer feature expansion, new policy, dependencies, real data, staging, downstream work or freeze. Criterion-1 remains **not satisfied**.

### Current checkpoint disposition and validation

Supervisor `4453a405-8453-4e5d-837e-3f6e90637641` approved bounded preservation after the production transition consumed the retained context. It explicitly does **not** accept readiness or defer any owned proof. No child compaction control is exposed; no alternative model/CLI/protocol or session-history edits were used.

On this actual source: generation and vet passed; full uncached tests failed; diff checking initially found SQL-literal trailing whitespace, which was fixed and rechecked successfully. Full tests were rerun after that formatting-only repair and remain failed. Focused canonical core plus preserved correctness, API/HTML revision parity, MCP SDK revision parity and executable CLI smoke all passed (`transition-final-focused.log`). A relevant focused core shuffle passed with seed **1791281219463491600**; full shuffle is unrun while the full baseline is red. Staticcheck/govulncheck remain unavailable; Linux/race/release execution and final changed-diagram rendering are unrun. There is no new canonical mutant pass: TestMutants stops on the unmutated fixture's missing preferred key, before mutation evaluation. API's broad suite panics on legacy fixture SQL, so later API cases are not claimed exercised by that run.

Cold-start failure/repair map and exact commands/checkpoint source hashes are in the runtime-bound packet and run-local transition artifacts. First repair: re-home the test fixture constructors and SQL cookbook insert/save convention onto the deferred entity/name pair, then deliberately adapt damaged fixtures, named rules/recipe assertions and mutation witnesses; do not give C.ent an arbitrary default alias or add a pages facade merely to make setup green. Rebuild existing independent preservation tests against that convention before resolving unknown production behavior in importer look-alike/replay/alias paths. Stable-name HTTP/HTML/MCP parity and complete contracts/diagrams/totals remain owned. Retire the prototype only after mapping every remaining guarantee to canonical regression/mutant coverage. Criterion-1 remains **not satisfied**.

## Archived correctness checkpoint

That checkpoint still had separate `entities` and `pages`, immutable titles and legacy replacement-ID/redirect renames. It does **not** claim direct aliases, stable renames, final addressability grammar or new FTS adoption. It now has canonical clock-independent edit revisions, stricter exact-time checks and the correctness changes below. Current-truth docs describe that actual intermediate candidate, not the proposed final model.

Only disposable synthetic fixtures were opened, with production connection settings. No original checkout/index, real data, source export/workspace/media, dependency, authority policy, migration, staging, commit, import, replay, rebuild or freeze operation was authorized or performed. All 14 seeded RFC/issue/index inputs are preserved byte-for-byte. Generated DDL is refreshed only by `go generate`.

## Native supervisor decisions retained for continuation

These are candidate approvals, not final owner acceptance. The continuation brief supplied the actual references for earlier native replies:

| Reply reference | Disposition |
|---|---|
| `1c986998-1cf2-4c22-a9e7-48a102b18ee2` | Approve preferred-key ownership, symmetric relationship notes and exact ASCII time; challenge initial grammar proof and reject body-per-alias FTS. |
| `08529948-1707-40b2-be07-172792023222` | Approve single-document combined-names FTS and raw-plus-NFC grammar; supply skipped-INSERT corruption witness requiring repair. |
| `4e5a7e2a-feb5-4a46-9934-58b8a4cde6c7` | Approve first bounded proof-only checkpoint; production completion explicitly not accepted. |
| `15731fe0-528c-44a2-a797-1d438cb109ce` | Approve second PARTIAL/BLOCKED checkpoint of actual correctness/revision preparation; finish coherent contracts, effective mutants, explicit parity/full validation and durable evidence; do not begin the combined-name transition in remaining context. |

Approved remaining identity design — do not rediscover:

1. `entities.preferred_name_key` NOT NULL, deferred owner-checked composite FK `(id, preferred_name_key)` to `entity_names(entity_id, name_key)`. Global unique normalized keys, immutable owner/key, retained/reserved aliases including tombstones. Entity then name in `BEGIN IMMEDIATE`; no authoritative spelling copy, compatibility pages facade or identity-changing merge.
2. One FTS document/entity with preferred spelling, deterministic ordered combined all-names field and body once, derived external-content JOIN view. Exact alias lookup remains registry lookup. Phrases may span adjacent aliases in the combined field; they do not prove one literal alias contains that phrase. Preferred spelling in both short fields is acceptable. Preserve live filtering, stable ties and snippet field references. Alias insertion and preferred selection may each reindex; no performance/one-reindex-per-rename claim.
3. Existing filename/Unicode predicate plus no brackets, and lower-level CommonMark extraction must yield exactly the normalized key in `See [[name]].`, `See ![[name]].`, `See [[name|display]].` for **both raw and NFC spelling**. Avoid recursive validity/Targets calls. Safe underscores, bare ampersands and isolated single backticks are not blanket-banned. Surrounding code/emphasis/reference definitions can suppress references; isolated addressability is not arbitrary compositional safety.
4. Symmetric relationships have one shared note; directional notes remain independent. Exact days/instants use ASCII fixed widths, years `0000..9999`, hour `00..23`, fixed milliseconds and calendar round trips. No partial people dates/period profiles.

## Implemented versus pending

| Issue / seam | Actual implementation and proof at this checkpoint | Remaining |
|---|---|---|
| 0011 / identity | Deferred ownership/alias/FTS mechanism remains **only** in `internal/db/identity_prototype_test.go`. | Canonical pages absorption/name registry/typed direct FKs/seeds/day reservations/FTS/ghost view; shared writer/read/rename/importer/adapter transition and real stable-ID regressions. Legacy renames still lose identity/backlinks. |
| 0012 / revisions | `entities.revision`, monotonic guard and change-sensitive touches for prose/day/type/lifecycle/details/habit periods/incident links. `PageByID` returns decimal string token; `SaveBody` compares it inside transaction, never timestamp. Core equal-clock stale failure, no-op/rollback/typed mutation/cancellation/max-int refusal; API/HTML/MCP local+remote round trips and stale/timestamp refusals. | Name-registry mutation invalidation and stable-rename token tests after transition; explicit new CLI executable stale/max-token scenario still omitted. Existing CLI tests pass but are not that new proof. |
| 0013 / typed graph | `entities_endpoint_types` rejects incoming/outgoing invalidation; `Integrity` reports `invalid_typed_links` independently. Production rollback and deliberate damage regressions; contract semantic query and effective mutants; fault-driver coverage includes query/scan/late/close failures in added stream. | Revalidate against combined core/direct typed ownership and renamed category identities. |
| 0014 / exact time | Every existing exact day/instant CHECK strengthened; single boundary-vector home `contract/exact-time.md` is read by dates suite and compared independently with DDL **and** Go validators on all columns. Negative/expanded/non-ASCII years, leap/endpoints/hour24/fraction/offset cases included. | Carry checks onto absorbed entity day and future phase separately; no uncertain/partial dates implemented. |
| 0015 / Mood | Writer and habit-period insert/update DDL refuse metric ID 1; atomic no-period/no-prose witness and ordinary mood capture control; effective mutants. Old capture rollback test now labels deliberate external damage, not valid Mood registration. | Mood-after-stable-rename and capture/correction identity lookup after aliases; named metric rename is still unsupported. |
| 0016 / grammar | Approved raw+NFC profile remains prototype-only, with punctuation/Unicode/NFD/U+1FEF and cross-reference delimiter controls. Production predicate unchanged. | Adopt contract vectors/nonrecursive production validity and real create -> save -> resolve/embeds/display/re-save/self-alias/resurrection pipeline. |
| 0017 / link identity | `links_fixed` protects ID and creation time alongside each existing fixed field; independent attempted mutations and persisted-state control. | Revalidate during final identity transition. |
| 0018 / notes | Terminating `links_mirror_note`; both directions/NULL/no-op/self/directional/unrelated/rollback controls under recursive triggers; storage mutant. | Revalidate alias/stable-ID graph lifecycle; no new note UI/action was introduced. |

Counts are **11 tables + 1 FTS + 2 views + 41 triggers**, **217 effective mutants**. Two genuine cross-table rules (`edit_revisions`, `typed_links`) have metadata and 2075 questions; the document suite's small-rule cap is updated from eight to ten for those explicit additions, not to conceal unexplained keys. ER diagrams draw key/FK fields only, so the added non-key revision and trigger changes do not require diagram edits.

## Regression and mutant evidence

Actual production pre-fix failures, observed before fixes:

- `TestPromotionRefusesRetainedTypedEdges`: both incoming and outgoing promotions accepted invalid retained edges.
- `TestMoodHabitRegistrationRefusedAtomically`: Mood registration succeeded.
- `TestLinkFieldsImmutableAndSymmetricNoteShared`: `id=id+1000` was accepted.
- `TestConditionalSaveRejectsEqualClockStaleToken`: stale save returned nil after committed audit time was restored deterministically to the prior clock value. This is not a millisecond collision search.

Final owning regressions are green; state assertions include retained type/body/token/edge/detail state, no forbidden target/prose/period, rollback/cancellation and exhaustion. `TestEditRevisionExhaustionAndCancellation` proves max token `9223372036854775807`, exhausted true no-op, refused changed body/lifecycle/decrease and obsolete timestamp token refusal.

Effective new mutants cover endpoint guard, link ID/time/shared note, Mood insertion/update, revision constraint/monotonicity and every revision touch trigger, exact-day shape/hour24/canonical instant representation, and typed semantic destination checks. A proposed instant-shape-only mutation was **not killed** because that subcondition was redundant with SQLite's round trip for the attempted negative-year value. It is not claimed independently enforced: the final mutant removes the actual named `entities_created_at` representation CHECK and fails the explicit behavioral rejected-vector witness on valid mutant DDL. Counts include that meaningful rule mutant, not a setup failure.

Old mutants were adapted, not skipped: new ASCII shape makes malformed-width `= vs IS` witnesses redundant, so valid-shape invalid-month NULL cases now isolate `IS`; updated trigger headers/counts retain their old clock witnesses. The legacy file recipe's redirect-stub preflight is tested with the independent transition guard explicitly disabled in a disposable fixture so the recipe mutant remains effective rather than masked. No blanket test weakening or retries.

Earlier prototype red/green is preserved externally: BEFORE INSERT FTS deletion corrupted committed `ON CONFLICT DO NOTHING`; AFTER-actual-INSERT reconstruction excluding NEW.id repaired it. All uniqueness/no-op/spelling-UPSERT paths, rollback, ranked limits/rebuild/damage are in the retained prototype. Raw-only U+1FEF addressability failed; raw+NFC tests repaired it. No prototype guarantees are misrepresented as production named-core behavior.

## Adapter proof and limits

- `internal/api/revision_parity_test.go`: in-process and actual HTTP transport, `cli`, `api`, `ui`, `agent:revision` shared clients; equal-clock stale and old timestamp refusal with no target creation; exact max-int string in properties/action prefill and submitted no-op/exhaustion; 409/422 outcomes.
- Browser HTML GET/prefilled form/POST: exact `9007199254740993` token, successful 303 save, stale/timestamp 409, persisted next token `9007199254740994` and no target.
- `internal/mcp/revision_test.go`: real SDK tool calls over in-memory MCP transport with in-process and remote HTTP backing; lossless large string read/save; equal-clock stale/timestamp `IsError` plus semantic status 409, no forbidden side effects. Synthetic test server only, not live tools.
- Existing CLI/importer/API/client/MCP suites exercise unchanged wiring and compatibility; no new CLI subprocess token regression or real browser automation. Owner-only/authority/path policies remain unchanged.

## Cold-start continuation map

Next bounded task is the approved canonical identity/name/FTS plus coupled writer/read/rename transition, **not more proof discovery**. Preserve current correctness/revision tests and adapt their ownership queries rather than discarding them.

- `docs/schema/schema.sql`: absorb pages fields into entities; preferred key/name registry, direct typed FKs, day-name reservations, Mood seed, single-document FTS/content view/safe AFTER-insert maintenance, ghost view; remove pages and redirect kind/logic. Add name/alias revision invalidation; regenerate embedded schema only through generation.
- `internal/core/write.go`: `insertPage`, `Lookup`/`lookupKey`, `pageByID`, `syncWikilinks`/`linkTarget`, `CreatePage`/named/Promote/SaveBody/SetBody/Record/correction lookup. Resolve aliases then deduplicate IDs; ignore self-alias without reviving/linking self; ghost conflicts refuse; targets remain SAVEPOINT-isolated.
- `rename.go` `Rename`: stable ID, existing owned alias selection, case-only spelling, retained old names/body/import keys/readings/edges; no merging/taken ghost reuse/stubs/day transfer; no-op and exhausted token behavior.
- `read.go`, `habits.go`, `files.go`, `places.go`, `imports.go`, `categories.go`: actual name joins for all reads/writes/backlinks/search/snippets/day/file workflows/Mood capture and corrections. Dependent SQL in importer/API/CLI must change together; thin adapters remain shared-core callers.
- `internal/text/text.go`: base predicate plus approved raw+NFC lower-level extraction checks; contract's single vector set must feed actual create/save/resolve tests. Keep post-parse NFC and known Markdown context limits.
- Current-truth identity ADRs/contract/cookbook/metadata/threat model/diagrams/totals and `tests/{identity,named,pages,renames,wikilinks,links,dates,habits,integrity,mutants}` transition in step. No compatibility pages table/view or semantic stubs. Full baseline/analysis/shuffle and final parity required before phase acceptance.

Temporal-records, import-interpretation and integration remain pending and have no ownership transfer. No sessions, periods, scoped readings, provider/sidecar features were added.

## Validation and preservation

Windows/amd64; Go 1.27.1; SQLite 3.53.4. Final exact commands/logs, full shuffle seeds, changed-file list and checkpoint path are recorded in run-local `artifacts/identity-core` and the runtime-bound handoff. Focused production/core/API/HTML/MCP cases and contract tests/full 217 mutants passed before the final baseline. Final baseline success is required for the checkpoint, not sufficient for phase completion.

Staticcheck/govulncheck are not available on PATH; no tools/dependencies installed. Linux race/cgo and Linux release execution are unrun; no cross-platform claim. Diagrams unchanged, so render check is not applicable. Full diff/generated/untracked inspection and input/dependency/plan-071 hashes are retained externally. The checkpoint helper saves binary tracked patch, new-source copies/hashes and manifest without staging; both earlier checkpoint and archived proof handoff are preserved. Criterion-1 remains **not satisfied**.

### Temporal candidate implementation and proof gate

The ongoing-horizon regression was first observed RED against independent enumeration (15/30 possible
September starts), then repaired without altering the finite ordered-pair formula. The permanent horizon
owner retains horizon-invariance, start-after-horizon and observed-finite-end contrast witnesses.

Canonical adoption now includes named `periods`, independent untitled `sessions`, and nullable measurement
session scope. Shared core operations capture/promote/edit/query periods, capture/retry/edit/tombstone/revive/query
sessions, preserve same-scope corrections, and atomically relocate ordinary unkeyed owner leaves by retract/new-root.
Relocation checks imported/keyed ancestors; no source-format ingestion or imported relocation was added. Session
kind IDs survive aliases/rename; retained kind references refuse type reinterpretation. Session time evidence and
reporting day remain independent; exact calendar-wide integer elapsed and exact string revisions have persisted proofs.

The current contracts own boundary/endpoint vectors and lifecycle/scope rules. The semantic integrity group adds
actual session-kind and correction-scope damage detection; its four groups now comprise seven literal statements.
One new cross-table `measurement_scope` metadata key takes the total to eleven; the document owner/count mutant
changes with this legitimate rule. Canonical totals are 13 regular tables, one FTS virtual table, three views and
60 triggers. Mutants are **273 effective copies**, including 40 temporal additions beyond accepted identity's 233;
all owners first complete green and each copy must fail its exact intended witness. SQL/profile no-op mutations,
unrelated failure and setup stops remain rejected. Cookbook count is 23, with explicit unassociated and labeled
all-scope recipes rather than silently adding session totals.

Meaningful writer tests cover correction/relocation refusal and rollback after retraction insertion, lifecycle,
retry/rename/source-key preservation, stale/no-op/exhausted revisions, full-calendar elapsed, canceled calls,
reopen and real semantic damage. Thin adapter proofs exercise in-process and TCP HTTP, actual HTML capture/labels,
MCP SDK capture/edit/scope and absence of owner relocation, and compiled executable period/session/scope/relocation.
Direct agent sessions remain explicitly counted as outside importer replay; trial/target session counts are visible.
The importer receipt/completion/recovery protections and their full baseline remain green; no ledger/intent is bypassed.

Validation: generation, readonly vet and uncached all-package baseline pass; all 273 mutations and unchanged controls
pass separately; full shuffle-on and reproducible shuffle seed 20261006192455 pass; document/diagrams/cookbook and
diff checks pass. Exact commands/output, protected-input hashes and durable source checkpoint are retained under
external `artifacts/temporal-records/`. Windows/amd64 only; staticcheck, govulncheck and mmdc unavailable (not installed),
Linux/race/rendering not claimed. This is a checked **candidate for parent temporal review**, not parent acceptance,
owner/RFC/release acceptance, real-data permission or authorization for subsequent phases.

### Temporal checked-review repairs

Parent withheld temporal handoff acceptance after independently validating checkpoint
`20261006T180518Z-b86d735c` and reproducing four concrete faults. That checkpoint and its checked report
remain preserved; the native checked status was not parent acceptance.

Permanent regressions first reproduced RED: NUL-tailed qualified period boundaries and session evidence
committed despite writer refusal; new period category selection accepted a tombstone; temporal collections
omitted member links and later heterogeneous evidence; range expansion dropped scope/session/history.
The existing paths now refuse raw NUL before normalization, require live targets on new period selections
while retaining existing edges, expose collection member links, render the union of object columns, and
carry all selected query coordinates when expanding a series range. Source syntax is guarded separately;
opaque import keys are untouched. Shared raw boundary/endpoint/category vectors and eight selective
mutants supplement the original proofs: **281 effective mutations** with exact intended witnesses.

Required risk witnesses are now permanent: actual request cancellation after an observed first relocation
retraction, unchanged reading/session/counts and reopened file; daily habit and Mood independence from scoped
facts, registration/value/correction/relocation and tombstone/revival; durable same-database workspace value
correction interrupted after publication, session reporting-day edit, reopen/recovery/retry with unchanged
reading scope/attribution. Imported/keyed relocation and an unkeyed owner leaf over that chain refuse without
SQL or workspace file mutation. This uses existing correction intents unchanged: no portable session mapping,
source ingestion or intent redesign. The narrow per-call relocation fault boundary is internal, not a detached
recovery framework. D22 index, scoped overview/zone wording, duplicate type literal and touched expressions
have been aligned; independently gated canonical-data/freeze claims were not changed.

Parent's supplied storage/navigation overlays are now green. The current-source readonly vet, full uncached
baseline (including all 281 mutants), full shuffle seed 1791286054253722900 and diff/preservation gates are
retained in external `artifacts/temporal-records/repair-*` evidence. Missing-start/imported-relocation profile
limitations and Windows/static-analysis/rendering gaps remain. This repaired candidate requires parent review;
no phase transfer, owner acceptance, real-data permission or freeze is implied.


### Import-interpretation transfer and policy gate

Parent accepted temporal checkpoint `20261006T192421Z-ac1f0119` after independent source/probe/full-gate review,
recorded in external `artifacts/temporal-records/parent-review-repair-review.md`; the actual report is preserved
as `review-repair-handoff.md`. Owner said “ok. continue”; parent explicitly assigned import interpretation only.
Accepted identity and temporal rules remain settled. The two temporal test-hardening notes belong to subsequent
integration, not this source component.

Read-only inspection identified missing export-shape/unit/reporting-day evidence as limits rather than invented
coverage. `artifacts/import-interpretation/proposal-01.md` proposes bounded source profiles, explicit reviewed
identity/day/metric bindings and portable session source/key references, existing workspace approval/retry/replay
integration, and explicit selected-photo pairing with no capture-day fallback or automatic conflict precedence.
The independent disposable arithmetic oracle uses synthetic values only; it proves neither export coverage nor
source semantics. A native supervisor disposition is required before production/DDL/current-truth/test adoption.
This is proposal-only preparation, not completed import interpretation or owner/RFC/freeze acceptance.


### Import gate disposition delivered by continuation

Parent approved with adjustments in `artifacts/import-interpretation/parent-gate-01-decision.md`, delivered
by this retained native continuation. The expired request `2b2737c0-4fb9-4694-ac54-666a8f70531f` did not receive
a reply; its proposal checkpoint remains preserved. `established-shapes.txt` is previously produced shape-only
evidence, not a new export inspection. Exact supported profiles are Fit Date CSV explicit selected columns,
legacy sleep root array, and Fit root object with explicit reviewed identity/day/activity binding. Health and
exercise variants and automatic Fit aggregate interpretation remain unsupported. Source creation claims are
not established upload/import time, and bare epoch claims are not local capture days. No intent redesign or
new DDL tables is authorized.

The first approved source slice adds bounded fixed-profile decoding and confined snapshot preparation, lossless
source IDs, independently attributed sleep dates and distinct quantities, and visible excluded summaries. It
also implements the explicit unknown-photo-day core mode and selected-file adapter projection, including no
place/day side effects and preservation of existing owner attribution. This is partial component implementation: typed
artifact approval/binding/replay/catalog integration and reviewed sidecar pairing/conflict handling still remain.

### Targeted retained continuation after tooling-stop review

The parent verified checkpoint `20261006T212931Z-3c3782bf` and cleared the diagnosed local
Python default-codec helper failure; ordinary compiler/test/helper errors remain task errors, not
infrastructure outages. `parent-tooling-stop-review.md` supplies the controlling current review.
No model/protocol change, broad policy re-gate or downstream acceptance occurred.

Permanent owning regressions reproduce the three selected-workflow groups RED, then the supplied
overlay passes GREEN: offset syntax, absent/skipped ledger admission, and conflicting retained-day
promotion. Capture agreement is enforced inside the core file transaction, including hash dedup and
plain-page promotion. Artifact hash and parsing use one bounded byte snapshot; selected namespace is
bound explicitly. Reservations and completion evidence are distinct, bounded and checked; mere
reservation is not commit evidence. Selected receipts retain multiple applied pairs without coordinate
mirrors. Actual Rehearse/Replay proves multiple selected files, scoped corrected readings and wrong
trial-scope refusal; an actual request cancellation hook returns nil and production observes failure.
Well-formed normalized-record forgery, strict choice decoding, actual EXIF-bearing preview stripping,
owner-hash/namespace snapshots and HTTP/MCP prepared-source proofs are permanent.

### Applied binding and remaining-work continuation

The latest `parent-retained-review.md` supplied a new applied-source reassignment RED. Its permanent
owning test and overlay now pass: receipt lookup cannot select a fresh interpretation by changing
namespace/profile in the same workspace logical file. A separate provider/workspace control remains
independent. Bound session-key changes likewise refuse without SQL/new reservation effects.

Replay now serializes workspace selection mutations across rehearsal and application, revalidates
external artifacts/source claims at the pre-target boundary, and consumes admitted in-memory selection
inputs without reopening them to choose a different interpretation. Tests cover an actual concurrent
namespace mutation and a changed source between completed rehearsal and target initialization.
Reservation/commit-to-completion/publication failures have verified retry/refusal cases; multiple
selected histories retain minimal pair/choice bindings without coordinates. Cancellation observes the
actual inserted keyed SQL root before canceling and returning nil. Genuine metadata-bearing input and
nonempty valid stripped preview are asserted. Actual replay into existing targets with distinct local
IDs, wrong target/trial scope, session-only and unapplied/reserved/skipped selections is exercised.

CLI process, actual HTTP, HTML review rendering and MCP use the shared catalog/core workflows;
owner stamps remain terminal-only. Browser automation is not claimed. Four narrow application mutations
have completed final-tree green owners and exact failing witnesses. Redundant snapshot and namespace
mutations were not counted; independent guards retained their refusals.
Current-source baseline, shuffle and preservation evidence will accompany the final candidate checkpoint.
This is implementation/proof evidence for parent review, never owner/RFC acceptance, real-data import,
integration transfer or freeze. Optional Linux/race/rendering/tool checks remain environment limitations.

### Checked-handoff parent-review repair

Parent withheld import acceptance after independently reproducing false status consistency for missing
prepared roots, lost completed selected history, undiscoverable review navigation and live recreation of
an owner-tombstoned trial photo. The current assignment is `parent-checked-review.md`; broad source and
photo policy remains settled. Permanent owning RED witnesses and controls now pass the supplied overlay.

Status uses original-root/session/scope verification, reconciles completed selected receipts and unexplained
imported sessions, and remains observational. Corrected roots and renamed retained handles are positive
controls. Shared hypermedia links expose existing reviews to actual HTTP and HTML clients without stamp
authority; review evidence is escaped and check/apply actions retained. Selected replay refuses unsupported
trial owner tombstones before target snapshot/creation/mutation, while same-store checks/retries and
existing-target tombstones remain unchanged. Calorie prose now names the actual stable quantity code
`reported-calories`; preparation/application proves the source-reported kcal meaning without rekeying.

Repair evidence includes six effective application witnesses (the prior four plus required-root status
and trial-tombstone refusal) with green owners and exact meaningful failures. Final-tree full gates and
fresh checkpoint remain candidate evidence for parent review, not import/owner/RFC acceptance or transfer.

### Fresh integration checkpoint — partial inspection

The separate fresh integration owner read the controlling packets and accepted dispositions, inspected the
canonical DDL and selected identity/temporal/prepared-import/client/document seams, and retained an explicit
inspected-versus-uninspected inventory under external `artifacts/integration/`. This is not exhaustive cumulative
inspection or completed integration; every remaining file must still receive actual inspection before delivery.

Both reserved temporal test hardenings are implemented: the relocation hook observes the inserted retraction,
cancels the real request and returns nil; production must fail with cancellation or database/sql's rolled-back
transaction result and preserve state through reopen. Collection member tests assert both presence and absence
of live/deleted lifecycle actions while retaining navigation, mixed evidence, escaping and order coverage.
README's stale rename-as-text-move wording now agrees with the stable-ID/direct-alias contract and operation.
The initial checkpoint changed no production behavior, accepted policy, protected input, authority or dependency.

Windows/amd64 validation and exact logs/checkpoint are retained in `artifacts/integration/` and the runtime-bound
integration report. Green baseline/mutants/shuffle and probes do not replace remaining inspection or parent/owner
acceptance. Staticcheck/govulncheck/mmdc are unavailable; no installs, Linux/race, rendered diagrams, real browser
automation, lifetime/stress or power-loss proof. Source remains unstaged and uncommitted; plan071 is untouched.

The parent subsequently authorized a narrow range/navigation repair after inspection found that All readings
used an exclusive `0001-01-01` lower bound despite year0000 being admitted. A production behavioral RED proves
missing early identities and a default minimum-day query refusal. The explicit `all_history=1` mode now selects
no lower bound through the inclusive upper day, preserves scope/history/navigation, and rejects conflicting
`from` input. Ordinary explicit ranges remain exclusive/inclusive. A default 90-day window that would cross
before year0000 uses that same mode. The shared core query, API, browser label and README agree; regression
coverage follows actual links through aliases and active/historical unassociated/session/all scopes with
ordered identity/value assertions and bounded-range controls. This is parent-authorized integration repair,
not a new temporal policy, schema change or proof of exhaustive inspection. Exact RED/GREEN and final gates
remain in external integration evidence; remaining file inspection and parent/owner gates are open.

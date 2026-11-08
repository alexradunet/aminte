# 0006 — Stable names, one named-object core, and recorded life periods

- **Date:** 2026-10-06
- **Status:** withdrawn (2026-10-28). Nothing of this proposal was adopted as written. Issues 0011, 0012, 0013, 0014,
  0016 and 0017 were resolved by the schema and the contract as they stand (`entities.revision`, `links_fixed`,
  `entities_endpoint_types`, the title predicate, the rename rule); 0015 by its own fix; 0018 by
  [RFC 0017](0017-no-typed-links.md); 0019 and 0020 by [RFC 0014](0014-no-periods.md) and [RFC 0013](0013-no-sessions.md);
  0021 by [D21](../decisions/D21-location-history.md). Each issue records its resolution. The candidate of
  [plan 072](../plans/072-stable-names-candidate.md) was never merged.
- **Answers:** [issues 0011–0021](../issues/README.md), individually linked below.
- **Baseline:** `cfb7fe2`; a design proposal, not a change to [schema.sql](../schema/schema.sql).

## Problem

The owner wants a lifelong record for understanding choices and consequences: existing writing and selected files,
health summaries, and the context of jobs/study and trips. The current model cannot retain identity across a rename,
represent these periods honestly, or associate imported readings with their sleep/exercise sessions. A synthetic
audit also found correctness gaps that should not be preserved by a freeze.

| Issue | Evidence and concern |
|---|---|
| [0011](../issues/0011-renames-lose-identity-and-backlinks.md) | Repeated accepted renames lose backlinks; named objects cannot be renamed |
| [0012](../issues/0012-timestamps-allow-stale-overwrites.md) | Equal timestamp versions allow a stale body overwrite |
| [0013](../issues/0013-promotion-strands-typed-links.md) | Promotion strands an invalid typed edge while integrity reports success |
| [0014](../issues/0014-date-checks-disagree-with-writers.md) | DDL and writer disagree about canonical exact dates/instants |
| [0015](../issues/0015-mood-can-become-a-habit.md) | Registering Mood as a habit breaks normal mood capture |
| [0016](../issues/0016-accepted-handle-cannot-be-linked.md) | A valid title cannot be expressed by its literal wikilink |
| [0017](../issues/0017-link-identity-and-time-are-mutable.md) | Link ID and creation time escape the stated immutability rule |
| [0018](../issues/0018-symmetric-link-note-semantics-are-unclear.md) | Symmetric-link note semantics are ambiguous, not an established equality rule |
| [0019](../issues/0019-life-period-questions-have-no-structured-boundaries.md) | Jobs/study and trips need queryable, sometimes uncertain boundaries |
| [0020](../issues/0020-imported-sessions-need-time-and-measurement-scope.md) | Observed session exports need measurement scope and an honest time basis |
| [0021](../issues/0021-selected-photo-sidecars-have-unhandled-time-evidence.md) | Selected-photo sidecars carry evidence not covered by capture policy |

### Evidence and authority

The owner chose the following direction in the interview:

- Remove the separate `pages` table, not notes/day pages and not the protection against deleting their data.
- Rename objects while retaining their identity and old references, using aliases rather than redirect chains.
- Keep current editable contents plus snapshots, not full edit-history tables. Existing append-only measurement
  corrections are a separate mechanism; their removal was not requested.
- Start historical periods with jobs/study and trips. Keep daily health summaries and individual sleep/exercise
  sessions, not every sensor sample or GPS point. Represent approximate dates without fabricated precision.

The owner approved this draft's preparation, not its exact columns, constraints, implementation or freeze. The
existing database, including its manual writes, was declared disposable test data; that declaration is not an
instruction to delete or rebuild it. No canonical file's contents are assumed from that declaration.

The audit witnesses in issues 0011–0018 used temporary synthetic probes and/or explicitly identified code/contract
inspection. They are not permanent baseline coverage. Export evidence came from an authorized, bounded structural
sample of 20 files across 12 format families: only field names, headers and value types were reported. It did not
establish completeness, coverage dates, source equivalence or correct values. This proposal contains no private
records, source paths, personal filenames, readings, locations or source IDs. It requires no further private-data
inspection to review it.

## Options

| Option | Change and cost | After-freeze implications |
|---|---|---|
| A. Patch the present shape | Fix the demonstrated defects; retain `entities` plus `pages`; add aliases and temporal tables separately. Less existing SQL changes, but keeps a one-to-one split the owner asked to remove. | Several additions could be additive; name resolution and query meanings still require explicit compatibility decisions. |
| B. One named-object core, direct names, separate temporal records | Combine `entities`/`pages`; give names one authoritative registry; add named periods, lightweight sessions and session-scoped measurements. Removes duplicate identity rows and redirect machinery, but requires coordinated writer, reader and contract changes. | Removing `pages` and redirect-based behavior is not additive. Do this only through the pre-freeze process; do not label it a future additive migration. |
| C. A universal record/event/property model | Put notes, periods, sessions and readings into one generic structure, with arbitrary fields or graph-encoded values. Fewer named concepts in the DDL but more implicit types, validation and query conventions. | Broadly incompatible; adds abstraction rather than replacing a demonstrated complexity. |

## Recommendation

**B**, in independently reviewable parts. Simplification means fewer identities, resolution paths and implicit rules,
not the fewest possible tables. Do not make every workout a uniquely titled note or turn the file into a planner.
The following is a proposed logical shape, **not executable DDL or a finalized column list**.

### Identity and names

- `entities` becomes the core for named objects: ID, structural type, body, the existing page-day meaning, lifecycle
  and import metadata, and an edit revision. It absorbs the page fields that are not moved into the name registry.
  `people`, `places`, `metrics` and `files` retain their existing distinct domain fields and reference this core
  directly. A person's full-name field and a disambiguated reference handle remain distinct concepts.
- A proposed `entity_names` table holds every preferred name and alias, including its normalized lookup key and
  owning entity ID. All names share one uniqueness boundary, using the existing pinned Unicode key definition.
  There is no separately writable copy of the preferred spelling in `entities`.
- Each named entity selects exactly one of its own registry entries as preferred. An owner-checked, deferred foreign
  key from the entity to that entry is the candidate mechanism; its construction, commit-time enforcement and
  rename behavior must be demonstrated before acceptance. Do not substitute an unenforced comment for exactly-one
  ownership, and do not claim the cyclic insertion arrangement has been validated by this draft.
- An alias resolves directly to an entity ID, never to another alias. Renaming selects or creates a name owned by
  the same ID; the previous preferred name remains a resolvable alias. Case-only spelling changes do not allocate a
  second normalized key. Names owned by tombstoned entities remain reserved.
- A conflicting name, even one owned by an empty ghost, refuses the rename without moving text, readings or links.
  This deliberately removes the current rename-as-typo-merge behavior. Merging two established identities would
  need a separately reviewed operation; this proposal must not hide it inside name resolution. Reassigning a name
  to another ID would reinterpret old prose and is not an ordinary rename or alias edit.
- Journal day identities remain `YYYY-MM-DD`. Those names are reserved to the corresponding plain day object and
  are not transferable aliases for other objects. The day object's preferred name remains its date; a friendly
  heading can be prose without adding a second stored calendar identity. Day objects remain unpromotable.
- Existing body text is not rewritten. The text after `|` in `[[name|display text]]` remains presentation, not a request
  to register an alias. Resolve names first, then deduplicate by entity ID: several aliases in one body produce one
  wikilink, and an alias to the body-owning entity is still a self-reference. Tombstone revival remains explicit in
  save feedback, including when reached through an old alias.
- Reference names must be compatible with their grammar. The narrow recommendation is to reject brackets in new
  handles rather than invent escaping syntax. Before finalizing the predicate, test other CommonMark-sensitive
  punctuation and Unicode against reference round trips in ordinary prose. Preserve the documented contextual
  Markdown limitations; a bracket-only fix does not prove all names are addressable.

The canonical rename path would no longer allocate replacement objects, create `#REDIRECT` bodies or maintain a
`redirect` link kind. Rebuildable backlinks and lookup use IDs plus the name registry. Derived full-text search must
stay synchronized with body and name changes; exact alias lookup must work even when an old name is not the preferred
one. Search-index shape and trigger maintenance belong to the required prototype, not a new authoritative copy of
names. Import identities, measurement ownership and original-file hashes do not change on rename.

Alias entries preserve name ownership, not a timeline of when each spelling was preferred. This remains compatible
with current contents plus snapshots. The cost is an extra name join and explicit conflict handling in exchange
for removing redirect traversal and the separate page-identity row.

### Edit revision

Introduce a monotonic integer revision on the named object, independent of `updated_at`. Conditional edits compare
this token inside the write transaction. Mutations relevant to a client's editable representation, including names
and typed details, invalidate stale tokens; changing only the clock cannot determine whether an edit is newer.
A true no-op does not consume a revision; rollback restores it along with the data. Counter exhaustion must refuse
rather than wrap. A transaction may perform several mutations; clients depend on a changed token, not on its exact
increment count.

No previous body is stored by this mechanism. Readers and all client adapters must switch their edit-token behavior
together; an old timestamp token must not be accepted as an alternative path that bypasses the protection.

### Named life periods

Add a proposed `periods` extension of a named entity for jobs/study and trips. Its body is the ordinary entity body;
its title is an ordinary renameable name. It carries historical boundaries, not tasks, deadlines, recurrence or a
work-completion workflow. Periods may overlap. Their kind can use the existing named taxonomy and `is-a` relationship;
no separate employer, school, trip-kind or project registry is justified here. Existing general context links may
be used where their meaning fits; do not silently repurpose the day-page `at` semantics.

Propose a small date profile **only for period boundaries**, borrowing EDTF spellings without claiming general
EDTF support. The boundary interpretation below is part of the proposed profile:

- `YYYY`, `YYYY-MM` or `YYYY-MM-DD`, with optional whole-date uncertainty (`?`), approximation (`~`) or both (`%`),
  following the distinctions of [EDTF](https://www.loc.gov/standards/datetime/edtf.html). No EDTF sets, seasons,
  arithmetic expressions or expanded years in this first profile. Precision is in the value, not a redundant column.
- Unknown endpoints and a known-ongoing end are distinct. Candidate encoding: NULL for unknown and `..` for the
  ongoing end. An ongoing value is not replaced by today's date when stored; queries use an explicit as-of date.
  An exact final day is inclusive for a day-based period.
- An unqualified month/year boundary represents an unknown day within that month/year, not an asserted first or
  last day. Query code may derive possible boundary ranges, but must not store those derived dates as observations.
- Approximation alone supplies no numerical tolerance. Do not silently turn `2019~` into a fixed plus/minus window
  or assume which hemisphere's summer was meant. Keep the original wording in the body. Until boundaries are
  sufficiently established, a query must expose uncertainty rather than present a definitive membership count.
- Validate order only to the extent the boundary information establishes it: reject definitely reversed spans,
  distinguish possible from definite membership, and do not reject a possible span merely because a guessed
  first-of-month comparison reverses it. Unknown is not ongoing and is not evidence of presence on every day.

The precise comparison truth table, absent-start policy and representation of ongoing/unknown endpoints are review
and proof gates. The proposed vocabulary is not a claim that SQLite's date functions implement EDTF. Exact
measurement days and UTC write timestamps retain their separate, stricter contracts. This proposal does not widen
people's birth/death dates without a corresponding use case.

### Recorded sessions and measurement scope

Add a proposed `sessions` table separate from named entities. A session has a stable local ID, source/import identity,
a reference to its activity kind in the existing named namespace, source-assigned reporting day, time information
and lifecycle metadata. Repeated workouts need no globally unique title or alias; they are reached by ID and queried
by kind/day/time. This trades direct wikilink naming of every session for a smaller capture model. Sessions are not
made graph endpoints merely to reuse the graph's storage; no attendance or arbitrary event framework is introduced.

Time needs two distinct cases:

- A known instant uses the canonical UTC form, retaining source zone/offset evidence when supplied, not an inferred
  historical zone from the current machine or current user profile.
- A local clock reading with no known offset stays local and explicitly unresolved. Candidate columns are separate
  UTC and local representations per endpoint, with at most one populated for each endpoint, rather than placing a
  non-UTC value in an existing `*_at` convention. An unavailable end remains unavailable; do not fabricate it by
  adding a duration to an offset-free clock across a possible DST transition.

The UTC/local exclusivity, required start information, retained zone/offset fields, mixed-basis comparisons and DST
ambiguity require a concrete profile before DDL approval. Compare elapsed intervals only when their endpoints are
comparable. Source-assigned days remain facts of attribution: an overnight sleep's reporting day is not necessarily
its start day. No zone is guessed to force every session into the same absolute-time timeline.

Extend `measurements` with a nullable session association; keep `captured_with_id` as provenance rather than changing
its meaning to session ownership. Numeric session summaries use this association, not a property bag or a second
measurements table. Unassociated rows retain their current day/point meaning; NULL does not itself mean “daily total.”
Metric definitions must still distinguish total steps, average heart rate, resting heart rate and other different
quantities. A shared unit does not make two metrics interchangeable.

A session's step count is not another whole-day step count. Query/cookbook/client changes must make this distinction
explicit before importing sessions. Retain append-only correction chains; the recommended chain boundary includes
both metric and session association. A wrong association is corrected by retracting the old reading and writing a
properly associated one atomically, not by rewriting history. Session timing/metadata itself follows current contents
plus snapshots, with its own clock-independent edit revision and an explicit tombstone path rather than a full
revision table.

Do not store an elapsed duration redundantly when comparable endpoints already determine it. Provider-reported
active time and time asleep are different quantities. A reported duration that cannot be derived because endpoints
are incomplete may be kept as a scoped reading; disagreement between supposedly equivalent representations needs
review, not an automatic choice. Do not automatically generate another daily total from these session values.
Period membership derived from time is also a query result, not another stored association.

### Import interpretation and selected media

- Prefer provided daily summaries for the selected first scope. Do not combine subdaily rows, session totals and
  daily totals indiscriminately. Averaging averages, filling missing values with zero, and equating similarly named
  provider metrics are not defaults. Units and reporting-day conventions need per-format evidence and synthetic tests.
- Keep source IDs as lossless import keys, independent of names. Re-importing the same source identity must not
  duplicate a session. Changed payloads under an existing identity need an explicit review/correction path; a second
  provider's similar record is a possible overlap, not proof of sameness. Source precedence is an owner-approved
  import policy, not a schema guess or automatic merge.
- Recognize the observed export families, including the `Google Health` product name. Shape detection must report
  unsupported variants rather than claiming successful coverage. This is writer work, not a new database table.
- For selected photos, introduce sidecar-aware preparation with explicit image-to-sidecar association, source
  precedence and conflict reporting. A capture timestamp is not a creation timestamp; an absolute timestamp alone
  may still be insufficient to establish the historical local day. Recommend withholding day/place attribution when
  its evidence is unresolved rather than silently using the import day. Do not silently match an ambiguous basename,
  follow a metadata URL, rewrite originals or retain raw metadata in the canonical file as an escape hatch.
- Selected originals stay outside the database; kept text/previews and supported place/day links retain their purpose.
  Sidecar support does not authorize whole-library ingestion or retention of a movement track. Calendar entries,
  if considered later, are not proof of attendance and are not included in this first session import scope.

The sidecar policy changes the current fallback behavior and therefore needs explicit acceptance and fixture proof.
No new photo-specific storage column is justified by the field-shape inspection alone.

### Correctness fixes alongside the shape change

These fixes must not disappear inside the larger refactor. Each needs its own regression witness and failure-state
assertions; the issue records distinguish demonstrated failures from policy choices.

| Concern | Proposed disposition |
|---|---|
| Conditional saves | The independent revision above; preserve conflict behavior across all edit surfaces |
| Type changes | Refuse any transition that leaves incoming or outgoing endpoints invalid, atomically; add a semantic integrity check that detects such damage independently |
| Exact days/instants | Add explicit canonical shape/range checks alongside round trips; agree the calendar range and run shared writer/DDL boundary vectors |
| Mood as habit | Reject incompatible registration by seeded metric identity, including after rename; no partial period or body write |
| Reference addressability | Agree and test the name/grammar profile above before aliases inherit the mismatch |
| Link immutability | Protect ID and creation time as well as the already protected fields; retain permitted note edits |
| Symmetric notes | Recommend one relationship-wide note for symmetric kinds, updated atomically in both directions with terminating triggers; directional kinds keep their own notes. This is an explicit policy choice, not an existing invariant |

If directional notes are preferred for symmetric relationships, document and test that instead. Do not add a mirror
update trigger just to make the temporary probe green. Likewise, the Mood refusal is not a complete remedy for all
mistaken habit registrations; that correction story remains a separate pre-freeze consideration.

## Scope and contract impact

Nothing in this draft changes current truth. If accepted, implement through the [change process](../process.md):

| Area | Pages and behavior to change together |
|---|---|
| Identity/names | [D5](../decisions/D05-pages-and-day-pages.md), [D8](../decisions/D08-entities-and-links.md), [D19](../decisions/D19-wikilink-save-contract.md), [D20](../decisions/D20-named-pages.md), [D27](../decisions/D27-a-metric-is-a-page.md); title vectors, save/rename/backlink recipes, entity model and lifecycle; dependent typed FKs, FTS and integrity queries |
| Time/facts | [D6](../decisions/D06-mood-is-a-measurement.md), [D7](../decisions/D07-measurements.md), [D10](../decisions/D10-time-model.md), [D22](../decisions/D22-events.md), [D24](../decisions/D24-habits.md); scoped series, day/period views, import identity and correction recipes |
| Selected media | [D9](../decisions/D09-binary-files.md), [D16](../decisions/D16-places.md), [D21](../decisions/D21-location-history.md); file/day attribution and sidecar preparation policy |
| Reliability | [D11](../decisions/D11-tombstones.md), [D12](../decisions/D12-no-revision-tables.md), [D13](../decisions/D13-migrations-and-freeze.md), [D17](../decisions/D17-contract-as-data.md); revision/tombstone coverage, semantic integrity and precise freeze/compatibility wording |

Table-local rules belong in named constraints/triggers and comments inside their `CREATE` statements. Only genuine
cross-table rules earn `aminte_meta` entries and [2075 questions](../contract/threat-model.md). Grammar/time profiles
and shared vectors belong in the contract, not solely in writer code. Update the cookbook, diagrams, overview totals,
non-goals and validation suites together. No implementation-specific rule is to be copied into the language-neutral
contract from one application's behavior.

Do not create migrations or an init migration. The combined-table proposal is a pre-freeze replacement, not a
migration runner or authorization to rebuild an existing file. Before any freeze decision, reconcile the owner's
disposable-test-data declaration with D13 and the docs index's current statement about the canonical file; do not
silently classify disposable manual tests as unreplayable canonical captures or declare that a rebuild occurred.

This proposal does not accept [plan 071](../plans/071-audit-followups.md), close other review gates or satisfy the
[freeze checklist](../process.md#before-the-freeze). A real trial import and replay checks remain separate approvals.

Excluded: tasks/plans, recurrence, money, raw sensor mirrors, GPS tracks, all-photo ingestion, managed originals,
full edit history, generic property bags, new dependencies and analytical “decision/outcome” tables. The earlier
optional diacritic-tokenizer change and unspecified ordering/tie-break policies are not bundled as proven fixes;
they need their own bounded disposition. No lifetime-performance claim follows from the sample or this design.

## Validation

The existing [suites](../../tests/README.md) remain unchanged by this draft. Passing them checks the existing contract
and document consistency, not the proposed design. No new SQLite behavior below is claimed as permanently executed.

### Proof obligations before implementation approval

1. Demonstrate the preferred-name ownership mechanism in disposable SQLite files with production connection settings:
   create/rename/no-op/case-only rename, deferred-constraint failures, missing preferred names, aliases owned by another
   entity, tombstones, conflicting aliases and rollback. Show the chosen FTS synchronization/rebuild and semantic
   integrity queries agree, including corruption/damage controls.
2. Specify the reference-name predicate and extend the single set of contract vectors. Test preferred names and
   aliases through CommonMark extraction, direct lookup, repeat renames, save/re-save, self-links, embeds, alias
   collisions, promotion and resurrection. Old prose must still resolve after repeated renames without extra IDs.
3. Settle the period date profile and query truth table with hand-checked synthetic jobs/study/trips: exact, month,
   year, approximate, uncertain, overlapping, definitely reversed, unknown and ongoing boundaries. Distinguish
   possible from definite matches; show what cannot be answered without clarification.
4. Settle the session time profile and measurement association: offset-bearing and offset-free inputs, DST gaps and
   folds, overnight reporting days, absent endpoints, scoped corrections, daily/session separation and lossless IDs.
   No host-timezone dependence. Replay, changed-source and cross-source cases must assert persisted values and scope,
   not just row counts. Refusal after partial work must leave no orphan session, reading or import-ledger mutation.
5. Define synthetic selected-photo fixtures for correct, missing, conflicting and ambiguously paired sidecars, with
   known capture-day answers and no original-file mutation. Keep private exports out of fixtures and version control.
6. Turn the earlier audit witnesses into deterministic regressions at their owning layers, including stale-version
   refusal under equal clock values and both directions of promotion/link checks. For symmetric notes first accept
   the intended semantics; then test synchronization or deliberate directionality, including recursive-trigger behavior.

### Implementation completion gates, not results of this draft

- Each accepted storage rule gets an owning suite and a mutant with a meaningful failure witness. Table/diagram/mutant
  counts and evolution fixtures include all supported types, including files and any newly accepted types.
- Exercise shared operations through applicable CLI, HTTP/HTML and MCP surfaces, in-process and remote where relevant;
  keep owner-only import approvals/actions unavailable to agents. Do not duplicate business rules in adapters.
- Run generation, vet and the full tests including mutants; inspect generated/schema diffs. Run required analysis and
  concurrency checks for the implementation changes, reporting actual tools, versions, OS and limitations. Validate
  OS-sensitive import paths on Windows and Linux before release; cross-compilation is not execution coverage.
- Before a separately authorized real import: select formats and sources, approve interpretation/overlap policies,
  import into a disposable trial, verify independent answers and integrity, and replay with no new records. No
  automatic freeze, private-data publication, canonical replacement or production import follows from a green suite.

## Outcome

Pending owner review. Direction agreed; the name/grammar constraints, date/time profiles, scoped-query semantics,
symmetric-note policy and sidecar precedence remain explicit acceptance gates. No ADR has been rewritten, no DDL or
writer change made, no database rebuilt or imported, and no freeze declared. An execution plan may follow acceptance;
this draft is not that plan.

# 084 — The owner decides each new name in `entities.md`

> Dated implementation record, not permission to operate on real data. The owner runs every approval and replay.

## Status

- **Date / baseline:** 2026-10-09, `575a706` (master).
- **Priority:** P1. **Effort:** L. **Risk:** MED for the import process (a new gate; aliases leave `rules.md`), none
  for the schema (untouched).
- **Status:** DONE.
- **Implements:** RFC [0008](../rfcs/0008-owner-decides-names-in-imports.md); **resolves** issue
  [0052](../issues/0052-names-are-written-on-the-models-word.md).

## The change

| what | where |
|---|---|
| **`entities.md`**: a stamped table `status, kind, name, as, like, from, doubts`; statuses `proposed`, `approved`, `rejected`; kinds `person`, `place`, `page`. Decisions are read only under a valid stamp, as `metrics.md` is. | `internal/importer/files.go`, `approval.go` |
| **Held writes.** A person or place write whose title no live row of that kind holds (new, tombstoned, or a plain page to promote), and a new page with look-alikes, need a decision: approved → written; rejected → refused (`rejected`); `as` → the title is rewritten to the existing title before the checks; none → refused (`held`, the look-alikes as candidates). *apply facts* writes nothing for a held file and notes its ledger line `held: …`; the line stays to do. | `internal/importer/apply.go`, `facts.go` |
| **propose-entities** (catalog action; the model may call it, it chooses no name): adds a `proposed` row for each held name, for each person and place that the done facts files write, and for each `## Aliases` and `## Distinct` line of `rules.md`, which it removes from `rules.md`. | `internal/importer/entities.go`, `internal/api/import.go` |
| **approve entities** at the terminal: the stamp flips `proposed` to `approved`; `rejected` and `as` rows keep what the owner wrote. | `cmd/aminte/main.go`, `approval.go` |
| **status**: the gate; a held file is the next file only when each of its names has a decision under the stamp; "propose the held names" and "stop: entities.md waits for the owner" when nothing else is to do. | `internal/importer/status.go` |
| **replay**: refuses while `entities.md` is a draft; the evidence snapshot hashes it. | `replay.go`, `prepared_batch.go` |
| **rules.md** keeps only the folders, the source and the decisions in words: `## Aliases` and `## Distinct` are not read. | `workspace.go` |
| **Docs**: the guide (the workspace, `entities.md`, gates, the checks, the procedure, what an implementation must get right, a `name-decision` diagram); D11's revival sentence; README; the RFC accepted; `tests/README.md` and the `diagrams` suite count the diagram. | `docs/`, `README.md`, `tests/` |

## Done criteria

1. Tests: a held file writes nothing and lists its names; propose adds each name once; approved, rejected and `as`
   behave as above; a tombstoned name is held, not revived; a name decision does not close the rules gate; status
   orders held files after the others; replay refuses a draft `entities.md` and applies the stamped decisions.
2. The guide states each rule once; the document and diagrams suites are green; baseline green on Windows.
3. The issue resolved, the RFC accepted, this plan DONE, in the commit of the change.

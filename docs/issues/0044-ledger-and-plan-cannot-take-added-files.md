# 0044 — Files added to a source after the ledger is made are invisible to the import

- **Date:** 2026-10-08
- **Status:** resolved
- **Seen in:** the 2026-10 import: the owner keeps adding exports to one ingest folder, and expects the import to
  tell processed files from unprocessed ones

## What happened

`ledger.md` is written once from the source tree and never rebuilt (*ledger* refuses with 409 when it exists), and
`plan.json` is drafted once and never redrafted (*plan a vault* refuses the same way). A file copied into the source
after that moment is in no ledger line and in no plan note: *status* never names it as the next file, *apply facts*
refuses it as "not a ledger file", and a note added to a notes folder is never planned or copied. The owner's only
way to import it is a second source folder and a second workspace, which splits one source's aliases, questions and
replay in two.

Expected: the ledger stays the one index of a source — every file, its state — and running *ledger* again appends
the files added since, each `[ ]`, without touching an existing line; *plan a vault* run again appends the notes added
since without touching an existing entry (a title or day the model fixed stays fixed). Files the source no longer
holds are reported, never removed: a line is the record that a file was imported.

## Reproduce

1. A source `Notes/` with `a.md`; a workspace with the ledger made (1 file).
2. Copy `b.md` into `Notes/`.
3. `aminte do make-ledger --workspace Notes.aminte`: 409, "the ledger exists; it is never rebuilt".
4. `aminte import status --workspace Notes.aminte`: `b.md` is nowhere; `aminte import apply b.md` is refused.

## Rules involved

- [importing with a model](../guides/importing.md), "The workspace" (`ledger.md`) and "The writer's operations"
  (*ledger*, *plan a vault*)
- [imports](../contract/imports.md), step 3 — a re-run writes nothing twice; the key is the source path

## Resolution

Resolved by [plan 077](../plans/077-ingest-folder-survey-and-passes.md): *ledger* run again appends the files added since and reports the missing ones, changing no line; *plan a vault* run again appends the notes added since and keeps every fixed entry.

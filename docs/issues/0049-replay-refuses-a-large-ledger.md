# 0049 — A real replay refuses a source whose ledger is larger than a prepared artifact may be

- **Date:** 2026-10-08
- **Status:** resolved
- **Seen in:** the 2026-10 import of a Google Takeout extraction (80 412 files, a `ledger.md` of about 6 MB)

## What happened

The replay's rehearsal on a throwaway copy ran clean and ended with the trial's counts. The real run refused with
`422: workspace artifact exceeds byte limit` and wrote nothing. The writer hashes the externally editable workspace
files (`rules.md`, `ledger.md`, the prepared and selected-photo artifacts and their bindings) as evidence before it
publishes a completion marker, and reads each of them with the bound meant for a prepared source artifact (4 × 1 MiB).
A ledger lists every file of the source, one line each; a source of tens of thousands of files is over that bound
as a matter of course, so the real run of any large source is refused while its rehearsal passes — the dry run does
not publish, so it never hashes.

Expected: the evidence hash of the ledger and the rules is computed by streaming the file, with no bound borrowed
from another artifact; the bound stays on the artifacts it was written for.

## Reproduce

1. A workspace with a `ledger.md` of 5 MiB (a source of 100 000 files) and an approved, applied prepared batch.
2. `aminte import replay --to x.db --workspace … --dry-run`: clean. Without `--dry-run`: `422: workspace artifact
   exceeds byte limit`, nothing written.

## Rules involved

- [importing with a model](../guides/importing.md), "Trial, then the real run" and "The workspace" (`ledger.md`)
- [imports](../contract/imports.md), step 1

## Resolution

The evidence snapshot hashes `rules.md` and `ledger.md` as a stream, with no bound; the prepared and selected-photo artifacts and their bindings keep theirs. Regression: `TestSelectionSnapshotHashesALargeLedger` (a 5 MiB ledger is evidence like any other, an oversized prepared artifact is still refused).

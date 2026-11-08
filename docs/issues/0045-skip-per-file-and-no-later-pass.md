# 0045 — A folder is skipped one file at a time, and a file cannot wait for a later pass

- **Date:** 2026-10-08
- **Status:** resolved
- **Seen in:** the 2026-10 import of a Google Takeout extraction (about 81 000 files, 63 000 of them a photo library)
  and of a notes folder with about 100 attachments

## What happened

Two things, both in the ledger's marks.

**Skips are per file.** `rules.md` says what a folder holds in one line (`Drive/** — skip: not a life log`), but
*skip* takes one file: the model would call it once per file, tens of thousands of times for a photo library, and
*status* names each of them as the next file until it does. The rule is written once and applied one file at a time.

**A skip is forever, a to-do is now.** An attachment named by a note, or a photo, is not imported in the notes pass:
it is kept later, as a file with its text (a transcript, the text of a PDF, a scan read by OCR), or as a selected
photo ([files](../guides/importing.md#files-a-recording-a-pdf-a-photo)). The ledger has no state for "later": left
`[ ]`, the file is the next file *status* names and blocks the notes pass; marked `[-]`, it is skipped for good and
the later pass has no list to work from.

Expected: *skip* takes a pattern in the language the rules already use (`Journal/**/*.md`) and marks every file still
to do that it matches; a fourth mark, `[>]` *later*, holds a file for a later pass: not now, not never. *status*
counts it apart and names it only when nothing else is to do.

## Reproduce

1. A source `Notes/` with `a.md` and `img/1.png`, `img/2.png`; the ledger made.
2. `aminte do skip-file file=img/*.png reason=attachment`: refused, `img/*.png is not in the ledger`.
3. Skip `img/1.png` alone; *status* names `img/2.png` as the next file, before `a.md` is done.
4. There is no mark that keeps `img/2.png` out of `next_file` and still in the ledger as to do.

## Rules involved

- [importing with a model](../guides/importing.md), "The workspace" (`ledger.md`, `rules.md`), "The writer's
  operations" (*ledger*, *status*), "Files: a recording, a PDF, a photo"
- [D9](../decisions/D09-binary-files.md) — a file is a page with its text and a small picture

## Resolution

Resolved by [plan 077](../plans/077-ingest-folder-survey-and-passes.md): *skip* takes a file or a glob; *defer* marks `[>]` later, a file held for a later pass that *status* counts apart and names only when nothing else is to do.

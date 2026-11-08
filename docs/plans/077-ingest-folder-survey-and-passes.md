# 077 — An ingest folder: survey without reading, notes folders, added files, a later pass

> Dated implementation record, not permission to operate on real data. The owner runs every approval and every replay.

## Status

- **Date / baseline:** 2026-10-08, `fcb0f90` (master).
- **Priority:** P1. **Effort:** M. **Risk:** LOW for the schema (untouched), MED for the import workflow (ledger
  semantics change).
- **Status:** DONE.
- **Resolves:** issues [0043](../issues/0043-notes-folder-without-obsidian-marker.md),
  [0044](../issues/0044-ledger-and-plan-cannot-take-added-files.md),
  [0045](../issues/0045-skip-per-file-and-no-later-pass.md),
  [0046](../issues/0046-no-inventory-of-an-ingest-folder.md).

## Why and boundaries

The owner's import process has three parties that must not swap roles: the owner, who drops every export into one
*ingest folder* and approves; a party that may see **paths and counts but never a content** (the owner at a terminal,
or a hosted model), who surveys the folder and writes the rules; and a local model, who reads contents one file at a
time and does the import through the writer's operations ([importing with a model](../guides/importing.md)). The four
issues are the places where the writer made the first party guess or do by hand what it should have done itself.
Nothing here is particular to one owner's exports: a notes folder, an archive, a photo library and files added later
happen to every import.

Out of scope: new bounded source profiles (Google Health CSV families), converters of structured exports to Markdown,
OCR and transcription (the later pass only *holds* those files), a ledger across workspaces, and any schema change.
`docs/schema/schema.sql` stays byte-identical.

## The change

Each row is one thing the writer does; the guide is the home of the rule and the code cites it.

| what | where |
|---|---|
| **Inventory.** `aminte import inventory FOLDER` prints, as JSON, a value-free survey of any folder: every folder's path with its files, bytes, extensions and digit-masked name patterns (`IMG_N_N.jpg`), archive listings (zip, tar, tar.gz) the same way without extracting, and the sources recognised: a folder whose files are mostly Markdown, with its daily-note count; a Takeout extraction with its product folders. Bounded (folders, patterns per folder, archive entries); context-aware; an unreadable file is reported without its name. CLI only, like `takeout inventory`, which stays. | `internal/inventory` (new), `cmd/aminte` |
| **A folder of notes.** *status* no longer guesses from `.obsidian`: with the rules approved and no plan, it reports how many Markdown files the source holds among how many, and its `do_now` says to plan them when the rules say they are notes, else to make the ledger. The guide's "An Obsidian vault" becomes "A folder of notes"; Obsidian's link forms are still rewritten when present. The `plan-vault`/`apply-vault` action names are kept. | `internal/importer/status.go`, `vault.go`; the guide |
| **Added files.** *ledger* run again appends the source files not yet listed, each `[ ]`, in path order after the existing lines, and reports `added` and `missing` (listed files the source no longer holds; their lines stay). *plan a vault* run again appends the notes not yet planned, validated like the rest, and leaves every existing entry as it is. | `internal/importer/files.go`, `vault.go`; the guide |
| **Skip by pattern; a later pass.** *skip* takes a file or a pattern in the rules' glob language (`*`, `**`): every `[ ]` or `[>]` file it matches becomes `[-]` with the reason; the count is returned. A new *defer* marks the same way `[>]` ("later: reason"): a file for a later pass. *status* counts `later` apart, never names a `[>]` file while a `[ ]` or a ready `[?]` exists, and when only `[>]` files remain says so and names the first. Every operation that accepts a `[ ]` file accepts a `[>]` one (facts, prepared, selected photo); a done file is never re-marked. | `internal/importer/files.go`, `status.go`, `internal/api/import.go`; the guide |
| **The guide.** "The workspace": the ledger's four marks and the refresh; "The writer's operations": *inventory*, *ledger*, *skip*, *defer*; "The procedure for the model": step 5 skips and defers by pattern; a new short section "An ingest folder" (one folder, one workspace per source beside it, the inventory names them, *ledger* again for files added later); "What an implementation must get right": a refresh adds and never removes, a pattern mark touches only files still to do. | `docs/guides/importing.md`, `README.md` (usage) |

## Done criteria (all met; checks on Windows: gofmt, go generate, go vet, go test -count=1 ./... green; staticcheck and govulncheck not run)

1. `aminte import inventory` on a synthetic folder with a notes folder, a Takeout-like tree, an archive and a camera
   dump prints the expected JSON and no content, no individual file name and no value; a cancelled context returns no
   report; unreadable files are counted, not named.
2. *status* on a source without `.obsidian` that holds notes reports the counts and tells the model to plan when the
   rules say notes; a source with a few stray `.md` files among many others is not planned by default.
3. *ledger* twice: the second run appends only the new files, keeps every mark and note of the first, reports the
   missing ones; replay order is unchanged for the old lines. *plan a vault* twice: a fixed title survives, the new note
   is appended and validated.
4. *skip* and *defer* with `**/*.png` mark every matching `[ ]`/`[>]` file and no other; a literal file not in the
   ledger is refused as before; a `[>]` file is accepted by *check facts* and *apply facts*; *status* orders `[ ]`
   before `[>]` and its `do_now` names the later pass when only `[>]` files remain.
5. The guide and `README.md` say all of the above once; the document suite is green; baseline
   `go generate ./... && go vet ./... && go test ./...` is green on Windows; `gofmt` clean.
6. The four issues are marked resolved and this plan DONE in the commit of the change, both retained as the
   [process](../process.md) requires.

# 0043 — A folder of notes is only recognised when it is an Obsidian vault

- **Date:** 2026-10-08
- **Status:** resolved
- **Seen in:** the 2026-10 import of an exported notes folder: Markdown notes, images and PDFs in folders by year and
  month, with no `.obsidian` folder (the export left it out)

## What happened

With `rules.md` approved and no plan yet, *status* said "Make the ledger" instead of "Plan the vault": the writer
decides that a source is a vault by the presence of an `.obsidian` folder. A notes folder exported without one is
treated as a folder of facts-only files, so its notes would never become pages with their text
([an Obsidian vault](../guides/importing.md#a-folder-of-notes)); the model, told to do what *status* says, would have
written facts for 559 notes and copied no note.

The opposite guess is as wrong: a Google Takeout extraction holds a few dozen `.md` files among tens of thousands
(repositories kept in Drive), and "any `.md` file makes a vault" would plan pages for them.

Expected: the guide describes a folder of Markdown notes and their attachments; an Obsidian vault is one such folder,
not the only one. The writer should say what it found (how many Markdown files among how many) and let the approved
rules decide, instead of guessing from a marker of one application.

## Reproduce

1. A source `Notes/` with `Journal/2031-04-11.md` and `photo.png`, no `.obsidian` folder; a workspace `Notes.aminte`
   with `rules.md` approved.
2. `aminte import status --workspace Notes.aminte`: `do_now` is "Make the ledger (make-ledger), …".
3. Add an empty `Notes/.obsidian/` folder; *status* now says "Plan the vault (plan-vault), …".

## Rules involved

- [importing with a model](../guides/importing.md), "An Obsidian vault" and "The writer's operations" (*status*)
- [D5](../decisions/D05-pages-and-day-pages.md) — a daily note is its day's page

## Resolution

Resolved by [plan 077](../plans/077-ingest-folder-survey-and-passes.md): *status* reports the Markdown files among all files of a source with no plan and no ledger, and the approved rules decide; the guide's "An Obsidian vault" is "A folder of notes".

# 0046 — An ingest folder cannot be surveyed without reading it

- **Date:** 2026-10-08
- **Status:** resolved
- **Seen in:** the 2026-10 import: one folder holding a notes export, a Google Takeout extraction and a camera's
  memory card, surveyed by a hosted model that must not see any content

## What happened

The import's first step is a survey of the source: which folders hold what, how many files of each kind, which are
archives still to extract, where the notes are and how their files are named — the facts behind `rules.md` and the
choice of a workspace per source. Only `aminte import takeout inventory` exists, and it reads three families of one
product (Timeline, Fit, Fitbit): it found nothing in an extraction whose health data is elsewhere, and it says nothing
about a notes folder or a camera dump. The survey was done with ad-hoc shell commands (`find`, `unzip -l`, `sed`),
written and run by hand, and each one risked printing a value.

The survey is the one step that a party *forbidden to read contents* must be able to do — the owner at a terminal,
or a hosted model with no access to the data — so it needs a tool that cannot print a content by construction:
folder paths, counts by extension, file names only as digit-masked patterns (`IMG_N_N.jpg`), archive listings the same
way, and the sources it recognises (a folder of Markdown notes, a Takeout extraction and its product folders).

## Reproduce

1. A folder `Ingest/` with `Notes/Journal/2031-04-11.md`, `Notes/photo.png`, `Camera/DSC0001.JPG` and `old.zip`.
2. `aminte import takeout inventory Ingest`: every folder is "Other"; no families; nothing about notes, names or the
   archive.
3. There is no other survey command.

## Rules involved

- [importing with a model](../guides/importing.md), "The procedure for the model", step 2 (survey) and "The workspace"
- [threat model](../contract/threat-model.md) — what must never leave the machine

## Resolution

Resolved by [plan 077](../plans/077-ingest-folder-survey-and-passes.md): `aminte import inventory FOLDER` surveys any folder with no content read and no single file name printed.

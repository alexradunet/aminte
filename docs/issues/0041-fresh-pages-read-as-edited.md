# 0041 — A fresh page reads as edited: creating it counted as an edit

- **Date:** 2026-10-07
- **Status:** resolved
- **Seen in:** a synthetic probe of the day view against a fresh file built from the schema, SQLite 3.53.4

## What happened

The day view ([the day view](../cookbook/day-view.md) and its copy in the writer) flagged a page "(edited)" when
`updated_at > created_at`. Creating a page is two statements in one transaction ([capture](../cookbook/capture.md)):
the `entities` insert, which wrote both instants, then the insert of the owned preferred name, whose
`entity_names_touch_insert` rewrote `entities.updated_at` with SQLite's clock. The two statements need not share a
millisecond. Of 400 fresh pages that nothing had ever edited, 155 read as edited; when the writer supplied its own
clock for the first statement, 220 of 400.

Creation was also counted as an edit by the token itself. Every fresh page had revision 2 (the name insert), and a
fresh person revision 3 (`people_touch_insert` as well); a place, a metric, a file and a period each took the same
extra step from their typed-row insert trigger. A reader that treated "revision above 1" as "edited" could not
tell the two apart.

## Reproduce

Apply [schema.sql](../schema/schema.sql) to a fresh file. Run 400 times, each in its own `BEGIN IMMEDIATE`: the
`entities` insert and the `entity_names` insert of [capture](../cookbook/capture.md), with `day = '2026-09-29'` and
a distinct title. Read `revision` of the new rows (2 each) and run [the day view](../cookbook/day-view.md) for
`2026-09-29`: roughly 40% of the rows come back as `page (edited)`. Insert a `people` row for a new person and read
its revision (3). The journal suite had only the positive case, an edited page that is flagged, and no case of a
page that is not.

## Rules involved

`aminte_meta.edit_revisions` and the trigger `entity_names_touch_insert` in [schema.sql](../schema/schema.sql),
[D12](../decisions/D12-no-revision-tables.md), [the day view](../cookbook/day-view.md),
[capture](../cookbook/capture.md), [issue 0012](0012-timestamps-allow-stale-overwrites.md) (a clock is not a version).

## Resolution

Creation is not an edit: a row whose creation transaction is its only write keeps revision 1 and the `updated_at`
its insert wrote. `entity_names_touch_insert` now fires only for a name that is not the entity's preferred name
(an alias added later, a rename's new spelling); the typed-row insert triggers `people_touch_insert`,
`places_touch_insert`, `metrics_touch_insert`, `files_touch_insert` and `periods_touch_insert` are dropped (79 triggers
become 74). A promotion still advances the page through `entities_touch` on `entity_type`, and
`habit_periods_touch_insert` stays: a habit period added to an existing metric is an edit of it. The day view
flags a page by `revision > 1`, in the cookbook and in the writer, so the answer no longer depends on two clocks.
The writer inserted a metric and a period with an empty body and wrote the note in a second statement; both now
write the body in the insert, so a new page of every kind ends at revision 1.

One write still counts, correctly, as an edit of a new page: the first capture of a day writes its text after
creating the day page (revision 2; the day view lists the day page apart and never flags it).

The vault import used to create all its pages empty in one transaction and set each body in a later one, so that links
resolve to the notes and not to stubs; every imported page with a body ended at revision 2 and a day view showed it as
edited although nobody had edited it. It now creates every page with its note's text in the first transaction, as one
write, and the later transaction of each note only syncs its links, which advance nothing (a wikilink is derived from
the body). An imported page is at revision 1 with `updated_at` equal to `created_at`; a repeated run writes nothing and
leaves it there; a note changed since is saved as an edit of its page (revision 2); a run that ended between the two
transactions is completed by the next, which adds the links and rewrites no text.

Tests: the `identity` suite creates a page by the cookbook statements 25 times and a page, person, place with a
point, metric, file and period, and expects revision 1 and `updated_at = created_at`; the `journal` suite expects the
day view not to flag a created page, including one whose two clocks differ, and still to flag an edited one;
`TestCreationIsNotAnEdit` runs every writer creation path, and `TestImportedPagesAreNotEdited` applies a vault twice and
replays it into a fresh file, expecting revision 1 and `updated_at = created_at` for every imported page, no page of the
day flagged, then one edit through the writer to make revision 2 and one flag. Each was observed failing before the
change (revision 2 and 3; all 3 never-edited pages of the day flagged; imported pages at revision 2, all 3 listed pages
of the day flagged) and passes after. Mutants restore the unconditional name trigger, the five
typed-row triggers and the clock comparison in the day view.

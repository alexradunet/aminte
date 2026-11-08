# 0042 — An incoming link makes another writer's open edit stale

- **Date:** 2026-10-07
- **Status:** resolved
- **Seen in:** a synthetic review of conditional body saves through the writer, SQLite 3.53.4

## What happened

`links_touch_insert`, `links_touch_delete` and `links_touch_note` advanced the edit revision and `updated_at` of
both endpoints of a link. A body save is refused when the page's revision is not the one the editor read, so a
page was refused for a change that was not to the page: an unrelated capture that wrote `#health` made the day
page link to the `health` page, advanced `health`, and the owner's open edit of it failed with `page N changed
since version "2" (now "3"): read it again`. An import of 4,000 notes that tag one page would have done that to
every concurrent editor of the page, 4,000 times. The same advance moved the `updated_at` the day view shows as a
page's time, so a page appeared to have changed when only something else began to mention it.

## Reproduce

Through the writer on a fresh file: create `health` with a body and read its version; capture `#health` on a day;
save a new body of `health` with the version read first. The save is refused as stale although nothing on the page
changed. At the SQL level, insert a `links` row of kind `wikilink` from page A to page B and read B's `revision`:
it moved.

## Rules involved

`aminte_meta.edit_revisions` and the comment of `links` in [schema.sql](../schema/schema.sql),
[D12](../decisions/D12-no-revision-tables.md), [D19](../decisions/D19-wikilink-save-contract.md) (the body is the
truth of the wikilinks), [D8](../decisions/D08-entities-and-links.md) (symmetric mirrors),
[save a body](../cookbook/save-a-body.md), [issue 0012](0012-timestamps-allow-stale-overwrites.md).

## Resolution

A link is an edit of the page it leaves (`from_id`) and of no page it points at. `links_touch_insert`,
`links_touch_delete` and `links_touch_note` advance only `from_id`; the first two skip `kind = 'wikilink'`, because a
wikilink is derived from the body and the body change already advanced its page (a wikilink's note is still an edit
of the page that leaves it). A symmetric kind's mirror row is its own insert, delete or note update, so a
friendship still advances both pages, which is right: it is on both. An incoming link never advances a page.
`aminte_meta.edit_revisions` and the `links` comment state the rule; this reverses the earlier rule that incident
link changes advance both endpoints, on purpose.

Tests: `TestIncomingLinkDoesNotInvalidateAnOpenEdit` (writer: a capture, a body save and a manual link into a page,
then the open edit of the page succeeds) failed before the change with the stale-version conflict above and passes
after; the `identity` suite checks insertion, note edit and deletion on a directed kind, on a wikilink and on a
symmetric kind; the graph model's expectation of which endpoints advance follows the rule. Mutants restore the
`to_id` advance on each of the three triggers, the wikilink advance on insert and delete, and remove the mirror row's
own advance for insert, note edit and delete.

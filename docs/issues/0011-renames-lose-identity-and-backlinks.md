# 0011 — Renames change identity and repeated renames lose backlinks

- **Date:** 2026-10-06
- **Status:** resolved
- **Seen in:** a synthetic writer audit and the owner's request to rename notes, people, places, metrics and files without losing references.

## What happened

A plain note renamed from `First handle` to `Second handle` and then `Final handle` acquired successive IDs.
A day page mentioning `First handle` appeared in the first replacement's backlinks but not the final replacement's.
The reader follows one redirect hop; two accepted renames produce a longer chain. This was observed with a
temporary synthetic probe against baseline `cfb7fe2`, SQLite 3.53.4 on Windows; it is not a permanent regression test.

Named entities cannot be renamed under the current contract. The owner wants the same object and its old references
to survive a preferred-name change. The owner also selected removal of the separate `pages` table, not removal of
notes/day pages or permission to delete their data. Every currently supported entity has a page, so combining these
one-to-one rows is a candidate simplification rather than a new domain capability.

## Reproduce

On a fresh database built from [schema.sql](../schema/schema.sql), through the writer:

1. Create `First handle` with nonempty prose.
2. Capture a synthetic day containing `Read [[First handle]]`.
3. Rename the note to `Second handle`; verify the day is a backlink.
4. Rename the replacement to `Final handle`; read its backlinks.

Observed: the original day is absent. Expected: repeated accepted renames preserve access to the same material.
For a person, place, metric or file, the rename is refused by design rather than reaching step 3.

## Rules involved

- [D5](../decisions/D05-pages-and-day-pages.md), [D20](../decisions/D20-named-pages.md) and [D27](../decisions/D27-a-metric-is-a-page.md).
- [Renames and name resolution](../contract/titles-and-wikilinks.md), [rename a page](../cookbook/rename-a-page.md) and [backlinks](../cookbook/backlinks.md).

## Resolution

Resolved by the rule "Renames" of [titles and wikilinks](../contract/titles-and-wikilinks.md) and
[rename a page](../cookbook/rename-a-page.md): a rename selects a new preferred spelling on the same identity in one
transaction, previous names stay direct aliases, and every link keeps its identity. No rename makes a redirect, so no
chain can form. The writer runs it as `aminte rename` and the `rename` action. [RFC 0006](../rfcs/0006-stable-names-and-life-periods.md)
is withdrawn.

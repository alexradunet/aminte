# 0023 — Moving a place point bypasses both owners' edit revisions

- **Date:** 2026-10-07
- **Status:** resolved
- **Seen in:** a synthetic fresh-file DDL probe on a hardened SQLite connection

## What happened

Changing a `places` row's id moved its point from one named place to another. Neither entity's revision changed.
Both places stayed structurally and semantically valid because a point is optional. A point that cannot be deleted
could therefore disappear from its original owner through reassignment, bypassing the typed-detail edit token.

## Reproduce

Create two named places, give only the first a point, and read both revisions. Run
`UPDATE places SET rowid = :second WHERE id = :first`. Before the fix it succeeds and preserves both revisions.
The same bypass works with `id`, `_rowid_` and `oid`, including a simultaneous coordinate change.

## Rules involved

The `places` comment and `aminte_meta.edit_revisions` in [schema.sql](../schema/schema.sql),
[place points](../decisions/D21-location-history.md), and [permanent identity](../decisions/D03-integer-ids.md).

## Resolution

`places_fixed` refuses owner changes through every rowid alias. Regressions verify atomic refusal of combined
changes, allowed identity no-ops and ordinary coordinate edits that still advance revision. Its mutant removes the
guard and must fail the ownership witness. The failure was observed before the fix; focused checks and the full baseline now pass (`go generate ./...`, `go vet ./...`,
`TMPDIR=/var/tmp go test -count=1 ./...`, Go 1.27.1 / Linux amd64). This enforces the existing decision and needs no RFC ([process](../process.md)).

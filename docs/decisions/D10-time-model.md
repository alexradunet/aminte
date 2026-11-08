# D10 — Time model: UTC instants + denormalized local days, both TEXT.

**Status:** accepted

- **Decision.** The rules are `aminte_meta.instants` and `aminte_meta.days`, and the round-trip CHECK on every
  `*_at` and `*_day` column, with shared [exact-time boundary vectors](../contract/exact-time.md).
  `entities_provenance_fixed` protects initial recording time alongside import provenance: editing current content
  must not rewrite when the identity first entered the file.
  Explicit ASCII shape and hour bounds are necessary because SQLite's round trip alone accepts negative
  years and hour-24 spellings that canonical civil dates and UTC clocks do not (executed).
  An instant has milliseconds because `%f` always renders `SS.SSS`: one
  fixed-width string that the round-trip CHECK can compare and that sorts recorded instants chronologically as
  plain text (executed). Millisecond precision does not establish causal or commit order: timestamps may coincide
  or the writing clock may move backward. Correction edges establish reading ancestry; revision tokens reject
  stale mutable edits ([D7](D07-measurements.md), [D12](D12-no-revision-tables.md)). The
  CHECK uses `IS`, because a CHECK passes when it evaluates to NULL and `date()` returns NULL for malformed
  input: `date(x) = x` silently accepts `2026-9-3` (executed). A recorded local day is written in the zone of the
  device that captured it — a phone's — when the client names the date. When the client sends the word `today`, or no
  day where `today` is the default, the writer gives the local day of its own clock and zone: the hub's
  ([D3](D03-integer-ids.md)). A client that needs the day of its device sends the date. A day is never
  derived from the instant at query time: "the day I graduated" is a local-date fact, not an instant
  [R7](../research/references.md#r7)[R8](../research/references.md#r8)[R9](../research/references.md#r9). A timed reading's zone is a column because only capture time can supply it: a
  UTC instant alone cannot say whether `22:30Z` was 14:30, 22:30 or 07:30 the next morning. A write time needs
  no zone. The DB checks the
  shape of a zone name, not that it is a real zone; `measurements_tz` explicitly rejects NUL because
  SQLite TEXT `length` and `GLOB` otherwise ignore its suffix (executed). Readers convert with a tz database, which keeps
  renamed zones (`Europe/Kiev` → `Europe/Kyiv`) as links. The standard way to write an instant with
  its zone is RFC 9557 [R69](../research/references.md#r69): `2026-06-09T21:14:03.482Z[Europe/Berlin]` — exactly `*_at` plus `tz`.
  Provenance (`source`, `aminte_meta.source`) is a column for the same reason as the zone: only the moment of writing
  knows it, and with agents among the writers ([D3](D03-integer-ids.md)) it identifies the original inserting writer.
  It does not identify later editors of mutable content ([D12](D12-no-revision-tables.md)).
- **Alternatives.**
  - *Instants only, local day computed at query time*: rejected — a timezone move or DST rule
    silently rewrites history. This is why FxLifeSheet needed a dedicated `tag_days` importer to
    reconstruct local dates [R7](../research/references.md#r7), and why health-mcp denormalizes the local date at write time [R8](../research/references.md#r8).
  - *Unix epoch / Julian day integers*: unreadable in the file and in ad-hoc queries (principle 2).
    SQLite's own docs list TEXT ISO-8601 as the canonical representation [R5](../research/references.md#r5)[R6](../research/references.md#r6).
  - *SQLite `CURRENT_TIMESTAMP` defaults*: rejected — second precision, non-ISO (executed) [R28](../research/references.md#r28).
- **Sources.** [R5](../research/references.md#r5)[R6](../research/references.md#r6)[R7](../research/references.md#r7)[R8](../research/references.md#r8)[R9](../research/references.md#r9)[R28](../research/references.md#r28)[R69](../research/references.md#r69).

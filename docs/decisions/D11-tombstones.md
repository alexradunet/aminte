# D11 — Deletion: tombstones, never hard deletes.

**Status:** accepted

- **Decision.** `aminte_meta.deletes`; the `*_no_delete` triggers, which also hold on a connection that forgot
  `foreign_keys` (executed), and under `recursive_triggers=ON` stop `INSERT OR REPLACE` from deleting a row
  (executed, [connection setup](../contract/connections.md)). Junk captured by accident is tombstoned like
  everything else. An import writes through the writer's operations, so it brings a tombstoned page back only as they
  do: a `[[link]]` in a saved body revives it, and the save names it ([save a body](../cookbook/save-a-body.md)). A
  tombstone is never earlier than its row's creation (`entities_deleted_after_created`): a tombstone stamped by a clock that has
  stepped back behind `created_at` is refused, and the refused write applies
  nothing. The owner sets the host clock right and sends the write again. A restore onto a host with a wrong clock
  meets the same refusal. Tombstoning and un-tombstoning bump `entities.updated_at`
  (`entities_touch`, watching `deleted_at` and `entity_type`, so it cannot re-fire itself under
  `recursive_triggers=ON`, executed).
- **Rationale.** Retaining a tombstoned row preserves its contents and references for recovery and explicit
  historical reads. A currently tombstoned row says when its current deletion was recorded. Revival clears that
  tombstone, and deleting it again replaces the deletion time; no sequence of deletion and revival events is
  retained ([D12](D12-no-revision-tables.md)). Hard deletes also break the `links` graph. Storage cost is irrelevant at this scale.
- **Alternatives.** Hard delete + `ON DELETE CASCADE` (destroys evidence, cascades surprises);
  trash-with-expiry (a policy layer that can be added on top of tombstones later).
- **Facts.** `measurements` are not even tombstoned: they are corrected by inserting rows, as medical
  records are. The triggers guard against mistakes, not against a writer that drops
  them, so "tamper-evident" would overstate it.
- **Known asymmetry.** `links` rows are hard-deleted — no tombstone, no audit. Wikilink removal is
  *required* (the body is the truth, [D19](D19-wikilink-save-contract.md)). The authored link `part-of` goes the same way,
  weighed against a split policy and **rejected**: an authored link says what is true now (a category a page is filed in), as a body does. Its removal is an edit, and an edit keeps no history
  ([D12](D12-no-revision-tables.md)): the removal is invisible, as the old text of a body is. A
  [snapshot](D25-snapshots.md) keeps the links it contains. The split
  policy stays additive later (a `deleted_at` column + partial unique index + a flag in `link_kinds`),
  and `sqlite-history` triggers [R48](../research/references.md#r48) are the documented retrofit.

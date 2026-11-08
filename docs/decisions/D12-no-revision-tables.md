# D12 — Audit trail: no revision tables.

**Status:** accepted

- **Decision.** No revision or history tables. The temporal metadata is row-level: `created_at`
  (on every table that has it), `updated_at`, the tombstone, `source`,
  and the append-only facts. `aminte_meta.edit_revisions` supplies clock-independent stale-edit
  protection without retaining any previous body or typed value. The token moves only with the page's own
  content: creating a page, with its owned name and typed row, is not an edit (`entity_names_touch_insert`), so a
  fresh page is never shown as edited, and a link is an edit of the page it leaves, never of the page it points
  at (`links_touch_insert`), so a capture that tags `#health` does not make another writer's open edit of the
  `health` page stale. A commit must survive power loss, so connections use
  `synchronous = FULL` ([connection setup](../contract/connections.md)) [R54](../research/references.md#r54).
- **Alternatives.** *Full revision snapshots per edit*: rejected — significant code for a history
  nobody has asked to query. *Trigger-based history tables* (`sqlite-history` [R48](../research/references.md#r48)): rejected **for
  now**; it retrofits onto the current schema with no redesign if a real need appears.
- **Cost accepted.** Mutable bodies, display spellings, typed details and file previews
  retain their current contents, not their edit histories. `source`
  identifies the original inserter, not the latest editor; timestamps and revisions do not reconstruct an edit.
  Retained aliases are name ownership, not a dated rename history. Revival removes the prior tombstone.
  The database retains an earlier mutable value only in a [snapshot](D25-snapshots.md) that actually contains it;
  edits between snapshots are absent. Measurement correction chains retain their own history ([D7](D07-measurements.md)),
  but a historical reading displayed with today's metric name does not reconstruct its name at recording time.
- **Sources.** [R48](../research/references.md#r48)[R54](../research/references.md#r54).

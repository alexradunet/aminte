# D28 — A capture key: a capture sent again changes nothing.

**Status:** accepted

- **Context.** A client sends a capture, the writer commits, and the reply is lost. The client sends the capture
  again, and the page holds the paragraph twice with two Mood readings
  ([issue 0085](../issues/0085-a-resent-capture-appends-twice.md), [proposal 0021](../rfcs/0021-a-capture-key.md)).
  A capture has no version to compare, and two equal captures can be two true facts, so the content cannot tell a
  retry from a repeat.
- **Decision.** The table `capture_keys(id, source, import_key, page_id, recorded_at)` holds the key of each capture
  that a sender gave a key. `UNIQUE (source, import_key)` makes the key unique per source, as the `import_key` of
  `entities` and `measurements` is. The `id` is the integer key of every fact table ([D3](D03-integer-ids.md)). `capture_keys_source` and `capture_keys_import_key` have the
  shape of `entities_source` and `entities_import_key`: 1 to 512 bytes without NUL, so the empty key is refused
  and cannot make every later capture look added before. `capture_keys_recorded_at` is the instant of
  `aminte_meta.instants`. `page_id` is the day page that the capture was added to.
  A writer reads the key first. A row back means the capture was added before: it changes nothing and answers with
  the day of `page_id`. Without a row, it adds the paragraph, records the Mood reading with the same key
  (`measurements.import_key`) and inserts the key, all in one `BEGIN IMMEDIATE` transaction
  ([capture](../cookbook/capture.md)). A capture with no key is not recorded in this table, and a second one with
  no key appends again, as before. The sender makes the key once for one capture and sends it unchanged on every
  retry ([import a row once](../cookbook/import-a-row-once.md)). A browser form carries a new key each time the
  page draws it.
- **Append-only.** `capture_keys_no_update` and `capture_keys_no_delete` refuse a change and a removal, as the
  triggers on `measurements` do. A key that changes or goes away lets its capture be added again. This follows
  [D11](D11-tombstones.md): `aminte_meta.deletes` says that life data is never deleted, and a key is the proof that
  a capture happened. A key needs no tombstone, because it is never wrong: it says that a capture was added.
  A page tombstoned later does not undo its capture, so a resend still changes nothing.
- **Shape of the table.** It is a `STRICT` table with an `INTEGER PRIMARY KEY`, as every sibling is. The unique pair adds one
  automatic index. `WITHOUT ROWID` with the pair as the key would save it, but no other table of the file uses it,
  and a file of this size does not need it. There is no index on `page_id`: an entity is never deleted, so no delete looks for the rows
  that point at it.
- **The reading.** The Mood reading of a capture carries the key of the capture. The source of that reading is the
  writer of the capture, so it is in the namespace of the same writer, and the unique index of `measurements` is
  per source, key and metric. A key that a writer gave to a reading of Mood with the `record` action and then gives
  to a capture with a mood meets that index. The capture is then refused (409) and nothing is written. A sender
  uses one key for one intent.
- **Alternatives.**
  - *A key for each paragraph and reading, given by the client* ([proposal 0021](../rfcs/0021-a-capture-key.md),
    option B): rejected. A paragraph is not a row, and a table of paragraphs is a revision table
    ([D12](D12-no-revision-tables.md)).
  - *No key, compare the content*: rejected. It loses a true fact without a sign.
  - *A column on `entities` for the last capture key*: rejected. It holds one key, so a second retry after a
    later capture appends again; and `entities` would carry a rule of one kind of page.
- **Trade accepted.** One row of about 100 bytes for each keyed capture, for good. A sender that gives no key gets
  no protection, as before. After the freeze, the table and its triggers are additive
  ([D13](D13-migrations-and-freeze.md)). Before the freeze, a file in use gets the table by a one-off program that
  accepts only the new table.
- **Sources.** The `journal` and `schema-safeguards` suites (executed); [imports](../contract/imports.md).

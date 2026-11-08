# D17 — The contract as data: `aminte_meta`, comments inside the statements, in-DB guards.

**Status:** accepted

- **Decision.** The rules of a table are comments inside its `CREATE` statement: a comment *outside* a
  statement is not stored in the file (executed), and `.schema` prints the statements. The few rules
  that span tables are rows of `aminte_meta(key, value)`, queryable with `SELECT *`. Alongside, the
  contract is enforced where CHECK constraints reach: date and instant round-trips ([D10](D10-time-model.md)), GLOBs only
  as character-class guards, and the composite FKs that make a row's type and its table agree. A CHECK
  uses only functions every SQLite the contract allows has (`aminte_meta.sqlite`) — a SQLite that
  lacks one cannot write the table or integrity-check it — so no `octet_length()` where
  `length(CAST(x AS BLOB))` does the same.
- **The 2075 test** ([threat model and the 2075 test](../contract/threat-model.md)) checks that `.schema` and
  `aminte_meta` retain the reading summaries and every metadata key answers a question. It executes presence
  checks, not a proof of complete interpretation. A new rule that spans tables needs a key and a row in that
  table; a table's own rule needs a comment in its statement. Exact writer behavior also depends on the matching
  [contract and vectors](../guides/building-a-writer.md), including pinned Unicode and Markdown behavior and
  calendar/time-zone interpretation. Those external algorithms are not copied into the database.
- **Threat model.** The database is deliberately not encrypted ([threat model](../contract/threat-model.md)): health data never enters git; the
  disk is encrypted at rest; Datasette listens on localhost only and opens the file read-only; no
  credentials or full account numbers, ever.
- **Alternatives.** Comments only in a separate document (lives outside the artifact, rots); all
  rules as `aminte_meta` rows (a second copy of what the statements already say).

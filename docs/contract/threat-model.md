# Threat model and the 2075 test

**What is protected, and from what.** The asset is `life.db`: prose, health data and the pictures of kept files, in one plaintext file ([D17](../decisions/D17-contract-as-data.md) — the database is deliberately not encrypted).

| Threat | Control | Residual |
|---|---|---|
| The file is damaged or lost | `synchronous=FULL` and WAL on SQLite ≥ 3.51.3, on a local disk ([connection setup](connections.md)); the integrity checks find damage ([integrity checks](integrity-checks.md)); a dated snapshot, trusted once its restore check passes, is the second copy ([take a snapshot](../cookbook/take-a-snapshot.md), [D25](../decisions/D25-snapshots.md)) | what was written after the last snapshot; a snapshot on the same disk is lost with it, and a copy off the machine is the owner's business ([non-goals](../architecture/non-goals.md)); power loss is documented, not simulated [R67](../research/references.md#r67) |
| A changed value inside the file (bit rot) | a data-checksumming filesystem (btrfs, ZFS), never `chattr +C`, a periodic `scrub` ([integrity checks](integrity-checks.md)) | structural integrity alone cannot detect an arbitrary changed value; FTS content comparison detects changes to indexed tokens, but other undetectable value changes remain |
| A buggy writer, importer or agent | one writing application; triggers for append-only facts, no hard deletes and fixed kinds and retained name ownership; `ON CONFLICT … DO NOTHING`; `BEGIN IMMEDIATE`; the `source` of a row (`aminte_meta.source`); the foreign-key and orphan checks ([D11](../decisions/D11-tombstones.md), [connection setup](connections.md), [integrity checks](integrity-checks.md)) | the pragmas are per connection, so the application asserts them at connect |
| Instructions hidden in an imported source, read by an agent during an import ([imports](imports.md)) | the agent writes through the writer's operations only, so every write meets their checks; direct metric creation and snapshots are owner-only operations, refused to an agent; the agent works in a conversation with the owner, who says yes or no to each person and link; the agent is instructed to treat source text as data, not instructions; a snapshot before the import | instructions are not enforcement; a wrong but valid row can pass, and an agent that copies a note can change its text — the owner's review in the conversation, and the snapshot to restore, are what hold |
| Another tool editing rows | exploration tools open the file read-only ([connection setup](connections.md), [D14](../decisions/D14-ui-and-tools.md)) | anything with write access to the file bypasses every control |
| A stolen disk | the disk holding `life.db` is encrypted at rest ([D17](../decisions/D17-contract-as-data.md)) | a stolen *unlocked* machine has everything |
| Health data or private notes leaking through git | `life.db` and its `-wal`/`-shm` are never committed; no credentials or account numbers, ever ([non-goals](../architecture/non-goals.md)) | page bodies and `note` fields are free text — the owner's discipline |
| The data exposed on a network | Datasette on localhost only and read-only; nothing that runs arbitrary SQL is reachable from outside ([D17](../decisions/D17-contract-as-data.md)) | a wrong bind address |
| A reader in fifty years without these docs | the in-file reading summary checked by the 2075 test, below | exact writer conformance still needs the matching contract and vectors; Unicode, Markdown and time-zone algorithms are not reproduced in the database |

Out of scope: a hostile local user, malware running as the owner, and legal compulsion — those
need the database itself encrypted ([non-goals](../architecture/non-goals.md)).

**The 2075 test.** A stranger holds `life.db` without its application. `.schema` and
`SELECT * FROM aminte_meta` explain the stored records and the interpretation limits below. They are a
reading summary, not a complete replacement-writer specification: a [conforming writer](../guides/building-a-writer.md)
also needs the matching contract, its pinned algorithms and its vectors.

The table is executed as a presence check: each place named in the third column — a `aminte_meta` key, or
a table, view or trigger whose `CREATE` statement `.schema` prints — must exist in a fresh database and its
text must contain each phrase in the last column. **Every key of `aminte_meta` must be used by some question**.
This catches missing summaries; it does not prove that prose is unambiguous or that an independent reader
can reconstruct the full Unicode or Markdown algorithms. Their
behavioral vectors remain in the [storage contract](README.md).

| # | Question | Where the answer is | The answer says |
|---|---|---|---|
| 1 | What is this, and where are its rules? | `schema` | `aminte`, `CREATE statement`, `conforming writer`, `vectors` |
| 2 | How is an instant stored, and does it establish commit order? | `instants` | `UTC`, `ISO-8601`, `commit order` |
| 3 | What is a `*_day` column? | `days` | `LOCAL`, `never recomputed`, `IS, not =` |
| 4 | In which time zone was a reading taken? | `measurements` | `IANA` |
| 5 | When was a row written, versus when did it happen? | `instants`, `measurements` | `never back-dated`, `created_at` |
| 6 | Can anything be deleted, and does revival retain prior tombstones? | `deletes`, `entities_no_delete` | `tombstone`, `links`, `registries`, `revival` |
| 7 | Are measurements kept? How is one corrected? | `measurements`, `measurement_values` | `append-only`, `RETRACTS` |
| 8 | Which link kinds exist, and who may link what? | `link_kinds` | `CLOSED registry` |
| 9 | How do body references become links, and do display labels register names? | `entities`, `entity_names` | `CommonMark`, `invalid names do not block`, `registers no alias` |
| 10 | How does rename preserve identity, and what makes a name valid? | `entity_names`, `entity_names_fixed` | `owners never change`, `Filename`, `NFC`, `Old prose is not rewritten` |
| 11 | Why do ids of different tables coincide? How are rows created? | `entities` | `SAME id`, `RETURNING` |
| 12 | How does `[[Bob Sample]]` reach a person? | `entities`, `people` | `is also a page`, `handle` |
| 13 | Who may write, and with which settings? | `writers` | `BEGIN IMMEDIATE`, `read-only` |
| 14 | What is derived and can be rebuilt? | `entities_fts`, `entity_search_content` | `Rebuild`, `Derived`, `one document per entity` |
| 15 | How do imports avoid duplicates and bad rows? | `writers`, `measurements` | `DO NOTHING`, `OR IGNORE` |
| 16 | How does the schema change, and must readers check compatibility? | `evolution` | `additive`, `user_version`, `data-preserving`, `explicit columns` |
| 17 | Where is the journal? What did I write on a given day? | `entities`, `days` | `plain journal-day identity`, `YYYY-MM-DD`, `neither may transfer` |
| 18 | Which SQLite may write this file? | `sqlite` | `3.51.3`, `3.53` |
| 19 | Who inserted this row, and is that necessarily its last editor? | `source` | `written at insert`, `agent`, `last editor` |
| 20 | Which metrics are biomarkers, or what the owner took in? | `metrics` | `a category is a page`, `part-of`, `NOT a category` |
| 21 | Where are the photos, the recordings and the scans? | `files` | `never stored in life.db`, `sha256`, `JPEG` |
| 22 | Can a type change reinterpret retained edges? | `typed_links`, `entities_endpoint_types` | `incoming`, `outgoing`, `retained` |
| 23 | How are stale edits rejected when write clocks coincide, and what counts as an edit? | `edit_revisions` | `monotonic`, `independent`, `no-ops`, `rollback`, `exhaustion`, `creation is not an edit`, `never the page it points at` |
| 24 | What is a page named `2026-W43`, `2026-10`, `2026-Q4` or `2026`? | `periods`, `entities` | `canonical key`, `day is NULL`, `ISO 8601`, `entities_period_exists` |
| 25 | Can a period page be a day, be filed, be renamed or have an alias? | `entities`, `entities_day_identity`, `links_endpoint_types` | `entities_period_page`, `period identity cannot be renamed`, `never a period page`, `never filed in a category` |


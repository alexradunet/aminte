# Schema

[`schema.sql`](schema.sql) is the canonical init DDL, always the full current one: until the freeze it is
edited **in place**, and after it each migration edits it too ([D13](../decisions/D13-migrations-and-freeze.md)). A new database, a
test database included, is that file applied verbatim to a fresh file:

```
sqlite3 life.db < docs/schema/schema.sql
```

`PRAGMA application_id` marks the result as an Aminte database. Each table's rules are comments inside its
`CREATE` statement, so `.schema` prints them; the rules that span tables are the rows of `aminte_meta`.

**10 tables + 1 FTS5 virtual table + 3 views** (`measurement_values`, `ghost_pages`, `entity_search_content`)
**+ 42 triggers.** That is the entire system. Every `CHECK` is named (`CONSTRAINT <table>_<rule>`), so
any rule can be dropped or re-added by name after the freeze ([D13](../decisions/D13-migrations-and-freeze.md)).

## The objects

What each one is for, and where it is explained. Its rules are in its `CREATE` statement, not here.

| object | holds | why | used in |
|---|---|---|---|
| `entities` | one named identity and prose body per linkable thing: id, type, day, provenance, preferred-name selection and tombstone; a day page and a period page have canonical keys | [D8](../decisions/D08-entities-and-links.md), [D11](../decisions/D11-tombstones.md), [D30](../decisions/D30-period-pages.md) | [capture](../cookbook/capture.md) |
| `entity_names` | authoritative preferred spellings and retained direct aliases, globally unique normalized keys | [D5](../decisions/D05-pages-and-day-pages.md), [D20](../decisions/D20-named-pages.md) | [titles and wikilinks](../contract/titles-and-wikilinks.md), [capture](../cookbook/capture.md) |
| `people` | what a person has beyond its page | [D20](../decisions/D20-named-pages.md) | [a person](../cookbook/person.md) |
| `metrics` | the registry of what is measured, each metric filed in a category page; which metrics the writer asks for on every day | [D7](../decisions/D07-measurements.md), [D26](../decisions/D26-metric-categories.md), [D31](../decisions/D31-daily-metrics.md) | [a metric series](../cookbook/metric-series.md), [metrics by category](../cookbook/metrics-by-category.md) |
| `files` | what a file the owner keeps has beyond its page: the hash and type of the original, a small picture | [D9](../decisions/D09-binary-files.md) | [keep a file](../cookbook/keep-a-file.md) |
| `measurements` | readings, append-only | [D6](../decisions/D06-mood-is-a-measurement.md), [D7](../decisions/D07-measurements.md) | [correct a measurement](../cookbook/correct-a-measurement.md) |
| `capture_keys` | the keys of the captures the file has added, so that a capture sent again changes nothing; append-only | [D28](../decisions/D28-capture-keys.md) | [capture](../cookbook/capture.md), [import a row once](../cookbook/import-a-row-once.md) |
| `link_kinds` | the closed registry of link kinds and their endpoint types | [D8](../decisions/D08-entities-and-links.md) | [who may link what](../architecture/link-rules.md) |
| `links` | the one graph between entities | [D8](../decisions/D08-entities-and-links.md), [D19](../decisions/D19-wikilink-save-contract.md) | [backlinks](../cookbook/backlinks.md), [save a body](../cookbook/save-a-body.md) |
| `aminte_meta` | the rules that span tables, as data | [D17](../decisions/D17-contract-as-data.md) | [threat model and the 2075 test](../contract/threat-model.md) |
| `entities_fts` | one derived search document per named identity | [D5](../decisions/D05-pages-and-day-pages.md) | [full-text search](../cookbook/full-text-search.md) |
| `entity_search_content` | derived preferred spelling, combined names and body for the search document | [D5](../decisions/D05-pages-and-day-pages.md) | [full-text search](../cookbook/full-text-search.md) |
| `measurement_values` | the read rule of measurements: corrections applied | [D7](../decisions/D07-measurements.md) | [a metric series](../cookbook/metric-series.md) |
| `ghost_pages` | empty pages nothing points at | [D5](../decisions/D05-pages-and-day-pages.md) | [ghost pages](../cookbook/ghost-pages.md) |

The diagrams are in the [entity model](../architecture/entity-model.md).

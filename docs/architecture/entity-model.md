# Entity model

The diagrams draw tables, keys and relationships only; the other columns are in [schema](../schema/README.md). They are part of
the contract: `tests/` checks every table, key column and foreign key they draw against [schema](../schema/README.md), so a
diagram cannot drift from the DDL without a test failing. (`entities_fts`, the FTS5 index over `entity_search_content`,
is derived and rebuildable and is not drawn.)

## Entities and the graph

One named identity and prose body per linkable thing (`entities`), with authoritative spellings in
`entity_names` and a preferred selection checked by a deferred owner FK. A person, metric or file
is a page concept, not a separate prose row. Typed extensions reference `(id, entity_type)`
directly on `entities`. The polymorphic `links` graph references
the same stable id and the closed `link_kinds` registry.

```mermaid
%% diagram: er-core
erDiagram
    entities ||--o{ entity_names : "entity_id"
    entity_names ||--o| entities : "id"
    entities ||--o| people : "id"
    entities ||--o| files : "id"
    entities ||--o{ links    : "from_id"
    entities ||--o{ links    : "to_id"
    link_kinds ||--o{ links  : "kind"

    entities {
        INTEGER id PK, FK
        TEXT preferred_name_key FK
    }
    entity_names {
        INTEGER id PK
        INTEGER entity_id FK
    }
    people {
        INTEGER id PK, FK
        TEXT entity_type FK
    }
    files {
        INTEGER id PK, FK
        TEXT entity_type FK
    }
    links {
        INTEGER id PK
        INTEGER from_id FK
        INTEGER to_id FK
        TEXT kind FK
    }
    link_kinds {
        TEXT kind PK
    }
```

## Facts and registries

A reading is a fact, not an entity: `measurements` is append-only ([D7](../decisions/D07-measurements.md)). A correction is a new row, and
`measurements.supersedes_id` points back at the row it corrects; `measurements.captured_with_id` records
provenance (a mood reading points at its day page). A metric is a page with a `metrics` row, as a person is a page with a `people` row ([D27](../decisions/D27-a-metric-is-a-page.md));
it is filed in a category, which is a page, by a `part-of` link ([D26](../decisions/D26-metric-categories.md)). `capture_keys` holds the key of each capture the file has added, so that a capture sent again changes nothing; it points at
the day page of the capture ([D28](../decisions/D28-capture-keys.md)). `aminte_meta` stands alone: the rules that span
tables ([D17](../decisions/D17-contract-as-data.md)).

```mermaid
%% diagram: er-facts
erDiagram
    entities    ||--o| metrics : "id"
    metrics     ||--o{ measurements : "metric_id"
    entities    |o--o{ measurements : "captured_with_id"
    measurements |o--o| measurements : "supersedes_id"
    entities    ||--o{ capture_keys : "page_id"

    metrics {
        INTEGER id PK, FK
        TEXT entity_type FK
    }
    measurements {
        INTEGER id PK
        INTEGER metric_id FK
        INTEGER captured_with_id FK
        INTEGER supersedes_id FK
    }
    entities {
        INTEGER id PK, FK
        TEXT preferred_name_key FK
    }
    capture_keys {
        INTEGER id PK
        INTEGER page_id FK
    }
    aminte_meta {
        TEXT key PK
    }
```

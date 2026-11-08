# Decision log

Each decision: **context → decision → alternatives rejected → rationale → sources.** A decision cites
the constraints that carry it; the rule itself is in [schema](../schema/README.md) or [storage contract](../contract/README.md). New decisions are written from the [template](template.md) and get the next number; a number is never
reused, and a decision that changes is rewritten in place (git is its history).

| id | decision | status |
|---|---|---|
| [D1](D01-single-sqlite-file.md) | Container: a single SQLite file. | accepted |
| [D2](D02-typed-strict-tables.md) | Typed tables with real columns, `STRICT` mode; no JSON property bags, no EAV. | accepted |
| [D3](D03-integer-ids.md) | IDs: `INTEGER PRIMARY KEY`; UUIDs rejected. | accepted |
| [D4](D04-database-is-canonical.md) | Text ownership: the database is canonical. | accepted |
| [D5](D05-pages-and-day-pages.md) | One prose body per identity; the journal is a page per day; names are owned aliases. | accepted |
| [D6](D06-mood-is-a-measurement.md) | Mood: the `mood` metric in `measurements`, not a prose column. | accepted |
| [D7](D07-measurements.md) | Measurements: one FxLifeSheet-shaped table + tiny metric registry; append-only. | accepted |
| [D8](D08-entities-and-links.md) | One `entities` supertype + one polymorphic `links` graph; a closed kind registry. | accepted |
| [D9](D09-binary-files.md) | Files: a file the owner keeps is a page; its text is the body, a small JPEG its picture, and the original stays outside. | accepted |
| [D10](D10-time-model.md) | Time model: UTC instants + denormalized local days, both TEXT. | accepted |
| [D11](D11-tombstones.md) | Deletion: tombstones, never hard deletes. | accepted |
| [D12](D12-no-revision-tables.md) | Audit trail: no revision tables. | accepted |
| [D13](D13-migrations-and-freeze.md) | Migrations: numbered plain SQL + `PRAGMA user_version`; freeze-and-migrate. | accepted |
| [D14](D14-ui-and-tools.md) | UI: thin custom app for capture/browse; off-the-shelf tools for exploration. | accepted |
| [D15](D15-recurrence.md) | No recurrence table: a repeat lives in the owner's task manager. | accepted |
| [D16](D16-places.md) | No places: a place is a plain page, and where a day was is the words of the day page. | accepted |
| [D17](D17-contract-as-data.md) | The contract as data: `aminte_meta`, comments inside the statements, in-DB guards. | accepted |
| [D18](D18-money.md) | Money: DEFERRED out of v1. The design is kept here for the day it returns. | deferred |
| [D19](D19-wikilink-save-contract.md) | The wikilink save contract: one transaction, links follow the body, a bad target never blocks a save. | accepted |
| [D20](D20-named-pages.md) | A person is a page: one id, and `[[Name]]` reaches it directly. | accepted |
| [D21](D21-location-history.md) | No location: a place has no point, a photo's position is never read, and there is no track. | accepted |
| [D22](D22-events.md) | No recorded spans, sessions or events: the day page and plain pages hold them | accepted |
| [D23](D23-no-tasks.md) | No tasks table: a plan lives outside the file. | accepted |
| [D24](D24-habits.md) | Habits: a unitless metric with a reading of 1 or 0 a day; no periods. | accepted |
| [D25](D25-snapshots.md) | Snapshots: a dated `VACUUM INTO` copy, trusted once its restore check passes. | accepted |
| [D26](D26-metric-categories.md) | Categories: a category is a plain page, and a thing is filed in it by a `part-of` link. | accepted |
| [D27](D27-a-metric-is-a-page.md) | A metric is a page: its title is its name, its body its note. | accepted |
| [D28](D28-capture-keys.md) | A capture key: a capture sent again changes nothing. | accepted |
| [D29](D29-plan-lines.md) | No plan lines: a plan lives outside the file, in the owner's task manager; what was done comes back as an entry. | accepted |
| [D30](D30-period-pages.md) | A period is a page with a canonical key: a week, a month, a quarter, a year. | accepted |
| [D31](D31-daily-metrics.md) | A daily metric: the writer asks for a reading of it on every day. | accepted |

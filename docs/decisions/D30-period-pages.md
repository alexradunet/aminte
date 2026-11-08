# D30 — A period is a page with a canonical key: a week, a month, a quarter, a year.

**Status:** accepted

- **Context.** A week, a month, a quarter and a year each need one home for a review and a note: the future log, the
  monthly log, the quarterly review, the annual review. The bullet journal keeps a future log and a monthly log;
  multi-scale planning works at more than one horizon. A day page already has such a home ([D5](D05-pages-and-day-pages.md)). A longer span had none.
- **Decision.** A period page is a plain page with the key as its title. The key is a week (`2026-W43`: ISO 8601, Monday first, week 1 holds
  4 January), a month (`2026-10`), a quarter (`2026-Q4`) or a year (`2026`). The generated column `entities.period` is
  the one definition: it reads the folded `preferred_name_key` (`2026-w43`) and says `week`, `month`, `quarter`, `year`
  or NULL. A key of ten characters never has these shapes, so a page is never both a day page and a period page.
  - `entities_period_page`: a period page is a plain page and its `day` is NULL. A period's first day derives from its
    key, so a stored `day` would be a second home.
  - `entities_period_exists`: the month is 01 to 12, the quarter 1 to 4, the week 01 to 52, and week 53 only in a year that
    has one. The file refuses `2027-W53`, `2021-W53`, `2026-W00` and `2026-W54`, and accepts `2026-W53` and `2020-W53`.
  - The rules of a day page hold for a period page. `entity_names_day_insert` gives it no alias, reserves its key to its own
    page and fixes its spelling to the upper-case key (`2026-W43`, `2026-Q4`; for a month and a year the key itself).
    `entity_names_day_update` keeps that spelling. `entities_day_identity` never renames it, never lets a page acquire
    the key through aliases, and never lets a page with a `part-of` link acquire it. `links_endpoint_types` refuses a
    `part-of` link from or to a period page.
  - `aminte_meta.periods` and the comment in the `entities` statement say the same in the file.
  - A plain page with a period-shaped title in a file built before this rule is adopted at the rebuild, since there is no
    migration before the freeze ([D13](D13-migrations-and-freeze.md)). In such a file the page keeps its alias and its links, since a period page is never
    renamed (`entities_day_identity`) and an alias is never deleted (`entity_names_no_delete`). The owner tombstones it and writes
    the period page anew, with a new title for the old content if it must stay. At a rebuild from imports the alias insert is
    refused, and the importer skips it.
- **Alternatives.**
  - A `periods` table: rejected. It is a second home for a page, with its own id, name and body.
  - A plain page the owner titles `2026 Q4`: rejected. The writer cannot find it by its key, and the calendar cannot draw it.
  - A `day` set to the first day of the period: rejected. It is derivable from the key, and it would list a week page under its Monday.
- **Trade accepted.** A title of four digits is always a year page. Only ISO weeks exist; a
  week of another calendar is a plain page with a title the owner chooses.
- **Sources.** The `periods` block of the `journal` suite and the date probes of the `dates` suite, both *executed*:
  the Monday of an ISO week, the week-53 years, a CHECK that reads `'43' - 1`, and a virtual generated column over `GLOB`.

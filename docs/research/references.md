# References

What each source contributed to the decisions above. Reference numbers are identifiers, not a count:
the gaps are intentional.

## SQLite durability, format, and features

- <a id="r1"></a>**[R1]** SQLite: *The SQLite Application File Format* (essay) —
  <https://www.sqlite.org/appfileformat.html>
  "Data lives longer than code"; the SQL schema as human-readable documentation of the
  format; additive schema change as the compatibility mechanism; Application ID. → [D1](../decisions/D01-single-sqlite-file.md),
  [D2](../decisions/D02-typed-strict-tables.md), [D13](../decisions/D13-migrations-and-freeze.md).
- <a id="r2"></a>**[R2]** SQLite: *35% Faster Than the Filesystem* — internal-vs-external BLOB
  benchmarks — <https://www.sqlite.org/intern-v-extern-blob.html>
  Blobs < ~100 KB faster in-DB; larger blobs faster as files; page-size guidance. → [D9](../decisions/D09-binary-files.md).
- <a id="r3"></a>**[R3]** SQLite: *Faster Than The Filesystem* — <https://www.sqlite.org/fasterthanfs.html>
  Companion benchmark (10 KB blobs ~35% faster in DB, 20% less space). → [D9](../decisions/D09-binary-files.md).
- <a id="r4"></a>**[R4]** SQLite: *AUTOINCREMENT* — <https://www.sqlite.org/autoinc.html>
  `INTEGER PRIMARY KEY` = rowid alias; AUTOINCREMENT overhead, "usually not needed". → [D3](../decisions/D03-integer-ids.md).
- <a id="r5"></a>**[R5]** SQLite: *Date And Time Functions* — <https://www.sqlite.org/lang_datefunc.html>
  `strftime('%Y-%m-%dT%H:%M:%fZ','now')`; 'now' is UTC. → [D10](../decisions/D10-time-model.md).
- <a id="r6"></a>**[R6]** SQLite: *Datatypes* — <https://www.sqlite.org/datatype3.html>
  Dates as TEXT ISO-8601 is the canonical representation. → [D10](../decisions/D10-time-model.md).
- <a id="r10"></a>**[R10]** SQLite: *SQLite as a Library of Congress Recommended Storage Format* —
  <https://www.sqlite.org/locrsf.html> → [D1](../decisions/D01-single-sqlite-file.md).
- <a id="r11"></a>**[R11]** US Library of Congress: *Recommended Formats Statement — Data* —
  <https://www.loc.gov/preservation/resources/rfs/data.html>
  SQLite listed as a recommended storage format for datasets. → [D1](../decisions/D01-single-sqlite-file.md).
- <a id="r12"></a>**[R12]** SQLite: *SQLite Database File Format* (compatibility history) —
  <https://www.sqlite.org/formatchng.html>
  Backwards-compatible since 2004. → [D1](../decisions/D01-single-sqlite-file.md).
- <a id="r13"></a>**[R13]** SQLite: *Long Term Support* — <https://www.sqlite.org/lts.html>
  Commitment to support existing files through 2050. → [D1](../decisions/D01-single-sqlite-file.md).
- <a id="r14"></a>**[R14]** SQLite: *STRICT Tables* — <https://www.sqlite.org/stricttables.html>
  Per-table static typing since 3.37.0 (2021). → [D2](../decisions/D02-typed-strict-tables.md).
- <a id="r28"></a>**[R28]** allenap: *ISO-8601 and DATETIME in SQLite* —
  <https://allenap.me/posts/iso-8601-and-datetime-in-sqlite>
  DATETIME comparisons degrade to text; format discipline matters. → [D10](../decisions/D10-time-model.md). (See also Igor
  Bubelov, *Don't Trust SQLite Timestamps*, <https://bubelov.com/blog/2020/sqlite-timestamps/>.)

## Prior art: real long-lived personal databases

- <a id="r9"></a>**[R9]** KrauseFx/FxLifeSheet — repository, `db/create_tables.sql` —
  <https://github.com/KrauseFx/FxLifeSheet>
  Verified actual schema: single `raw_data` table (unix `timestamp`, denormalized
  `yearmonth/yearweek/year/quarter/month/day/hour/minute`, `key`, `question`, `type`,
  `value` TEXT, timezone-corrected `matcheddate`, `source`, `importedat`, `importid`).
  380k data points, 6+ years. → [D5](../decisions/D05-pages-and-day-pages.md), [D7](../decisions/D07-measurements.md), [D10](../decisions/D10-time-model.md).
- <a id="r42"></a>**[R42]** Felix Krause: *How I put my whole life into a single database* —
  <https://krausefx.com/blog/how-i-put-my-whole-life-into-a-single-database>
  Single self-owned DB; questions added/removed freely via config registry. → [D5](../decisions/D05-pages-and-day-pages.md), [D7](../decisions/D07-measurements.md).
- <a id="r7"></a>**[R7]** FxLifeSheet `tag_days` importer —
  <https://github.com/KrauseFx/FxLifeSheet/tree/master/ruby_importers/importers/tag_days>
  Local-date tagging with the timezone actually occupied — evidence that reconstructing
  local days after the fact is painful enough to need dedicated tooling. → [D10](../decisions/D10-time-model.md).
- <a id="r43"></a>**[R43]** Connor Gallic: *Open Brane — annotated, 8 columns, 80-line write path, one
  SQLite file* — <https://dev.to/connor_gallic/open-brane-annotated-8-columns-80-line-write-path-one-sqlite-file-43n3>
  Append-only single table (ts, source, type, actor, payload_json, attachment_uri,
  ingested_at); idempotent writes via `INSERT OR IGNORE`; 942k rows / 3 GB; blobs
  outside the DB. → [D7](../decisions/D07-measurements.md), [D8](../decisions/D08-entities-and-links.md), [D9](../decisions/D09-binary-files.md).
- <a id="r46"></a>**[R46]** Jamie Rubin: *ark, part 3* — <https://jamierubin.net/2026/06/09/ark-part-3/>
  Content-addressed flat store (sha256-named files, dedup for free) with SQLite as the
  index; typed edge tables doc↔doc, doc↔person, person↔person; the person-graph as the
  killer feature. → [D8](../decisions/D08-entities-and-links.md), [D9](../decisions/D09-binary-files.md).
- <a id="r8"></a>**[R8]** health-mcp: *DATA_MODEL.md* —
  <https://github.com/lukaisailovic/health-mcp/blob/main/docs/DATA_MODEL.md>
  Biomarkers with LOINC/UCUM/reference ranges (studied, then simplified away — [D7](../decisions/D07-measurements.md));
  two-tier wearables (raw mirror + normalized — deferred); UTC ISO timestamps **plus
  denormalized local `YYYY-MM-DD`**; numbered forward-only migrations. → [D7](../decisions/D07-measurements.md), [D10](../decisions/D10-time-model.md), [D13](../decisions/D13-migrations-and-freeze.md).
- <a id="r45"></a>**[R45]** Myome design paper — <https://scanlin.io/myome/paper.html>
  ~50 MB/year sensor data, ~5 GB/lifetime: scale is a non-issue; multi-resolution
  aggregation deferred. → [D7](../decisions/D07-measurements.md), [non-goals](../architecture/non-goals.md).
- <a id="r41"></a>**[R41]** mirat.dev: *Nine Years of Kaydet* —
  <https://mirat.dev/articles/nine-years-of-kaydet/>
  Plain-text entries + SQLite index; "plain text + SQLite beat every cloud note app"
  — 9 years of daily use. → [D4](../decisions/D04-database-is-canonical.md), [D5](../decisions/D05-pages-and-day-pages.md).

## The files-vs-database debate (D4 evidence)

- <a id="r34"></a>**[R34]** Logseq forum: *Why the database version and how it's going?* —
  <https://discuss.logseq.com/t/why-the-database-version-and-how-its-going/26744>
  Stated limits of markdown-canonical: block creation rewrites whole files; page rename
  updates all referencing files; files lack persistent IDs and timestamps. SQLite (via
  sqlite-wasm) chosen for the DB version.
- <a id="r35"></a>**[R35]** Logseq: *Big update — Logseq is splitting into two versions* —
  <https://logseq.io/page/b2ad9ce1-9cb7-4436-8083-54cb4516d324/df4dc09d-0a12-4c87-904e-22a9bf4c350a>
  Logseq OG (markdown files canonical) vs Logseq DB (SQLite canonical): the industry
  literally forked over this decision.
- <a id="r36"></a>**[R36]** *Logseq DB Unofficial FAQ* —
  <https://logseq.io/page/e87c7359-51f7-44fe-87b3-4a0cd9f2dee3/695feeec-88be-4c5b-8bf2-572513c2f730>
  In the DB version the database is canonical.
- <a id="r29"></a>**[R29]** morganholland/logseq-to-obsidian-migration —
  <https://github.com/morganholland/logseq-to-obsidian-migration>
  Dedicated tooling required between two "plain markdown" apps: journal filename
  conversion (`YYYY_MM_DD` → `YYYY-MM-DD`), frontmatter rewriting, property stripping,
  block-reference conversion, URL-decoded filenames.
- <a id="r30"></a>**[R30]** laughedelic/outbreak (Obsidian↔Logseq converter) —
  <https://github.com/laughedelic/outbreak>
  Syntax translation of tasks, highlights, wiki-links, embeds, callouts, frontmatter.
- <a id="r31"></a>**[R31]** msfjarvis: *The Obsidian Migration — One Week Later* —
  <https://msfjarvis.dev/posts/the-obsidian-migration--one-week-later/>
  First-person account of wikilink/tag/journal-date incompatibilities.
- <a id="r32"></a>**[R32]** laughedelic/obsidian-importer: Logseq assessment —
  <https://github.com/laughedelic/obsidian-importer/blob/feat/logseq-importer/docs/logseq-importer-assessment.md>
  Catalogue of Logseq-specific constructs an importer must translate.
## Schema-design evidence (D2, D3)

- <a id="r15"></a>**[R15]** Anton Zhiyanov: *JSON and virtual columns in SQLite* —
  <https://antonz.org/json-virtual-columns/>
  `json_extract()` parses text on every call; generated columns needed to make it fast.
- <a id="r16"></a>**[R16]** DelphiTools: *SQLite as a no-SQL database* —
  <https://www.delphitools.info/2021/06/17/sqlite-as-a-no-sql-database/>
  Measured: 169 ms unindexed scan vs 0.15 ms indexed column lookup; JSON paths worse.
- <a id="r17"></a>**[R17]** Simon Willison: *sqlite-tags-benchmark* —
  <https://github.com/simonw/research/tree/main/sqlite-tags-benchmark>
  Five strategies benchmarked on 100k rows: indexed relational fastest; `json_each()`
  scans much slower; FTS5 a close second (supports [D14](../decisions/D14-ui-and-tools.md)'s search choice).
- <a id="r18"></a>**[R18]** Jason Graczyk (NWOS): *Postgres JSON Columns vs Proper Schemas* —
  <https://nwos.com/daily/postgres-json-columns-when-theyre-a-lifesaver-and-when-theyre-a-trap>
  Practitioner pattern: the "flexible" JSON column becomes "an unmapped wasteland of
  inconsistent keys" within ~6 months.
- <a id="r19"></a>**[R19]** Cybertec: *entity-attribute-value design — don't do it!* —
  <https://www.cybertec-postgresql.com/en/entity-attribute-value-eav-design-in-postgresql-dont-do-it/>
  EAV as documented anti-pattern once the attribute set is fixed. (See also: SQLBlog
  *What is so bad about EAV, anyway?*; cedanet.com.au EAV anti-pattern page; Red Gate
  *Avoiding the EAV of Destruction*.)
- <a id="r26"></a>**[R26]** lik.ai: *SQLite Primary Key Benchmarks (UUIDv7, UUIDv4, Snowflake, Integer)* —
  <https://lik.ai/blog/sqlite-primary-key-benchmarks/>
  Integer fastest overall; UUID variants close for query, worse for insert; UUIDv4 vs v7
  difference minimal. (Same benchmark: <https://engineered.at/articles/sqlite-primary-key-benchmarks-uuidv7-uuidv4-snowflake-integer>.)
- <a id="r27"></a>**[R27]** Production Hardening: *Integer Primary Keys for Embedded Writes* —
  <https://www.productionhardening.org/sqlite-architecture-production-hardening/schema-design-for-edge-devices/integer-primary-keys-for-embedded-writes/>
  Random TEXT UUID keys scatter inserts across the B-tree (page splits, dirty-block
  churn). → [D3](../decisions/D03-integer-ids.md).
- <a id="r75"></a>**[R75]** vlcn.io: *cr-sqlite — Constraints* — <https://vlcn.io/docs/cr-sqlite/constraints> (code:
  <https://github.com/vlcn-io/cr-sqlite>, last release v0.16.3, January 2024) A merged ("CRR") table
  may not have checked foreign keys, unique constraints other than the primary key, or CHECKs that
  depend on other columns. → [D3](../decisions/D03-integer-ids.md), [non-goals](../architecture/non-goals.md).
- <a id="r47"></a>**[R47]** Microsoft Research TR-2006-45: *To BLOB or Not To BLOB* —
  <https://www.microsoft.com/en-us/research/wp-content/uploads/2006/04/tr-2006-45.pdf>
  Classic study; break-even a few hundred KB — small in DB, large on filesystem. → [D9](../decisions/D09-binary-files.md).

## Operations: migrations, audit (D12, D13)

- <a id="r20"></a>**[R20]** Ash: *Simple Migration System in SQLite* —
  <https://www.ash.dev/blog/simple-migration-system-in-sqlite/>
  `PRAGMA user_version` as the migration counter; no framework.
- <a id="r21"></a>**[R21]** Nhân: *Working with SQLite in Python without an ORM or migration framework* —
  <https://hi.imnhan.com/sqlite-python/>
  `mXXXX.sql` + user_version in < 100 lines.
- <a id="r22"></a>**[R22]** David Röthlisberger: *Simple declarative schema migration for SQLite* —
  <https://david.rothlis.net/declarative-schema-migration-for-sqlite/>
  Schema-in-one-file, auto-applied additions; the declarative extreme of the same idea.
- <a id="r48"></a>**[R48]** Simon Willison: *sqlite-history — tracking changes to SQLite tables using
  triggers* — <https://simonwillison.net/2023/Apr/15/sqlite-history/> (code:
  <https://github.com/simonw/sqlite-history>)
  The documented fallback audit mechanism if [D12](../decisions/D12-no-revision-tables.md) ever proves insufficient.

## UI tooling (D14)

- <a id="r49"></a>**[R49]** Datasette — <https://datasette.io/> (code:
  <https://github.com/simonw/datasette>)
  Instant browsing/SQL/faceting/JSON-CSV export over any SQLite file; Datasette Lite runs
  in-browser.
- <a id="r50"></a>**[R50]** sqlite-web — <https://github.com/coleifer/sqlite-web> (**not used** — it edits rows, i.e. is a second writer; it does have `-r/--read-only`. [D14](../decisions/D14-ui-and-tools.md))
  Web-based table browser with row insert/update/delete, CSV/JSON import-export.

## SQLite behaviour and money (D18)

- <a id="r53"></a>**[R53]** SQLite: *ON CONFLICT clause* — <https://www.sqlite.org/lang_conflict.html>
  "When the REPLACE conflict resolution strategy deletes rows in order to satisfy a
  constraint, delete triggers fire if and only if recursive triggers are enabled." The reason
  `PRAGMA recursive_triggers = ON` is mandatory ([connection setup](../contract/connections.md)). (The page's wording on which
  constraints `IGNORE` skips is loose; what it does was executed.) → [D18](../decisions/D18-money.md), [imports](../contract/imports.md), [connection setup](../contract/connections.md).
- <a id="r54"></a>**[R54]** SQLite: *PRAGMA statements* — <https://www.sqlite.org/pragma.html>
  `synchronous=NORMAL` in WAL mode: "A transaction committed in WAL mode with
  synchronous=NORMAL might roll back following a power loss or system crash";
  `recursive_triggers` is a per-connection setting, off by default. → [connection setup](../contract/connections.md), [D12](../decisions/D12-no-revision-tables.md).
- <a id="r55"></a>**[R55]** SQLite: *ALTER TABLE* — <https://www.sqlite.org/lang_altertable.html>
  With the release notes, <https://www.sqlite.org/changes.html>: since 3.53.0 (2026-04-09)
  `ALTER TABLE` can add and remove NOT NULL and CHECK constraints. Its restriction to *named* constraints,
  and that `ADD CONSTRAINT` checks existing rows, were found by executing it on 3.53.4. → [D13](../decisions/D13-migrations-and-freeze.md), [D18](../decisions/D18-money.md).
- <a id="r56"></a>**[R56]** Beancount `balance` directive (via the `beancount_ex` library docs — the official
  syntax page URL tried returned 404): asserts an account's balance at the *beginning* of a
  date — the reason `balances.day` states "end of day" explicitly.
  <https://beancount-ex.hexdocs.pm/0.6.0/Beancount.Directives.Balance.html>. → [D18](../decisions/D18-money.md).
- <a id="r57"></a>**[R57]** ISO 4217 (as tabulated at <https://en.wikipedia.org/wiki/ISO_4217>): the minor-unit
  exponent is a per-currency fact (JPY 0, KWD 3), and a few currencies (MRU, MGA) subdivide
  by 5 — why `currencies.subunits` is a per-row integer and not a global "cents" assumption. → [D18](../decisions/D18-money.md).

## The wikilink save contract (D19)

- <a id="r58"></a>**[R58]** Microsoft Learn: *Naming Files, Paths, and Namespaces* —
  <https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file>
  Reserved names (`CON`, `PRN`, `AUX`, `NUL`, `COM1`–`COM9`, `LPT1`–`LPT9`, and the superscript
  digits `COM¹ COM² COM³ LPT¹ LPT² LPT³`): "avoid these names followed immediately by an extension; for
  example, NUL.txt and NUL.tar.gz are both equivalent to NUL"; no trailing space or period. The
  DDL rejects the bare names, the names before an extension and the superscript names. → [titles and wikilinks](../contract/titles-and-wikilinks.md), [D5](../decisions/D05-pages-and-day-pages.md).
- <a id="r59"></a>**[R59]** goldmark, a CommonMark-compliant Markdown parser for Go —
  <https://github.com/yuin/goldmark>; the specification it implements is
  <https://spec.commonmark.org/>. The reader the suites in `tests/` run the vectors through: which text it hands
  back (code spans, fences, indented code, raw HTML and image alt text are not text; escapes and
  entities are decoded; `#Heading` without a space is not a heading) was executed, not read from
  the spec. → [titles and wikilinks](../contract/titles-and-wikilinks.md), [D19](../decisions/D19-wikilink-save-contract.md).
- <a id="r63"></a>**[R63]** Unicode Technical Standard #39, *Unicode Security Mechanisms* —
  <https://www.unicode.org/reports/tr39/> (with UAX #31, *Identifiers*): default-ignorable and bidi
  characters are dropped or rejected before identifiers are compared, because they are invisible.
  → [D5](../decisions/D05-pages-and-day-pages.md), [titles and wikilinks](../contract/titles-and-wikilinks.md) (the invisible-character rule).
- <a id="r74"></a>**[R74]** Unicode: *Character Encoding Stability Policies* —
  <https://www.unicode.org/policies/stability_policy.html> Case folding stability (Unicode 5.2+): for a
  string of assigned characters, `toCasefold(toNFKC(S))` is the same under every later version — the
  formal promise covers NFKC text only. Normalization stability covers assigned characters. → [titles and wikilinks](../contract/titles-and-wikilinks.md) (`Cn`), [non-goals](../architecture/non-goals.md).

## Reliability of the file (integrity checks, connection setup, imports)

- <a id="r64"></a>**[R64]** SQLite: *The Checksum VFS Shim* — <https://www.sqlite.org/cksumvfs.html>
  An 8-byte checksum per page (reserve bytes = 8), `SQLITE_IOERR_DATA` on a mismatch; SQLite ≥ 3.32.
  Considered for in-value damage and not used (an extension in every writer); a checksumming
  filesystem does the same job below the file. → [integrity checks](../contract/integrity-checks.md).
- <a id="r65"></a>**[R65]** SQLite: *Write-Ahead Logging* — <https://www.sqlite.org/wal.html>
  The WAL-reset bug (3.7.0 – 3.51.2, fixed in 3.51.3 and 3.53.0; backports 3.44.6 and 3.50.7): two or more
  connections, a write racing a checkpoint, a lost transaction. Also: checkpoint starvation by
  readers that never let go, "WAL does not work over a network filesystem", the conditions for
  read-only access. → [connection setup](../contract/connections.md), `aminte_meta.sqlite`.
- <a id="r66"></a>**[R66]** SQLite: *How To Corrupt An SQLite Database File* —
  <https://www.sqlite.org/howtocorrupt.html> Network filesystems, files copied while open, broken
  POSIX locks, `immutable` on a changing file. → [connection setup](../contract/connections.md).
- <a id="r67"></a>**[R67]** T. S. Pillai et al., *All File Systems Are Not Created Equal: On the Complexity of
  Crafting Crash-Consistent Applications*, OSDI 2014 —
  <https://www.usenix.org/conference/osdi14/technical-sessions/presentation/pillai>
  SQLite among the studied applications: crash consistency depends on the filesystem's persistence
  properties, which is why power loss is listed as documented, not simulated. → [threat model](../contract/threat-model.md).

## Time and dates (D7, D10, D18, non-goals)

- <a id="r68"></a>**[R68]** R. T. Snodgrass, *Developing Time-Oriented Database Applications in SQL*, Morgan
  Kaufmann 2000; SQL:2011's application-time and system-time periods are the same two clocks. Valid
  time vs transaction time — the two clocks of `measurements` and `balances`. → [D7](../decisions/D07-measurements.md), [D18](../decisions/D18-money.md).
- <a id="r69"></a>**[R69]** RFC 9557, *Date and Time on the Internet: Timestamps with Additional Information*
  (2024) — <https://datatracker.ietf.org/doc/html/rfc9557> An IANA zone in brackets after an RFC 3339
  instant. → [D10](../decisions/D10-time-model.md).
- <a id="r70"></a>**[R70]** Library of Congress, *Extended Date/Time Format (EDTF)*, incorporated in ISO 8601-2:2019 —
  <https://www.loc.gov/standards/datetime/> Year and month precision, approximate (`~`) and uncertain
  (`?`) dates. → [non-goals](../architecture/non-goals.md) (partial dates).

## Recurrence (D15)

- <a id="r51"></a>**[R51]** Stack Overflow: *Should I store dates or recurrence rules in my database when
  building a calendar app?* —
  <https://stackoverflow.com/questions/4239871/should-i-store-dates-or-recurrence-rules-in-my-database-when-building-a-calendar>
  The canonical store-the-rule-vs-materialize-the-instances discussion; the accepted
  answer separates "canonical" (the rule) from "serving" (generated dates) — exactly
  the template/expand-at-read split of [D15](../decisions/D15-recurrence.md)'s deferred design.
- <a id="r52"></a>**[R52]** Microsoft Learn: *dbo.sysschedules (Transact-SQL)* —
  <https://learn.microsoft.com/en-us/sql/relational-databases/system-tables/dbo-sysschedules-transact-sql>
  The structured-interval family in production since SQL Server 7: `freq_type` /
  `freq_interval` / `freq_recurrence_factor` / `freq_relative_interval` — the shape
  `repeat` / `repeat_weekdays` / `repeat_every` of [D15](../decisions/D15-recurrence.md)'s deferred design follows.

## Location (D21)

- <a id="r71"></a>**[R71]** RFC 7946, *The GeoJSON Format* (2016) — <https://datatracker.ietf.org/doc/html/rfc7946>
  Positions are WGS84 longitude and latitude in decimal degrees; geometry that crosses the
  antimeridian is cut in two (RFC 7946 section 3.1.9). → [D21](../decisions/D21-location-history.md).
- <a id="r72"></a>**[R72]** SQLite: *Built-in Mathematical SQL Functions* — <https://sqlite.org/lang_mathfunc.html>
  `sin`, `cos`, `acos`, `radians` are active only in builds compiled with
  `-DSQLITE_ENABLE_MATH_FUNCTIONS`. → [D21](../decisions/D21-location-history.md).

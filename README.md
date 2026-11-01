# Lifelog

`lifelog` is one Go binary that keeps a lifetime-scale, single-user SQLite database, `life.db` — a life log and
its backup: a journal of day pages, notes, the people in them, health readings, and the
files you keep — a recording's transcript, a PDF's text, a photo's small picture. It serves the file two ways: a
hypermedia API and a CLI. The browser is an extension, `lifelog-web`: a client of the API, like the MCP server. Every
other program beside the core is an extension too: the MCP server, the setup of a coding agent, Google Photos and the
connectors that name a third party ([extensions](#extensions)).

The database it writes is specified independently of it, so another program — in any language — can read
`life.db`, or write its own:

- **[docs/](docs/README.md)** — the contract of `life.db`: goals, the canonical [schema.sql](docs/schema/schema.sql),
  the storage contract, the decision log, the query cookbook, research, and the process (issues, proposals, plans).
  It names no implementation; [building a writer](docs/guides/building-a-writer.md) is the way in for another one.
- **[tests/](tests/README.md)** — the validation suites: every executed claim of the docs, run against the schema and
  against this writer's own extraction and save. `go test ./tests`.
- **[API.md](API.md)** — the wire shape for client authors: every entity class and its properties, the errors, the
  pages, how to reach a server; a test keeps it equal to the code.
- **[AGENTS.md](AGENTS.md)** — the rules for anyone, human or agent, working in this repo.

`lifelog` implements the contract and never defines it: a rule found missing or ambiguous here is an
[issue](docs/issues/README.md) against the docs, not a fix in this code alone.

```
go build -o lifelog ./cmd/lifelog   # Go >= 1.27, no cgo
for d in extensions/*/; do go build -C "$d" -o "$PWD/" ./cmd/...; done   # every extension, beside lifelog: lifelog-web, lifelog-mcp, lifelog-agent, lifelog-whatsapp, lifelog-ticktick, ...
lifelog init                               # life.db beside the program; --db PATH or $LIFELOG_DB names another
lifelog agentic-init                       # AGENTS.md, skills and MCP configs here, for a coding agent opened here

lifelog capture "Ran 5k with [[Sam]] #running" --mood 4
lifelog do settings model_url=http://HOST/v1 model=ID   # the model of the agent: kept in life.settings.json, beside life.db
lifelog day --human
mkdir -p ~/life && (umask 077; head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n' > ~/life/token)   # a token file: 64 characters
lifelog serve                              # the API on http://127.0.0.1:7777: Siren JSON, this machine only
lifelog serve --token-file ~/life/token --with web   # the browser too, on http://127.0.0.1:7780: log in with the token
lifelog serve --token-file ~/life/token --with web --with agent --with whatsapp   # and the agent that answers, and the WhatsApp transport, beside them
lifelog mcp --agent lmstudio               # MCP on stdio (the extension lifelog-mcp); rows are written as agent:lmstudio
lifelog snapshot --snapshots ~/snapshots --human  # life-YYYY-MM-DD.db and its restore check
lifelog file memo.m4a --text memo.txt     # keep a file: its text, the original hashed and never stored
lifelog file IMG_0001.HEIC --preview IMG_0001.jpg --day 2026-10-05   # a HEIC: lifelog reads neither its picture nor its day; send a JPEG and the day
lifelog cover 12 lake.jpg                  # a new photo as page 12's cover; `lifelog cover 12 --title Lake.jpg` takes a kept one
```

A token turns on the bearer check of `serve`. It is one line of 32 characters or more, and it must not start or end with
a space or a tab. `lifelog-web` asks the owner for the same token at its login page. The address `http://127.0.0.1:7777`
answers Siren JSON and no page ([API.md](API.md#reaching-a-server) names the plain answers of `net/http` that are not
Siren). On Windows, in PowerShell 7.2 or later, these two lines make the token file:

```
New-Item -ItemType Directory -Force $HOME\life | Out-Null
[Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(32)) | Set-Content -NoNewline $HOME\life\token
```

To run `lifelog serve` as a system service with a nightly snapshot, see [deploy/](deploy/README.md): the unit files
and the snapshot script, as a reference to compare with the installed copies before you replace them. The reference
unit runs `serve ... --with web`, so it needs `lifelog-web` beside `lifelog`.

An import is done by a program that loads rows itself ([imports](docs/contract/imports.md)), or by a coding agent
with the owner, in a conversation: the agent reads a source with its own tools and writes through the catalog's MCP
tools, and the owner says yes or no to each person ([plan 089](docs/plans/089-no-import-process.md)).
`lifelog agentic-init` prepares the folder for it ([plan 090](docs/plans/090-agentic-init.md)); then open Claude
Code, Codex, OpenCode or Pi in the folder.

```
lifelog snapshot                                   # the owner, before an import session
```

## Decisions

These are this application's own engineering decisions ([D14](docs/decisions/D14-ui-and-tools.md)).

- **The four rings** ([plan 135](docs/plans/135-extensions-outside-the-core.md)). A thing lives in the innermost ring
  that can hold it. Ring 0 is the file: `schema.sql` is the domain model, and every rule about the data is a CHECK or
  a trigger there. Ring 1 is the writer, `internal/core` and what it reads: transactions and only what SQLite cannot
  say. Ring 2 is the catalog and the CLI, `internal/api` and `cmd/lifelog`: one catalog, one handler and the CLI's
  `do`, all generated from the catalog. Ring 2 holds no browser: no HTML, no CSS, no JavaScript and no picture. Ring 3
  is the extensions: one program `lifelog-NAME` each, a client of [API.md](API.md), run as `lifelog NAME`, in any
  language, and the only ring that names a third party ([the contract of an extension](extensions/README.md)). The
  browser, `lifelog-web`, is one of them, by the owner's word of 2026-10-31
  ([plan 161](docs/plans/161-the-browser-as-an-extension.md)). An extension may also serve a catalog that ring 2
  merges into its own ([connected apps](#decisions)). Three tests of `cmd/lifelog` keep the rings: the core names no
  third party, the core dials only its own server, and rings 0 to 2 hold no HTML, CSS, JavaScript or picture
  (`TestTheCoreHoldsNoPresentation`).

  ```mermaid
  flowchart TB
    subgraph R0["ring 0: the file"]
      S["schema.sql: the domain model and every rule about the data"]
    end
    subgraph R1["ring 1: the writer"]
      C["internal/core, db, text, photo, preview"]
    end
    subgraph R2["ring 2: the catalog and the CLI"]
      K["internal/api and cmd/lifelog: the API and the CLI"]
    end
    subgraph R3["ring 3: extensions"]
      X["lifelog-web, lifelog-mcp, lifelog-agentic-init, lifelog-gphotos, lifelog-agent, lifelog-whatsapp, lifelog-ticktick: clients of API.md; they may serve a catalog of their own"]
    end
    S --> C --> K --> X
  ```
- **Connected apps through the catalog** ([plan 154](docs/plans/154-connected-apps-through-the-catalog.md)). The
  interface between `lifelog` and an app is the Siren catalog and nothing else. An extension that `serve --with NAME`
  runs may serve its own `GET /actions` on the loopback address `LIFELOG_LISTEN`. The server puts its actions in
  `GET /actions` as `NAME-action` and forwards `/with/NAME/...` to it, as the writer of the request. So the CLI's `do`,
  the MCP tools and the browser carry an app with no further work. The core names no app and no domain: the code of
  a third party stays in the connector. A ring 3 program may serve a catalog that ring 2 aggregates. Only two files of
  the core may call `client.Remote`: `cmd/lifelog/with.go` and `internal/api/with.go`, besides `cmd/lifelog/main.go`
  and the test helper. The protocol is in [the contract of an extension](extensions/README.md#a-catalog-of-its-own),
  and the wire shape is in [API.md](API.md#connected-apps).
- **One handler, three surfaces.** `internal/api` is the only thing the surfaces call. `serve` mounts it on a
  socket; the CLI calls it in-process (`client`), or a running server with `--url`. The MCP server is the
  extension `lifelog-mcp` ([extensions/mcp](extensions/mcp/README.md)): a client of the HTTP API, run as
  `lifelog mcp`, which gives it a temporary server on the file. The browser is the extension `lifelog-web`
  ([extensions/web](extensions/web/README.md)): a client of the HTTP API too, run by `serve --with web`.
  Writes go through `internal/core` only: no generic UPDATE or DELETE exists anywhere.
- **Server shutdown drains admitted requests.** An interrupt or a SIGTERM closes the listener, then allows ten seconds for active
  requests to finish before closing their connections. Shutdown completes before the database closes; expiration
  is reported as an error.
- **Hypermedia (HATEOAS).** Every response is a Siren entity: properties, links, and the actions legal on that
  resource, with their fields prefilled (a page's `save-body` carries its body and `version`). `GET /actions` is
  the catalog; the CLI's `do` and the MCP tools are generated from it, so a new action needs no client change. A
  client that cannot fill a path, such as a form of a browser, asks `GET /actions?new=<action>&id=12`: the answer has
  that action's `href` filled in ([plan 131](docs/plans/131-the-pareto-pass.md)). A file field takes the path of a
  local file in the client (the CLI's `do`, the MCP tools), which streams it; a browser sends the file it picked.
  An error is an entity too: `status`, a `code` a client branches on (`stale_version`, `exists`, `not_found`, `snapshot_refused`, `restore_check_failed`,
  `invalid`, `cross_origin`, `token_required`, or the status's general name) and the message. A fault of the server (500
  `internal`) says only "internal error": the server's log has the cause. The lists `/days`,
  `/browse` and `/search` take `limit` (1–500) and `offset` and link `next` and `prev`
  ([plan 079](docs/plans/079-client-contract-hygiene.md)).
- **Daily use** ([plan 091](docs/plans/091-the-browser-for-daily-use.md)). The root is the entry point of the API,
  with the five newest day pages. A day carries its mood and the `save-body` of its day page, so a client edits in
  place. These are properties and actions of the entities, so every client gets them ([API.md](API.md)).
  `/months/{YYYY-MM}` is the calendar of a month (each day, whether it has a page, its first line, its cover, its mood
  and its readings; the day view holds the rest), and the catalog action `open-day` goes to a day by its date, or to
  today with no date. Every field of the catalog whose type is `date` reads the word `today` as the server's local
  day. The handlers read today from one clock, which a test fixes. `/years/{YYYY}` is the year (twelve months); the
  People list names each person with the pages that mention it and the day of the last one
  ([plan 097](docs/plans/097-the-almanac-reflects.md)).
- **Period pages** ([D30](docs/decisions/D30-period-pages.md)). A week (`2026-W43`), a month, a quarter and a year may
  have a page: a plain page whose title is the key. `/weeks/{week}`, `/months/{m}` and `/years/{y}` show the page of
  their period. A period with no page offers `create-page` with its key as the title, the one action that makes it.
- **A cover for every page** ([plan 127](docs/plans/127-a-cover-for-every-page.md)). A page's cover is the first live
  file with a preview that its body embeds (`![[Ana.jpg]]`); a day page's first photo is its cover with no action. A
  file page has none: its own picture stands for it. A client that draws the cover leaves that embed out of the body.
  A Browse row's `picture` gives the href of its cover. The schema has no link for it, by the owner's choice
  (2026-10-15, again 2026-10-17): an embed is how any page shows a picture, so every surface can set it. `set-cover`
  sets it: a new photo (a form with a file field, and `lifelog cover PAGE-ID PHOTO`) or a kept picture by its title
  (`lifelog cover PAGE-ID --title T`, and an agent by either, a new photo by its path). `GET /pages/{id}/cover` is the
  picker, the one way to set a cover in a client that draws pages: the kept pictures (one the body embeds already
  trades places with the cover's embed), and a new photo. The new photo goes in no day page.
- **Provenance per surface.** `source` is `cli`, `api`, `ui` (`lifelog-web`, which sends `Lifelog-Source: ui`) or
  `agent:<name>` (MCP); the `Lifelog-Source` header sets it. A request with no header writes as `api`: there is no
  default `ui`.
- **Optimistic saves.** A body save sends the `version` (the decimal `entities.revision` token) it read; a newer one is a 409.
- **The readings of a metric** ([plan 100](docs/plans/100-one-page-a-metric.md), [plan 131](docs/plans/131-the-pareto-pass.md), [plan 144](docs/plans/144-one-view-for-a-metric.md)).
  A metric has one view, `/metrics/{name}`: its page (a metric is a page, D27) and its readings. The readings are
  `(from, to]`; with no `from`, every admitted day through `to`, on every surface. The series carries the page, so the
  API, the CLI and the MCP tools read both in one answer. The page of a live metric links to the series (rel `series`).
  A client that draws the view and not the page follows that link.
- **Connections** ([connection setup](docs/contract/connections.md)): one writing connection with the pragmas
  set by DSN and read back by a connection hook (a wrong value refuses the connection), `SQLITE_DBCONFIG_DEFENSIVE`,
  `BEGIN IMMEDIATE` via `_txlock=immediate`, the 3.51.3 floor checked; readers use `mode=ro` with `trusted_schema=OFF`, read back the same way.
- **Unicode.** Registry name keys use `x/text`'s full case fold; the `Cn` rule uses a pinned Unicode 15.0 assigned table,
  not the tables Go ships, so the titles this writer accepts do not change with a Go upgrade.
- **One transaction type.** Every write is a method on `core.Tx`; an operation alone is one transaction, and an
  operation that writes several rows (`file-pages`) runs several of the same methods in one. Nothing writes
  around them.
- **The folder of the program** ([plan 090](docs/plans/090-agentic-init.md)). Without `--db`, `LIFELOG_DB` or
  `--url`, a command uses the `life.db` beside the executable, and `lifelog init` with no path makes it there; a
  missing file is still refused. `lifelog agentic-init [--force]` is the extension `lifelog-agentic-init`
  ([extensions/agentic](extensions/agentic/README.md)). It writes into that folder, from templates embedded in the
  extension: `AGENTS.md` (the folder, the modes, the tools, the rules and a privacy note), a `CLAUDE.md` that
  imports it, three skills in `.agents/skills/` and `.claude/skills/`, and the MCP config of Claude Code
  (`.mcp.json`), Codex (`.codex/config.toml`), OpenCode (`opencode.json`) and Pi (`.pi/mcp.json`), each starting
  `lifelog mcp --agent NAME` on that `life.db` with absolute paths. A file with another text is kept unless
  `--force`; run it again after the folder moves. Like `init`, it is a setup command of the machine, not a catalog
  action: it writes files beside the program.
- **Owner-only actions.** The actions that the catalog marks `owner` are
  refused to an `agent:*` or `import:*` writer and are never MCP tools. The table [every action on every surface](#every-action-on-every-surface)
  lists each one (MCP column: "none, the owner's"), and a test holds the table equal to the catalog. The full-text check of `integrity-check` holds the write
  lock while it runs, so an agent must not start it. `rebuild-search` (`POST /search/rebuild`) is the repair of the
  full-text index, which the check finds: the FTS5 `'rebuild'` in one write. `resync-links` (`POST /links/resync`)
  rewrites the links of every live page whose links differ from its body (`link_drift`), by the code of a body save
  ([integrity checks](docs/contract/integrity-checks.md)). `GET /integrity` offers the two repairs, so the owner
  reads what needs them and runs them on one page. `GET /settings` and `GET /snapshots/{name}` are owner-only too, and are the owner-only reads that are not actions. An agent that imports with the owner asks for them in the conversation.
- **No conversation inside the writer** ([plan 116](docs/plans/116-strip-to-the-core.md), [RFC 0015](docs/rfcs/0015-no-conversation.md)).
  The owner's decision (2026-10-15): `ask` and the voice note are removed, with the model and transcriber
  endpoints they needed. The core calls no model. The owner talks to `life.db` through a coding agent's MCP tools, and
  a recording is kept as a file with its text ([D9](docs/decisions/D09-binary-files.md)). A conversation is a plain
  page that an agent writes to (the decision "The conversation of a day", below).
- **Renames select a preferred name on the same identity**, retaining direct aliases as
  [titles and wikilinks](docs/contract/titles-and-wikilinks.md) ("Renames") requires and
  [rename a page](docs/cookbook/rename-a-page.md) writes; `lifelog rename` and the API's `rename` action run it.
- **The schema is embedded.** `schema.go` at the root embeds `docs/schema/schema.sql`, and `internal/db` reads it; a change to the DDL
  is live in the binary at the next build. `lifelog init` refuses an existing file; `Open` refuses a file without Lifelog's
  `application_id`.
- **Snapshots are the owner's, from any surface** ([take a snapshot](docs/cookbook/take-a-snapshot.md),
  [plan 082](docs/plans/082-snapshot-in-the-catalog.md)). `snapshot` is an owner-only action of the catalog
  (`POST /snapshots`): refused to an `agent:*` or `import:*` writer, never an MCP tool, and offered at the root to the
  owner. It takes
  no path: the file lands in the folder `serve --snapshots DIR` names, or beside `life.db`, named by the local day
  and never overwritten; it refuses a folder inside a git work tree, and its restore check opens the snapshot with
  `Close` skipping `PRAGMA optimize`. The answer carries the path, the check and the link `download`:
  `GET /snapshots/{name}` sends the file, so a client on another device keeps a copy of `life.db`. It sends only a
  good snapshot of the folder by its name (opened through `os.Root`), never the live file.
  `lifelog snapshot [--snapshots DIR]` is
  the action in-process, `--snapshots` choosing the folder, as it does for `serve`; with `--url` the server's folder is
  the one, and `--snapshots` is refused.
  Restoring stays the manual procedure of the cookbook.
- **The settings file** ([plan 146](docs/plans/146-settings-and-the-chat-column.md)). The settings of the application
  are a JSON file beside the database, `life.settings.json` for `life.db` (the path with `.db` cut and
  `.settings.json` added), mode 0600, written through a temporary file and a rename, and never in git (`.gitignore`). They are not in `life.db`: the
  model key is a secret, and a snapshot copies the database. They are not in `docs/` either: they belong to this
  application, not to the contract. Now they are `model_url` (the base URL of a server that speaks the
  chat-completions protocol, ending in `/v1`), `model` (the model id), `model_key` (a bearer credential, optional) and
  `owner_number` (the digits of the owner's phone number with the country code: the one number that the agent on the
  phone answers; a value that is not digits is 422); a setting is added when a feature needs it. The settings of the
  browser (the ground, the lock) are not here: `lifelog-web` keeps its own
  ([the settings of the browser](extensions/web/README.md#the-settings-of-the-browser)). The action `settings`
  (`POST /settings`) and the resource
  `GET /settings` are the owner's: refused to an `agent:*` or `import:*` writer, and the action is never an MCP tool.
  `GET /actions` offers `settings` with no value, because any writer reads the catalog; only `GET /settings` fills
  the values, the key too, so the owner's form sends the key back as it is. A field of the type `password` is one that
  a client draws as a password input. A field left out of `settings` stays, and an empty one clears the setting. A
  store with no settings path (a test) refuses a read and a write. The core calls no model: the agent reads the
  settings.
- **The conversation of a day** ([plan 146](docs/plans/146-settings-and-the-chat-column.md), after
  [plan 145](docs/plans/145-whatsapp.md)). The conversation of a day is the plain page `Conversation YYYY-MM-DD`, filed
  in `Conversations`, with the first line `[[YYYY-MM-DD]]`. A turn runs from a marker line, `**Owner** (HH:MM): text` or
  `**lifelog** (HH:MM): text` (outside a fenced code block), to the next marker line, with the tool lines of the agent
  (`` - `tool` ``) just before its marker. The time is
  the server's local time. The marker may name the relay, `**Owner** (HH:MM, via whatsapp): `: `say` writes it when the
  request names `via`, and the turn carries it as `via` (and the addresses of its tool lines as `refs`). [API.md](API.md#conversation) states the format for an agent in any language, and
  `internal/api/conversation.go` holds it in this program. `say` (`POST /days/{day}/say`, `today` allowed) adds the
  owner's turn and makes the page on the first turn of the day, in one write; it is for today only (another day is
  422, and that day's conversation offers no `say`); it is the owner's alone: an
  `agent:*` or `import:*` writer is refused (403), and it is never an MCP tool, so no agent speaks as the owner. The
  agent's turn is written by `reply` (`POST /days/{day}/reply`), which is no owner action: the core puts the reply right
  after the owner's turn that it answers, in one write. Each turn of the conversation has a `key`, and `reply` names the
  turn by it: the key does not move when a reply or a say is written, or when an edit removes an earlier turn that is
  different; an edit that removes an earlier equal turn (the same who, minute, relay and words) gives its key to the later
  equal turn, and a key that is no more on the page is 409. An agent or an import cannot write the body of a conversation
  page (403): not with `save-body`, not with `set-cover`, and not with an action that is added later, because the core
  refuses it in its one write of a body. No write makes a page under a conversation title or changes the type of its
  page (409). So only `say` writes a turn of the owner for a surface, and an edit of the owner that types a marker line is
  a turn of the owner too. A wikilink may still make an empty page
  with that title, and the first `say` adopts it. The core escapes a line of a `say` or a `reply` that reads as a
  marker or as a tool line, and closes a code fence that the text or an edit leaves open, so the page stays readable as
  turns; the turns give the words back without the backslash of a tool line, and the last turn is read with its fence closed. The WhatsApp extension relays the owner's words as the owner, with `via=whatsapp`; the paragraph then reads `**Owner** (HH:MM, via whatsapp): `. `GET /days/{day}/conversation` gives the turns, parsed from
  the body, with the page's properties, links and actions when the page exists (as a series carries a metric's page,
  [plan 144](docs/plans/144-one-view-for-a-metric.md)), and `say`. The page of a live conversation links to it (rel
  `conversation`). A conversation page keeps its title: it offers no
  `rename` or `promote`, and `rename` to such a title is 409. The agent (`lifelog-agent`, run by `serve --with agent`) reads
  today's page, answers a turn of the owner on it and writes as `agent:lifelog`; the core calls no model. The conversation names each address in the tool lines of a turn as a
  `ref`, and `?embed=ref` ([embedding](API.md#embedding)) replaces it by the entity that the server's own handler
  answers, as the writer `agent:embed`: an owner-only read such as `/settings` or `/integrity` is refused, and its
  `ref` stays a link. A client draws the entity that answers as a card by its class.
- **One delete, of one row or several** ([plan 107](docs/plans/107-delete-several-at-once.md),
  [plan 131](docs/plans/131-the-pareto-pass.md)). `tombstone-pages` (`POST /pages/tombstone`) takes `ids`, separated
  by spaces or commas, and tombstones them all in one write or none: a missing id is a 404 that writes nothing. A page
  offers it with its own id as Delete. Browse offers it on pages (the ghost pages too), media and documents. The
  CLI's `do` and the MCP tools carry it as any action. It is a danger, not the owner's alone, and a revive undoes it.
- **Metrics filed in groups** ([plan 112](docs/plans/112-metrics-as-cards.md)). `file-pages` (`POST /pages/file`)
  files several pages in a category page in one write, made when it is missing; `move` 1 takes them out of their
  other categories, and `from` takes them out of one category ([API.md](API.md#filed)). The CLI's `do` and the MCP
  tools carry it as any action; it is not the owner's alone, as `link` is not.
- **The stream, a random thing and search by kind** ([plan 130](docs/plans/130-the-explore-pass.md)). `GET /days` is
  the stream: the days of the record, the newest first, 30 a page ([API.md](API.md#days) says which days and what a
  card holds). It pages by `limit` and `offset`, as every list. Every read of a page of the stream takes its days as
  one set, not a window: 30 sparse cards can span decades. `random-page` (`GET /pages/random`, `kind`) is a read that
  answers 303 to a thing picked at random, and with `kind=day` to a day. `search` takes `kind` (day, page, person,
  file).
- **Browse** ([plan 117](docs/plans/117-one-browse-view.md), [plan 132](docs/plans/132-browse-the-notion-way.md)). A category is a page, and anything is filed in it by
  `part-of` (D26); the owner's word (2026-10-15) is "category" everywhere, a thing may be in several, and a category
  shows everything filed in it, whatever its type. `GET /browse` (the catalog action `browse`) is one list with two
  filters: the kind of its rows (`kind`: pages, people, media, documents, metrics) and where it looks (`in` a category,
  or `unfiled`); `ghosts`, `deleted` and `media` narrow the pages and the media ([API.md](API.md#browse) has the
  query and the properties). Days are not in Browse: a day is the time axis, found in the Calendar and by
  Search, and it meets the things through its mentions (the owner's word, 2026-10-15); a day page is never filed
  ([D26](docs/decisions/D26-metric-categories.md)). One way files a thing ([plan 131](docs/plans/131-the-pareto-pass.md),
  [plan 132](docs/plans/132-browse-the-notion-way.md)): `file-pages` files a thing and, with `from`, takes it out.
  `create-page` takes an optional `category`: the page is made and filed there in one write, a missing
  category made as `file-pages` makes it, and a day as the category refuses the whole write (`entities_day_page`
  when the day has no page, `links_endpoint_types` when it has one).
  Rejected by the owner: exclusive membership, a manual order and a syntax in the body that files a page.
- **One verb for one meaning** ([plan 114](docs/plans/114-one-way-to-do-a-thing.md)). The catalog's titles say one
  verb for one meaning: New makes a thing, Add adds to one, Remove takes away, Restore brings back. The action names do
  not change.
- **Files** ([keep a file](docs/cookbook/keep-a-file.md), D9). `add-file` (`POST /files`, `lifelog file`) streams
  the original through SHA-256 and drops it; nothing of it is stored. `internal/preview` makes every picture itself —
  from a JPEG, PNG or GIF, or a JPEG sent for a HEIC or a video frame: scaled to 1280 px, turned upright by EXIF,
  re-encoded as a JPEG at quality 80 (lower only when it would pass 1 MB) with no metadata (no GPS). The contract
  allows 1600 px ([D9](docs/decisions/D09-binary-files.md)), which `preview.Check` holds; 1280 px at quality 80 is
  this writer's choice, about half the bytes ([plan 128](docs/plans/128-smaller-pictures.md)). A picture kept
  before stays as it was: a JPEG encoded again loses more, and its original may be gone. An agent sends the path of a local file, which the MCP server reads and sends, or `sha256`, `mime` and the text. A
  file with no `title` takes its original's file name. `![[Title]]` renders the
  picture; `GET /pages/{id}/preview` and `GET /previews?title=` serve it. A photo's day comes from its EXIF
  (`internal/photo`: a JPEG's APP1, read from the original's first megabyte). A HEIC has no day of its own: it takes the
  `day` that the request gives, and with none it has no day and goes in no day page. A photo's position is
  never read ([D21](docs/decisions/D21-location-history.md), [plan 116](docs/plans/116-strip-to-the-core.md)).
  `lifelog file` keeps one file, with `--dry-run`. Unknown capture-local day follows [keep a file](docs/cookbook/keep-a-file.md).
- **`serve` listens on the loopback only.** `life.db` holds health data, and the API is for this machine. The default
  bind is `127.0.0.1:7777`; `--addr` accepts numeric loopback IPs or `localhost` with a numeric port (0 selects an
  ephemeral port, printed at startup), and nothing else. `localhost` binds to `127.0.0.1` without DNS resolution.
  Network requests must name a loopback IP or ASCII case-insensitive `localhost` at the actual listener port, with
  brackets for IPv6. An omitted HTTP port means 80, not the listener's port. Custom hostnames, service-name ports and
  proxy deployments are unsupported. Forwarded headers confer no trust; absolute-form request targets are refused.
  The network guard protects reads, previews and writes; in-process CLI dispatch is unchanged. Browser-origin
  protection inside it rejects every cross-origin write (403 `cross_origin`) and trusts no origin, while it keeps
  same-origin forms and non-browser clients that send no browser origin headers working. The core has no CORS, no
  login, no cookie and no TLS. Another device reaches the API through a program on this machine: the browser through
  `lifelog-web`, behind the owner's proxy ([install](extensions/web/README.md#install)).
  `--with NAME` (repeatable, [plan 147](docs/plans/147-serve-with-an-extension.md)) runs the extension `lifelog-NAME`
  as a child of the server and keeps it running; [the contract of an extension](extensions/README.md#run-by-serve)
  says how. The child gets a free loopback address in `LIFELOG_LISTEN`, and the server merges its catalog, if it serves
  one, into the catalog of the server ([connected apps](#decisions)). An unknown name refuses the command before the
  file opens.
- **A token turns on the bearer check** ([plan 078](docs/plans/078-network-server.md)). `--token-file PATH` (one line
  of at least 32 characters; the command line never carries the secret) or `LIFELOG_TOKEN` gives `serve` a token. A
  token that is shorter, has more than one line, or starts or ends with a space or a tab, refuses the command before the
  listener opens. An empty value of `--token-file` is refused too. With a token, every request must carry
  `Authorization: Bearer <token>`; one without is answered 401 `token_required`, with nothing of the database. The compare takes the same time for every value, and there is no exemption: no install file and no
  login page. The check of the `Host` comes first, so a wrong `Host` is 403 before a missing token is 401. With no
  token there is no check, as on the temporary server of `lifelog NAME`. A token holder is the owner: owner-only
  actions are still refused by `Lifelog-Source: agent:*` or `import:*`, not by the token. The CLI and the MCP server
  with `--url` send `LIFELOG_TOKEN` as a bearer header. The startup line says `(token required)`. `serve --with` gives
  each child `LIFELOG_TOKEN` with the token of the server, or none when the server has none; a value that `serve`
  inherited is removed first.

## Every action on every surface

The catalog (`GET /actions`) is the one list of what `lifelog` can do, and each action of it is reachable on every
surface ([AGENTS.md](AGENTS.md#go-implementation)): the API by its route, the CLI by `do ACTION field=value` (and a
shortcut that wraps it), the MCP server by a tool of the same name with `_` for `-`, and the browser (`lifelog-web`) by
a form. Each thing has one way to do it: one action, one route, one tool, one form where its resource is. A link may
lead to that form, but no second form, field, flag, route or alias does the same. `GET /actions?new=NAME&id=...` fills
the `href` of an action, and `lifelog-web` draws every action from it, the ones with an id or a day too
([plan 131](docs/plans/131-the-pareto-pass.md)). The browser column of the table below describes `lifelog-web`.
The owner's actions are never MCP tools and are refused to an `agent:*` or `import:*` writer.
`TestTheTableOfTheSurfacesIsTheCatalog` keeps this table equal to the catalog, and holds only that each browser cell is
not empty. `extensions/web/web/parity_test.go` holds that `lifelog-web` has a form for each action.
`TestParityOfTheSurfaces` runs the actions on the API and the CLI.

| action | API | CLI | MCP tool | browser |
|---|---|---|---|---|
| `capture` | `POST /days/{day}/capture` | `do capture`, `capture` | `capture` | a day: Add to the day |
| `say` | `POST /days/{day}/say` | `do say` | none, the owner's | every page: the chat column; the rail: Chat |
| `reply` | `POST /days/{day}/reply` | `do reply` | `reply` | the form of `/actions` alone: no page draws it, because the agent writes it |
| `create-page` | `POST /pages` | `do create-page` | `create_page` | Browse, Pages: New page, a button in the head of the list (a category page: New page here) |
| `save-body` | `POST /pages/{id}/body` | `do save-body` | `save_body` | a day or a page: Edit |
| `create-person` | `POST /people` | `do create-person` | `create_person` | Browse, People: New person, a button in the head of the list |
| `add-file` | `POST /files` | `do add-file`, `file` | `add_file` | Browse, Media: New file, a button in the head of the list |
| `set-cover` | `POST /pages/{id}/cover` | `do set-cover`, `cover` | `set_cover` | the picker of a day or a page (`/pages/{id}/cover`) |
| `promote` | `POST /pages/{id}/promote` | `do promote` | `promote` | the ⋯ menu of a page: Make it a person |
| `edit-person` | `POST /pages/{id}/person` | `do edit-person` | `edit_person` | the ⋯ menu of a person: Edit name and dates |
| `edit-metric` | `POST /pages/{id}/metric` | `do edit-metric` | `edit_metric` | a metric: Ask for it every day (the sheet of its view); the ⋯ menu of its page |
| `revive` | `POST /pages/{id}/revive` | `do revive` | `revive` | a deleted page (Browse, Deleted): Restore |
| `tombstone-pages` | `POST /pages/tombstone` | `do tombstone-pages` | `tombstone_pages` | the ⋯ menu of a page: Delete; the pick bar of a Browse list |
| `file-pages` | `POST /pages/file` | `do file-pages` | `file_pages` | a page: + Add and the x of a category badge; a Browse list: Move on a row, and the pick bar (Move to, Add to, Remove from here); the Metrics board: the pick bar |
| `record` | `POST /measurements` | `do record` | `record` | a day: Add a reading; a metric's readings: Add a reading |
| `correct` | `POST /measurements/{id}/correct` | `do correct` | `correct` | a reading: Correct |
| `retract` | `POST /measurements/{id}/retract` | `do retract` | `retract` | the ⋯ menu of a reading: Retract |
| `rename` | `POST /pages/{id}/rename` | `do rename`, `rename` | `rename` | the pencil beside the title of a page |
| `register-metric` | `POST /metrics` | `do register-metric` | none, the owner's | Metrics: New metric, a button in the head of the board |
| `settings` | `POST /settings` | `do settings` | none, the owner's | Settings |
| `integrity-check` | `GET /integrity` | `do integrity-check` | none, the owner's | the sidebar: Integrity checks |
| `search` | `GET /search` | `do search`, `search` | `search` | the sidebar: Search (the search dialog) |
| `browse` | `GET /browse` | `do browse` | `browse` | the sidebar: Browse |
| `find` | `GET /pages` | `do find`, `page` | `find` | a `[[wikilink]]` |
| `open-day` | `GET /days/go` | `do open-day`, `day` | `open_day` | Calendar: Go to a day |
| `random-page` | `GET /pages/random` | `do random-page` | `random_page` | Calendar: A random day; a Browse list: A random one |
| `snapshot` | `POST /snapshots` | `do snapshot`, `snapshot` | none, the owner's | the sidebar: Snapshot |
| `rebuild-search` | `POST /search/rebuild` | `do rebuild-search` | none, the owner's | Integrity checks, the ⋯ menu: Rebuild search |
| `resync-links` | `POST /links/resync` | `do resync-links` | none, the owner's | Integrity checks, the ⋯ menu: Resync links |

This table lists the core alone. The actions of a connected app ([plan 154](docs/plans/154-connected-apps-through-the-catalog.md))
are in the catalog while the app runs, and each is on every surface by its name `NAME-action`:

| surface | an action of the app NAME |
|---|---|
| API | `GET /actions` lists `NAME-action`; the route is `GET` or `POST /with/NAME/...` ([API.md](API.md#connected-apps)) |
| CLI | `do NAME-action field=value --url URL`, over a running `serve` |
| MCP | the tool `NAME_action` over `lifelog mcp --url URL`; none for an owner's action |
| browser | `lifelog-web`: the form on `/actions` and on the pages of the app; the rail has one entry for each app that runs |

One child of `serve --with` may be the **task service** ([plan 158](docs/plans/158-one-task-service-behind-tasks.md)).
It is not an app: it has no `/with/NAME`, no link `app` at the root and no `NAME-` prefix. `lifelog` builds the
entities of Tasks under `/tasks`, and `lifelog-web` draws them (its rail has Tasks). Six actions are in the catalog
while the service runs: `add-task`, `edit-task`, `complete-task`, `reopen-task`, `delete-task` (the owner's) and
`sync-tasks`. Each is on every surface like any action. The routes, the 503 while no service runs and the protocol are
in [API.md](API.md#tasks) and [the contract of an extension](extensions/README.md#a-task-service).

The routes that are not actions are reads of a resource (`GET /`, `/days`, `/days/{day}`, `/weeks/{week}`, `/months/{m}`,
`/years/{y}`, `/days/{day}/conversation`, `/settings`, `/pages/{id}`, `/pages/{id}/cover`, `/metrics`, `/metrics/{name}`, `/measurements/{id}`, `/actions`),
which the CLI's `get` and the MCP tool `get` read.

## Layout

| path | what | test it at |
|---|---|---|
| `schema.go` (root) | `lifelog.Schema`: `docs/schema/schema.sql`, embedded so the binary carries the DDL; `internal/db` reads it | `internal/db` applies it in every test; `tests/` builds from the file itself |
| `cmd/lifelog` | the binary: `init`, `serve`, `get`, `do`, `actions` and the shortcuts (`file`, `snapshot` among them); the table of commands names what each shortcut wraps, and `TestTheTableOfTheSurfacesIsTheCatalog` holds the README table to it; `with.go` is the handshake of `serve --with`, and `testdata/withapp` is the connected app of its end-to-end test | the parity table `TestParityOfTheSurfaces` for a surface; `runArgs` with `testEnv` for a command line in process; `TestExecutableServeDrainsActiveRequest` for the process |
| `internal/db` | open, pragmas, `BEGIN IMMEDIATE`, the embedded schema; `dbtest` makes the fresh file of a test | `db.Init` and `db.Open` on a file in `t.TempDir()` |
| `text` | the title predicate, `title_key`, wikilink and `#tag` extraction, the parse of a day and an instant, the markers that a plain-text line cuts (`BlockMark`) and the marks of a match in a snippet of the search (`SnippetOpen`, `SnippetClose`); tested against the vectors of [titles and wikilinks](docs/contract/titles-and-wikilinks.md) | the pure functions with table tests, and the vectors of the contract page (`TestWikilinkVectors`, `TestTitleKeyVectors`) |
| `internal/core` | the cookbook's writes and reads; the save contract; metrics; renames; files | `core.Store` on `dbtest.New`, with a fixed `Clock` for a test of today |
| `internal/preview` | the picture a file page keeps: decode, scale to 1280 px (the contract allows 1600), EXIF orientation, a JPEG at quality 80 of at most 1 MB with no metadata | `preview.Make` and `preview.Check` on pictures that the test makes |
| `internal/photo` | what a photo's metadata says: the day and time taken and the orientation (a JPEG); `phototest` builds synthetic ones | `photo.Read` on the photos of `phototest`; `FuzzRead` |
| `internal/api` | the action catalog, the routes, Siren JSON and the bytes of a picture, a file or a snapshot; the guard of the loopback authority and the bearer check; `apitest` makes a fresh handler and its clients for a test | `api.NewWith(store, nil)`, through `apitest.Fresh`, or `apitest.FreshOn` for a store with a fixed clock; a test inside package `api` cannot import `apitest` (an import cycle), so it makes its store with `dbtest.New`; the pure functions of the handler (`errorEntity`, `browseQueryOf`) with table tests |
| `siren` | the Siren types, the `Lifelog-Source` header, the error codes, the JSON writer and the addresses of the resources; standard library only; public: the Go extensions import it | the tests of the package and of `internal/api`, which both use it |
| `client` | the hypermedia client, transport only; public: the Go extensions import it; in-process or remote; `DoFiles` sends file parts from readers, and `Do` sends paths through it; of this module it imports only `siren`, so it builds no `core`, no `db` and no SQLite | `client.InProcess` and `client.Remote` on one handler: `TestTheTwoTransportsGiveEqualAnswers` |
| `app` | the package a connected app serves its catalog with: `Catalog`, `Listen`, `Fail`, `StateDir`; public; standard library and `siren`; it serves and never dials | its tests |
| `lifelogtest` | the test kit of the extensions: `lifelogtest.New(t, day)` makes a fresh synthetic file, the real handler over it with a fixed clock, and a server on a loopback socket; public, for `_test.go` files of an extension only | its own test |
| `client/tools` | the catalog as the functions of a model: a tool for each action that is not the owner's, its JSON Schema from the fields, the arguments flattened to form values; public: the MCP server and the chat agent build their lists from it | its table tests |
| `extensions/web` | its own module, `lifelog-web`: the browser; it draws the answers of the API as HTML and owns the login, the lock, the forms and its settings; it imports `lifelog/siren`, `lifelog/client`, `lifelog/app` and `lifelog/text` of the root module (a `replace` directive), and nothing of `lifelog/internal` | the tests of its packages (`go test ./...` in the folder); `e2e_test.go` builds the real `lifelog` and `lifelog-web` and runs `lifelog serve --with web`; `web/parity_test.go` holds a form for each action |
| `extensions/mcp` | its own module, `lifelog-mcp`: the catalog as MCP tools, over HTTP; it imports `lifelog/siren`, `lifelog/client` and `lifelog/client/tools` of the root module (a `replace` directive), and nothing of `lifelog/internal` | `mcp.New` on a fake catalog (`go test ./...` in the folder); `TestTheMCPServerOnTheRealLifelog` builds the real `lifelog` and `lifelog-mcp` and runs `lifelog mcp --agent test` |
| `extensions/agent` | its own module, `lifelog-agent`: the agent; it reads the conversation page, calls a model through the action catalog, and writes the answer on the page as `agent:lifelog`; it imports `lifelog/siren`, `lifelog/client` and `lifelog/client/tools` of the root module (a `replace` directive), and nothing of `lifelog/internal` | the tests of its packages on a fake catalog and a fake model (`go test ./...` in the folder); `e2e_test.go` builds the real `lifelog` and `lifelog-agent` and runs `lifelog serve --with agent` with a fake model server |
| `extensions/whatsapp` | its own module, `lifelog-whatsapp`: the transport between WhatsApp and the conversation page; a message of the owner becomes `say`, and each new turn of the page goes to the phone; it calls no model; it imports `lifelog/siren` and `lifelog/client` of the root module (a `replace` directive), and nothing of `lifelog/internal` | the tests of its packages (`go test ./...` in the folder); `e2e_test.go` builds the real `lifelog`, and runs the relay with a fake chat against the real `lifelog serve` |
| `extensions/ticktick` | its own module, `lifelog-ticktick`: TickTick, the task service ([a task service](extensions/README.md#a-task-service)); it polls the done tasks into the day pages as `import:ticktick` and serves the task protocol (`lifelog` builds the entities of Tasks under `/tasks`, and `lifelog-web` draws them); it imports `lifelog/siren`, `lifelog/client` and `lifelog/app` of the root module (a `replace` directive), and nothing of `lifelog/internal` | the tests of its packages against a fake TickTick (`go test ./...` in the folder); `e2e_test.go` builds the real `lifelog` and runs `lifelog serve --with ticktick` |
| `extensions/agentic` | its own module, `lifelog-agentic`: `lifelog agentic-init`, the texts and MCP configs for a coding agent opened in the folder of the program; it reads `LIFELOG_EXE` and `LIFELOG_DB`, and imports nothing of `lifelog/internal` | `agentic.Files` and `agentic.Init` on the `Paths` of a temporary folder (`go test ./...` in the folder); `e2e_test.go` builds the real `lifelog`, `lifelog-mcp` and `lifelog-agentic-init` |
| `internal/scaletest` | the synthetic personas and the lifetime workload of `tools/lifescale`: generators, an oracle and the timed checks | `scaletest.Run` with the profile `small`, in the baseline |
| `tools/lifescale` | the lifetime and stress runner over `internal/scaletest` ([tests/README.md](tests/README.md)) | `internal/scaletest`: the runner has no test of its own |
| `tests` | the validation suites of the docs ([tests/README.md](tests/README.md)) | the contract suites `TestSuites` and the mutants `TestMutants`, on the DDL of `docs/` |

## Checks

`go vet ./... && go test ./...` — every test builds throwaway databases in a temporary folder:
the packages from the embedded schema (`schema.go`), the suites in `tests/` from `docs/schema/schema.sql` itself (and from broken
copies of the docs, the mutants). `go test -short ./...` skips the mutants.

An extension is checked in its own folder with `go vet ./... && go test ./...`. The `./...` of the root does not build
it, so it does not run the tests of the browser, which are in `extensions/web`.

### Request metadata limits

Action JSON and URL-encoded bodies are limited to 20 MiB of encoded data; JSON
must contain exactly one object. Multipart uploads allow up to 64 parts, 16 MiB
per text field and 16 MiB total text (including repeated fields; an unknown name is refused (422) before its bytes are read),
and at most one original and one preview part. A supplied preview may be up to
64 MiB. Over-limit requests return HTTP 413 without writing; after a JSON or
URL-encoded body over the limit, the server also closes an HTTP/1.1 connection (over
HTTP/2 it resets the stream, and the connection stays open). Originals are hashed as streams without a total size limit; originals above the 64 MiB picture
budget are kept without an automatically generated preview.

## Extensions

An extension is a client of the HTTP API ([API.md](API.md)). It lives in its own folder with its own Go module. It is
not built into `lifelog`, and `go build ./...` here does not build it.

The command line `lifelog NAME [ARGS...]` runs the program `lifelog-NAME`, found beside `lifelog` or on `PATH`. It gives
the program `LIFELOG_URL` (the API of the file), `LIFELOG_EXE` and `LIFELOG_DB`. The contract, for an author, is
[`extensions/README.md`](extensions/README.md).

- [`extensions/web`](extensions/web/README.md): the browser, `lifelog serve --token-file F --with web` (or `lifelog web`
  with `LIFELOG_TOKEN` set). It draws the answers of the API as pages of HTML and owns the login, the lock and the
  forms. Its door is `LIFELOG_WEB_LISTEN` (`127.0.0.1:7780`).
- [`extensions/googlephotos`](extensions/googlephotos/README.md): a local page where the owner picks, for each day
  that has no cover, one photo of that day from Google Photos.
- [`extensions/mcp`](extensions/mcp/README.md): the MCP server, `lifelog mcp [--agent NAME]`. Every action of the
  catalog that is not the owner's is a tool.
- [`extensions/agentic`](extensions/agentic/README.md): `lifelog agentic-init [--force]`. It writes `AGENTS.md`, the
  skills and the MCP configs for a coding agent opened in the folder of the program.
- [`extensions/agent`](extensions/agent/README.md): the agent that answers, `lifelog serve --with agent`. It reads the
  conversation page, calls a model at an OpenAI-compatible endpoint, and writes through the catalog as `agent:lifelog`.
  The chat of the browser and the chat of WhatsApp both get their answer from it.
- [`extensions/whatsapp`](extensions/whatsapp/README.md): the WhatsApp transport, `lifelog serve --with whatsapp`. A
  message from the owner's number becomes `say` with `via=whatsapp`, and each new turn of the conversation goes to the
  phone. It calls no model.
- [`extensions/ticktick`](extensions/ticktick/README.md): TickTick, the task service. What you finish in
  TickTick comes back as a `Done:` entry of its day, once. It is the task service:
  `lifelog` builds the entities of Tasks under `/tasks`, and `lifelog-web` draws them. `add-task` and the other actions
  of [Tasks](API.md#tasks) are in the catalog. Run it with `lifelog serve --with ticktick`.

## Engineering references

These primary sources inform the application policies of [AGENTS.md](AGENTS.md); they do not override this
repository's database contract or turn optional techniques into universal Go rules.

- Go: [review comments](https://go.dev/wiki/CodeReviewComments), [module layout](https://go.dev/doc/modules/layout),
  [error wrapping and API boundaries](https://go.dev/blog/go1.13-errors), [context](https://pkg.go.dev/context).
- Testing: [test review comments](https://go.dev/wiki/TestComments), [testing APIs](https://pkg.go.dev/testing),
  [fuzzing](https://go.dev/doc/security/fuzz/), [synctest](https://pkg.go.dev/testing/synctest),
  [race detector and prerequisites](https://go.dev/doc/articles/race_detector).
- Tools: [Staticcheck](https://staticcheck.dev/docs/),
  [govulncheck and its limitations](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck).
- Measurement: [benchmark loops](https://go.dev/blog/testing-b-loop),
  [benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat), [profiling](https://go.dev/doc/diagnostics),
  [SQLite performance methodology](https://sqlite.org/cpu.html), [query-plan caveats](https://sqlite.org/eqp.html).
- Model-based testing: [stateful reference-model example](https://hypothesis.readthedocs.io/en/latest/stateful.html)
  (the technique, not a Python dependency).

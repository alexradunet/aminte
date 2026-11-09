# Aminte

`aminte` is one Go binary that keeps a lifetime-scale, single-user SQLite database, `life.db` — a life log and
its backup: a journal of day pages, notes, the people in them, health readings, and the
files you keep — a recording's transcript, a PDF's text, a photo's small picture. It serves the file two ways: a
hypermedia API and a CLI. The browser is an extension, `aminte-web`: a client of the API, like the MCP server. Every
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

`aminte` implements the contract and never defines it: a rule found missing or ambiguous here is an
[issue](docs/issues/README.md) against the docs, not a fix in this code alone.

```
go build -o aminte ./cmd/aminte   # Go >= 1.27, no cgo
for d in extensions/*/; do go build -C "$d" -o "$PWD/" ./cmd/...; done   # every extension, beside aminte: aminte-web, aminte-mcp, aminte-ticktick, ...
aminte init                               # life.db beside the program; --db PATH or $AMINTE_DB names another
aminte agentic-init                       # AGENTS.md, skills and MCP configs here, for a coding agent opened here

aminte capture "Ran 5k with [[Sam]] #running" --mood 4
aminte day --human
mkdir -p ~/life && (umask 077; head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n' > ~/life/token)   # a token file: 64 characters
aminte serve                              # the API on http://127.0.0.1:7777: Siren JSON, this machine only
aminte serve --token-file ~/life/token --with web   # the browser too, on http://127.0.0.1:7780: log in with the token
aminte mcp --agent lmstudio               # MCP on stdio (the extension aminte-mcp); rows are written as agent:lmstudio
aminte snapshot --snapshots ~/snapshots --human  # life-YYYY-MM-DD.db and its restore check
aminte file memo.m4a --text memo.txt     # keep a file: its text, the original hashed and never stored
aminte file IMG_0001.HEIC --preview IMG_0001.jpg --day 2026-10-05   # a HEIC: aminte reads neither its picture nor its day; send a JPEG and the day
aminte cover 12 lake.jpg                  # a new photo as page 12's cover; `aminte cover 12 --title Lake.jpg` takes a kept one
```

A token turns on the bearer check of `serve`. It is one line of 32 characters or more, and it must not start or end with
a space or a tab. `aminte-web` asks the owner for the same token at its login page. The address `http://127.0.0.1:7777`
answers Siren JSON and no page ([API.md](API.md#reaching-a-server) names the plain answers of `net/http` that are not
Siren). On Windows, in PowerShell 7.2 or later, these two lines make the token file:

```
New-Item -ItemType Directory -Force $HOME\life | Out-Null
[Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(32)) | Set-Content -NoNewline $HOME\life\token
```

To run `aminte serve` as a system service with a nightly snapshot, see [deploy/](deploy/README.md): the unit files
and the snapshot script, as a reference to compare with the installed copies before you replace them. The reference
unit runs `serve ... --with web`, so it needs `aminte-web` beside `aminte`
([ship a change](deploy/README.md#ship-a-change)).

An import is done by a program that loads rows itself ([imports](docs/contract/imports.md)), or by a coding agent
with the owner, in a conversation: the agent reads a source with its own tools and writes through the catalog's MCP
tools, and the owner says yes or no to each person ([plan 089](docs/plans/089-no-import-process.md)).
`aminte agentic-init` prepares the folder for it ([plan 090](docs/plans/090-agentic-init.md)); then open Claude
Code, Codex, OpenCode or Pi in the folder.

```
aminte snapshot                                   # the owner, before an import session
```

## Decisions

These are this application's own engineering decisions ([D14](docs/decisions/D14-ui-and-tools.md)). Each one says
what was decided and why. The behaviour is in the page that the link names, and only there.

- **The four rings** ([plan 135](docs/plans/135-extensions-outside-the-core.md)). A thing lives in the innermost ring
  that can hold it: the file, the writer, the catalog with its surfaces, then the extensions, so the core stays small and
  names no third party. [AGENTS.md](AGENTS.md#the-four-rings) draws the rings once and names the tests that keep them.
  The browser, `aminte-web`, is an extension by the owner's word of 2026-10-31
  ([plan 161](docs/plans/161-the-browser-as-an-extension.md)).
- **Connected apps through the catalog** ([plan 154](docs/plans/154-connected-apps-through-the-catalog.md)). The
  interface between `aminte` and an app is the Siren catalog and nothing else. So the CLI's `do`, the MCP tools and
  the browser carry an app with no further work, and the code of a third party stays in the connector. The protocol is
  in [the contract of an extension](extensions/README.md#a-catalog-of-its-own), and the wire shape in
  [API.md](API.md#connected-apps).
- **One task service behind `/tasks`** ([plan 158](docs/plans/158-one-task-service-behind-tasks.md)). One child of
  `serve --with` may be the task service. `aminte` builds the entities of Tasks, so every client draws one kind of
  page whatever task manager stands behind it, and the file keeps no task ([D23](docs/decisions/D23-no-tasks.md)). The
  routes are in [API.md](API.md#tasks), and the protocol in
  [the contract of an extension](extensions/README.md#a-task-service).
- **One handler, three surfaces.** `internal/api` is the only thing the surfaces call. `serve` mounts it on a socket;
  the CLI calls it in-process, or a running server with `--url`. The MCP server, `aminte-mcp`
  ([extensions/mcp](extensions/mcp/README.md)), and the browser, `aminte-web`
  ([extensions/web](extensions/web/README.md)), are clients of the HTTP API. Writes go through `internal/core` only:
  no generic UPDATE or DELETE exists anywhere.
- **Server shutdown drains admitted requests.** An interrupt or a SIGTERM closes the listener, then allows ten seconds
  for active requests to finish before closing their connections. Shutdown completes before the database closes;
  expiration is reported as an error.
- **Hypermedia (HATEOAS).** Every response is a Siren entity: properties, links, and the actions legal on that
  resource, with their fields prefilled. `GET /actions` is the catalog, and the CLI's `do` and the MCP tools are
  generated from it, so a new action needs no client change
  ([plan 131](docs/plans/131-the-pareto-pass.md)). The entity, the errors with their codes, the lists with `limit`
  and `offset`, and `GET /actions?new=` are in [API.md](API.md).
- **Daily use** ([plan 091](docs/plans/091-the-browser-for-daily-use.md),
  [plan 097](docs/plans/097-the-almanac-reflects.md)). The root is the entry point of the API, with the five newest
  day pages. A day carries its mood and the `save-body` of its day page, so a client edits in place; a date field reads
  the word `today` as the server's local day. The pages are in API.md: the [root](API.md#root), the [day](API.md#day),
  the [month](API.md#month) and the [year](API.md#year).
- **Period pages** ([D30](docs/decisions/D30-period-pages.md)). A week (`2026-W43`), a month, a quarter and a year may
  have a page: a plain page whose title is the key. A period with no page offers `create-page` with its key as the
  title, the one action that makes it ([API.md](API.md#week)).
- **A cover for every page** ([plan 127](docs/plans/127-a-cover-for-every-page.md)). A page's cover is the first live
  file with a preview that its body embeds. The schema has no link for it, by the owner's choice (2026-10-15, again
  2026-10-17): an embed is how any page shows a picture, so every surface can set it with `set-cover`. The rules and the
  picker are in [API.md](API.md#page-person-file).
- **Provenance per surface.** `source` is `cli`, `api`, `ui` (`aminte-web`, which sends `Aminte-Source: ui`) or
  `agent:<name>` (MCP). The header `Aminte-Source` sets it, and a request with no header writes as `api`: there is no
  default `ui` ([API.md](API.md#reaching-a-server)).
- **Optimistic saves.** A body save sends the `version` (the decimal `entities.revision` token) it read; a newer one
  is a 409.
- **The readings of a metric** ([plan 100](docs/plans/100-one-page-a-metric.md),
  [plan 131](docs/plans/131-the-pareto-pass.md), [plan 144](docs/plans/144-one-view-for-a-metric.md)). A metric has one
  view, `/metrics/{name}`: its page (a metric is a page, D27) and its readings. One answer carries both, so the API,
  the CLI and the MCP tools need no second read ([API.md](API.md#series)).
- **Connections** ([connection setup](docs/contract/connections.md)): one writing connection with the pragmas
  set by DSN and read back by a connection hook (a wrong value refuses the connection), `SQLITE_DBCONFIG_DEFENSIVE`,
  `BEGIN IMMEDIATE` via `_txlock=immediate`, the 3.51.3 floor checked; readers use `mode=ro` with
  `trusted_schema=OFF`, read back the same way.
- **Unicode.** Registry name keys use `x/text`'s full case fold; the `Cn` rule uses a pinned Unicode 15.0 assigned
  table, not the tables Go ships, so the titles this writer accepts do not change with a Go upgrade.
- **One transaction type.** Every write is a method on `core.Tx`; an operation alone is one transaction, and an
  operation that writes several rows (`file-pages`) runs several of the same methods in one. Nothing writes
  around them.
- **The folder of the program** ([plan 090](docs/plans/090-agentic-init.md)). Without `--db`, `AMINTE_DB` or
  `--url`, a command uses the `life.db` beside the executable, and `aminte init` with no path makes it there; a
  missing file is still refused. `aminte agentic-init` is a setup command of the machine, not a catalog action: it
  writes files beside the program for a coding agent ([extensions/agentic](extensions/agentic/README.md)).
- **Owner-only actions.** An action that the catalog marks `owner` is the owner's alone, because an agent must not, for
  example, hold the write lock (`integrity-check`).
  [API.md](API.md#reaching-a-server) holds the refusal and the owner reads that are not actions, and
  `aminte actions --human` marks each owner action.
- **No conversation inside the writer** ([plan 116](docs/plans/116-strip-to-the-core.md),
  [RFC 0015](docs/rfcs/0015-no-conversation.md), [RFC 0026](docs/rfcs/0026-no-agent-inside-and-no-whatsapp.md)). The
  owner's decisions (2026-10-15, 2026-11-09): no model, no chat, no transport of a messaging app. The core and its
  extensions call no model. An AI agent works with `aminte` from the outside, through the CLI (`--source agent:NAME`)
  or the MCP server (`aminte mcp --agent NAME`), as the writer `agent:NAME`. A recording is kept as a file with its
  text ([D9](docs/decisions/D09-binary-files.md)).
- **Renames select a preferred name on the same identity**, retaining direct aliases as
  [titles and wikilinks](docs/contract/titles-and-wikilinks.md) ("Renames") requires and
  [rename a page](docs/cookbook/rename-a-page.md) writes; `aminte rename` and the API's `rename` action run it.
- **The schema is embedded.** `schema.go` at the root embeds `docs/schema/schema.sql`, and `internal/db` reads it; a
  change to the DDL is live in the binary at the next build. `aminte init` refuses an existing file; `Open` refuses a
  file without Aminte's `application_id`.
- **Snapshots are the owner's, from any surface** ([take a snapshot](docs/cookbook/take-a-snapshot.md),
  [plan 082](docs/plans/082-snapshot-in-the-catalog.md)). `snapshot` takes no path: the file lands in the folder that
  `serve --snapshots DIR` names, or beside `life.db`, named by the local day and never overwritten. `aminte snapshot
  [--snapshots DIR]` runs the action in-process, and with `--url` the server's folder is the one. The action and the
  download are in [API.md](API.md#snapshot), and restoring stays the manual procedure of the cookbook.
- **One delete, of one row or several** ([plan 107](docs/plans/107-delete-several-at-once.md),
  [plan 131](docs/plans/131-the-pareto-pass.md)). `tombstone-pages` takes `ids` and tombstones them all in one write or
  none ([API.md](API.md#tombstoned)). It is a danger, not the owner's alone, and a revive undoes it.
- **Metrics filed in groups** ([plan 112](docs/plans/112-metrics-as-cards.md)). `file-pages` files several pages in a
  category page in one write; it is not the owner's alone, as `link` is not ([API.md](API.md#filed)).
- **The stream, a random thing and search by kind** ([plan 130](docs/plans/130-the-explore-pass.md)). `GET /days` is
  the stream: the days of the record, the newest first. Every read of a page of the stream takes its days as one set,
  not a window, because 30 sparse cards can span decades. `random-page` answers 303 to a thing picked at random, and
  `search` takes `kind` ([API.md](API.md#days)).
- **Browse** ([plan 117](docs/plans/117-one-browse-view.md), [plan 132](docs/plans/132-browse-the-notion-way.md)). A
  category (the owner's word, 2026-10-15) is a page, and anything is filed in it by `part-of` (D26), so a thing may be
  in several categories and a category shows everything filed in it. Days are not in Browse: a day is the time axis,
  found in the Calendar and by Search, and a day page is never filed ([D26](docs/decisions/D26-metric-categories.md)).
  The owner rejected exclusive membership, a manual order and a syntax in the body that files a page; the query is in
  [API.md](API.md#browse).
- **One verb for one meaning** ([plan 114](docs/plans/114-one-way-to-do-a-thing.md)). The catalog's titles say one
  verb for one meaning: New makes a thing, Add adds to one, Remove takes away, Restore brings back. The action names do
  not change.
- **Files** ([keep a file](docs/cookbook/keep-a-file.md), D9). `add-file` streams the original through SHA-256 and
  drops it, so nothing of it is stored. `internal/preview` makes every picture itself, with no metadata (no GPS), at
  1280 px and quality 80: about half the bytes of the 1600 px that [D9](docs/decisions/D09-binary-files.md) allows
  ([plan 128](docs/plans/128-smaller-pictures.md)). A photo's day comes from its EXIF, a HEIC takes the `day` that the
  request gives or none, and a position is never read ([D21](docs/decisions/D21-location-history.md),
  [plan 116](docs/plans/116-strip-to-the-core.md)).
- **`serve` listens on the loopback only**, because `life.db` holds health data and the API is for this machine. The
  core has no CORS, no login, no cookie and no TLS: another device reaches the API through a program on this machine,
  such as `aminte-web` behind the owner's proxy ([install](extensions/web/README.md#install)). The rules of the address,
  the `Host` check and the cross-origin refusal are in [API.md](API.md#reaching-a-server).
- **`serve --with NAME`** ([plan 147](docs/plans/147-serve-with-an-extension.md)) runs the extension `aminte-NAME` as a
  child of the server and keeps it running ([the contract of an extension](extensions/README.md#run-by-serve)). An
  unknown name refuses the command before the file opens.
- **A token turns on the bearer check** ([plan 078](docs/plans/078-network-server.md)). `--token-file PATH` (the
  command line never carries the secret) or `AMINTE_TOKEN` gives `serve` a token, and a token that is too short,
  has more than one line, or starts or ends with a space or a tab refuses the command before the listener opens. A
  token holder is the owner. The CLI and the MCP server with `--url` send `AMINTE_TOKEN` as a bearer header, and
  `serve --with` gives each child the token of the server, or none when the server has none
  ([API.md](API.md#reaching-a-server)).

## Every action on every surface

The catalog (`GET /actions`) is the one list of what `aminte` can do, and each action of it is on every surface
([AGENTS.md](AGENTS.md#go-implementation)). The route is the `href` of the action. The CLI line is
`aminte do NAME field=value`, and a shortcut such as `aminte capture` wraps one action and adds nothing. The MCP tool
is the name with `_` in place of `-`, and an owner action has no tool. The browser, `aminte-web`, draws the form of the
action where its resource is. An action of a connected app or of the task service follows the same rule while it runs
(`NAME-action` and `NAME_action` for an app). No page keeps a copy of the list: `aminte actions --human` prints each
action with its route, its CLI line, its MCP tool, its owner mark and its shortcut. `TestParityOfTheSurfaces` runs one
row for each thing that a surface adapts (a shortcut, the word `today`, a flag, the refusals, the owner and the agent)
on the API and the CLI. The generic `do` is one path for every action, so no row repeats it for each action.
`extensions/web/web/parity_test.go` holds that `aminte-web` has a form for each action. A route that is not an action is a read of a resource, which the CLI's `get` and the MCP tool `get` read.

```
aminte actions --human
```

## Layout

| path | what | test it at |
|---|---|---|
| `schema.go` (root) | `aminte.Schema`: `docs/schema/schema.sql`, embedded so the binary carries the DDL; `internal/db` reads it | `internal/db` applies it in every test; `tests/` builds from the file itself |
| `cmd/aminte` | the binary: `init`, `serve`, `get`, `do`, `actions` and the shortcuts (`file`, `snapshot` among them); the table of commands names what each shortcut wraps, and `aminte actions --human` prints it; `with.go` is the handshake of `serve --with`, and `testdata/withapp` is the connected app of its end-to-end test | the parity table `TestParityOfTheSurfaces` for a surface; `TestActionsHumanShowsTheSurfacesOfEachAction` for the listing of the surfaces; `runArgs` with `testEnv` for a command line in process; `TestExecutableServeDrainsActiveRequest` for the process |
| `internal/db` | open, pragmas, `BEGIN IMMEDIATE`, the embedded schema; `dbtest` makes the fresh file of a test | `db.Init` and `db.Open` on a file in `t.TempDir()` |
| `text` | the title predicate, `title_key`, wikilink and `#tag` extraction, the parse of a day and an instant, the markers that a plain-text line cuts (`BlockMark`) and the marks of a match in a snippet of the search (`SnippetOpen`, `SnippetClose`); tested against the vectors of [titles and wikilinks](docs/contract/titles-and-wikilinks.md) | the pure functions with table tests, and the vectors of the contract page (`TestWikilinkVectors`, `TestTitleKeyVectors`) |
| `internal/core` | the cookbook's writes and reads; the save contract; metrics; renames; files | `core.Store` on `dbtest.New`, with a fixed `Clock` for a test of today |
| `internal/preview` | the picture a file page keeps: decode, scale to 1280 px (the contract allows 1600), EXIF orientation, a JPEG at quality 80 of at most 1 MB with no metadata | `preview.Make` and `preview.Check` on pictures that the test makes |
| `internal/photo` | what a photo's metadata says: the day and time taken and the orientation (a JPEG); `phototest` builds synthetic ones | `photo.Read` on the photos of `phototest`; `FuzzRead` |
| `internal/api` | the action catalog, the routes, Siren JSON and the bytes of a picture, a file or a snapshot; the guard of the loopback authority and the bearer check; `apitest` makes a fresh handler and its clients for a test | `api.NewWith(store, nil)`, through `apitest.Fresh`, or `apitest.FreshOn` for a store with a fixed clock; a test inside package `api` cannot import `apitest` (an import cycle), so it makes its store with `dbtest.New`; the pure functions of the handler (`errorEntity`, `browseQueryOf`) with table tests |
| `siren` | the Siren types, the `Aminte-Source` header, the error codes, the JSON writer and the addresses of the resources; standard library only; public: the Go extensions import it | the tests of the package and of `internal/api`, which both use it |
| `client` | the hypermedia client, transport only; public: the Go extensions import it; in-process or remote; `DoFiles` sends file parts from readers, and `Do` sends paths through it; of this module it imports only `siren`, so it builds no `core`, no `db` and no SQLite | `client.InProcess` and `client.Remote` on one handler: `TestTheTwoTransportsGiveEqualAnswers` |
| `app` | the helpers of a Go extension: the shell of a program (`Main`, `API`, `OwnerActs`, `ExitCode`) and what a catalog needs (`Catalog`, `Listen`, `Entity`, `Fail`, `StateDir`); public; standard library, `siren` and `client`; it serves, and it dials only in `program.go`, through `API` | its tests |
| `amintetest` | the test kit of the extensions: `amintetest.New(t, day)` makes a fresh synthetic file, the real handler over it with a fixed clock, and a server on a loopback socket; public, for `_test.go` files of an extension only | its own test |
| `client/tools` | the catalog as the functions of a model: a tool for each action that is not the owner's, its JSON Schema from the fields, the arguments flattened to form values; public: the MCP server builds its list from it | its table tests |
| `extensions/*` | one module each, the program `aminte-NAME`: a client of [API.md](API.md) that imports the public packages of the root module (`siren`, `client`, `client/tools`, `app`, `text`) and nothing of `aminte/internal` ([the contract of an extension](extensions/README.md#what-an-extension-is)); the exception is `extensions/agentic`, a setup extension that is no client and imports nothing from `aminte`; [the table of the extensions](extensions/README.md#the-extensions) names each one | the tests of its packages (`go vet ./... && go test ./...` in its folder); the `e2e_test.go` of an extension builds the real `aminte` and the program and runs the real line; the exception is `extensions/googlephotos`, whose `review/e2e_test.go` builds the real `aminte` and serves it, and runs the review page over it; `extensions/web/web/parity_test.go` holds a form for each action |
| `internal/scaletest` | the synthetic personas and the lifetime workload of `tools/lifescale`: generators, an oracle and the timed checks | `scaletest.Run` with the profile `small`, in the baseline |
| `tools/lifescale` | the lifetime and stress runner over `internal/scaletest` ([tests/README.md](tests/README.md)) | `internal/scaletest`: the runner has no test of its own |
| `tests` | the validation suites of the docs ([tests/README.md](tests/README.md)) | the contract suites `TestSuites` and the mutants `TestMutants`, on the DDL of `docs/` |

## Checks

`go vet ./... && go test ./...` — every test builds throwaway databases in a temporary folder:
the packages from the embedded schema (`schema.go`), the suites in `tests/` from `docs/schema/schema.sql` itself (and from broken
copies of the docs, the mutants). `go test -short ./...` skips the mutants.

An extension is checked in its own folder with `go vet ./... && go test ./...`. The `./...` of the root does not build
it, so it does not run the tests of the browser, which are in `extensions/web`.

## Extensions

An extension is a client of the HTTP API ([API.md](API.md)). It lives in its own folder with its own Go module. It is
not built into `aminte`, and `go build ./...` here does not build it.

The command line `aminte NAME [ARGS...]` runs the program `aminte-NAME`, found beside `aminte` or on `PATH`. It gives
the program `AMINTE_URL` (the API of the file), `AMINTE_EXE` and `AMINTE_DB`. The contract, for an author, is
[`extensions/README.md`](extensions/README.md).

- [`extensions/web`](extensions/web/README.md): the browser, `aminte serve --token-file F --with web` (or `aminte web`
  with `AMINTE_TOKEN` set). It draws the answers of the API as pages of HTML and owns the login, the lock and the
  forms. Its door is `AMINTE_WEB_LISTEN` (`127.0.0.1:7780`).
- [`extensions/googlephotos`](extensions/googlephotos/README.md): a local page where the owner picks, for each day
  that has no cover, one photo of that day from Google Photos.
- [`extensions/mcp`](extensions/mcp/README.md): the MCP server, `aminte mcp [--agent NAME]`. Every action of the
  catalog that is not the owner's is a tool.
- [`extensions/agentic`](extensions/agentic/README.md): `aminte agentic-init [--force]`. It writes `AGENTS.md`, the
  skills and the MCP configs for a coding agent opened in the folder of the program.
- [`extensions/ticktick`](extensions/ticktick/README.md): TickTick, the task service. What you finish in
  TickTick comes back as a `Done:` entry of its day, once. It is the task service:
  `aminte` builds the entities of Tasks under `/tasks`, and `aminte-web` draws them. `add-task` and the other actions
  of [Tasks](API.md#tasks) are in the catalog. Run it with `aminte serve --with ticktick`.

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

# 135 — Extensions outside the core: what names a third party is a subprocess found by name

> Dated implementation record. Tests use temporary folders and synthetic data. The owner's live `life.db` is never
> opened by a test.

## Status

- **Date / baseline:** 2026-10-19, `7637f66` (master).
- **Priority:** P2. **Effort:** L. **Risk:** MEDIUM. The contract in `docs/` and `schema.sql` do not change. The
  `aminte` binary loses two commands (`agentic-init`, `mcp`), which come back as extensions under the same command
  lines. The README, `AGENTS.md`, the site and the parity tests change.
- **Status:** IN REVIEW (owner). Built in `193f2f8` (step 1), `14fa315` (step 3), `ab8c1ca` (step 2) and `f3edb90`
  (step 4); the counter-review's findings are fixed in the commit after it: an extension never guesses an address
  (a missing file gave `aminte mcp` no `AMINTE_URL`, and the fallback `127.0.0.1:7777` was the owner's service), a
  clean interrupt is no error, a stop error is said, `--` protects the extension's words, a file beside the program
  that cannot run is refused, and the dial test refuses an aliased import. The install of the three binaries and the
  restart of the service wait for the owner.
- **Resolves:** no issue, no RFC. The owner's words are the incident.

## The owner's words (2026-10-19)

"I would like to further modularize the application into extensions, so that we keep the core clean and minimal and
then extensions would handle most of the extending." And: "I want to keep the core and the domain model and business
logic minimal." The owner asked for the perfect version, decided case by case, and asked that the browser stay in
the core until an extension proves the model.

## The four rings

A thing lives in the innermost ring that can hold it.

```mermaid
flowchart TB
  subgraph R0["ring 0: the file"]
    S["schema.sql: the tables, CHECKs, triggers and aminte_meta. The domain model and every rule about the data."]
  end
  subgraph R1["ring 1: the writer, in Go"]
    C["internal/core, db, text, photo, preview: transactions, and only what SQLite cannot say (wikilinks, calendars, zones, the title predicate, previews, 404 and 409)"]
  end
  subgraph R2["ring 2: the catalog and its surfaces"]
    K["internal/api and cmd/aminte: one catalog of actions, one Siren handler, the forms, the CLI's do, generated from the catalog"]
  end
  subgraph R3["ring 3: extensions, in any language"]
    X["extensions/NAME: a binary aminte-NAME, a client of API.md, found by name. It names the third party; the core never does."]
  end
  S --> C --> K --> X
```

The test that decides the ring:

```mermaid
flowchart TD
  Q["does it open life.db, or run inside a transaction?"] -->|"yes"| C["ring 0 or 1: the file, the writer"]
  Q -->|"no"| R["does it draw or dispatch the catalog on the same origin as the API?"]
  R -->|"yes"| K["ring 2: the forms, the CLI"]
  R -->|"no"| E["ring 3: an extension"]
```

## What moves, what stays

| what | today | after | why |
|---|---|---|---|
| `aminte agentic-init` | `internal/agentic`, a command of `cmd/aminte` | `extensions/agentic`, the binary `aminte-agentic-init`, run as `aminte agentic-init` | It names four agent frameworks whose formats churn. It opens no database. |
| `aminte mcp` | `internal/mcp`, a command of `cmd/aminte`, the dependency `modelcontextprotocol/go-sdk` | `extensions/mcp`, the binary `aminte-mcp`, run as `aminte mcp` | It is a third-party protocol with its own SDK, generated from the catalog, which is reachable over HTTP. The core's dependencies become SQLite, CommonMark and `x/text`. |
| importers | none in the core | a rule: an importer is an extension, an `import:<name>` writer of the API | The contract already says so ([imports](../contract/imports.md)). Google Photos is one ([plan 134](134-a-cover-from-google-photos.md)). |
| the browser (`internal/api/view`) | in the core | in the core | It draws the entity on the same origin as the API, with the forms that post to it and the login cookie. A renderer in front of the API would need a proxy and a second process for daily use, and the gain is small: the view adds presentation, not logic, and goldmark stays for extraction. The owner decides again after an extension proves the model. |
| `--public`, the login | in the core | in the core | A separate decision about the local proxy, with its own plan. |
| `internal/scaletest`, `tools/lifescale` | development tooling | unchanged | Not an extension question. |
| snapshots, `integrity-check`, `rebuild-search`, `resync-links` | the core | the core | They need the file. |

## The one way to run an extension: `aminte NAME`

```mermaid
flowchart LR
  U["aminte NAME ARGS..."] --> B{"NAME is a command of the table?"}
  B -->|"yes"| R["run it, as today"]
  B -->|"no"| F["find aminte-NAME: the folder of the program first, then PATH"]
  F -->|"none"| N["refuse: unknown command NAME, no aminte-NAME beside this program or on PATH"]
  F -->|"found"| D{"--url given?"}
  D -->|"yes"| X["run aminte-NAME with AMINTE_URL = --url"]
  D -->|"no, and the file exists"| T["open the file, serve it on 127.0.0.1:0 for the life of the extension, AMINTE_URL = that address"]
  D -->|"no, and no file"| Z["run it with no AMINTE_URL: a setup extension needs none"]
  T --> X
  Z --> X
  X --> W["wait for it; then stop the server, close the file; exit with its status"]
```

The rules, which `extensions/README.md` states for an author:

- **The name.** `aminte NAME` runs `aminte-NAME` (`aminte-NAME.exe` on Windows) from the folder of the program
  (symbolic links resolved), else from `PATH` (`exec.LookPath`). The folder of the program comes first, so the installed
  extension wins over one on `PATH`. A program on `PATH` runs with the owner's rights, as a `git-NAME` does.
- **The line.** Before `NAME`, `aminte` reads its global flags as today. After `NAME`, `aminte` reads only `--db`
  and `--url` (as `--db X` or `--db=X`); every other word goes to the extension as it is, in its order. So an
  extension may have a flag of any name (`--addr`, `--title`), and the MCP configs that `agentic-init` writes stay
  `<exe> mcp --agent NAME --db FILE`. `--source` and `--human` are refused with an extension: an extension names its
  own writer and prints its own output.
- **The server.** With `--url`, `AMINTE_URL` is that URL. Without it, when the file exists, `aminte` opens the file
  as every command does and serves it on `127.0.0.1:0` behind the loopback authority (`api.NetworkAuthority`, as
  `serve` does) for the life of the extension, and `AMINTE_URL` is that address. When the file does not exist,
  `aminte` starts no server and sets no `AMINTE_URL`: a setup extension (`agentic-init`) needs none, and a client
  extension refuses with the line that says what to run. The temporary server is the same writer as the CLI, which
  [connections](../contract/connections.md) allows beside the service.
- **The environment.** `AMINTE_URL` (as above), `AMINTE_EXE` (the program, symbolic links resolved), `AMINTE_DB`
  (the file the line names, absolute, whether it exists or not), and `AMINTE_TOKEN` as inherited. Stdin, stdout and
  stderr are inherited, so an MCP server on stdio works. The exit status of `aminte` is the extension's.
- **Signals.** The extension is in the process group of `aminte`, so an interrupt reaches both. `aminte` waits for
  the extension (`exec.CommandContext` with `Cancel` sending an interrupt on Unix, the default on Windows, and
  `WaitDelay` of ten seconds), then stops the server and closes the file. No goroutine outlives the command.
- **What an extension is.** A client of [API.md](../../API.md), in any language, in its own folder under
  `extensions/` with its own module. It never imports `aminte/internal` (a nested Go module cannot), and never opens
  `life.db`: one writer per file. It writes as `import:<name>` (an importer) or `agent:<name>` (a model), so the
  owner-only actions are refused to it by the existing rule. A page it serves listens on a loopback address only.
  It keeps its own state (a token, a cursor) in its own file under the user's configuration folder, never in
  `life.db`. In Go it uses the standard library unless a protocol needs an SDK. Its tests use synthetic data, fake
  the third party, and build the real `aminte` for an end-to-end test, as Google Photos does.
- **No shared client library.** Google Photos wrote its Siren client in about a hundred lines; `aminte-mcp` copies
  what it needs of `siren` and `client`. A public Go client would be a versioned surface to maintain across modules;
  a third Go extension decides whether it earns one.

## The extensions after this plan

| folder | binary | command line | writes as | needs the server |
|---|---|---|---|---|
| `extensions/googlephotos` | `aminte-gphotos` | `aminte gphotos [--addr A] [--credentials F] [--zone Z]` | `import:google-photos` | yes |
| `extensions/agentic` | `aminte-agentic-init` | `aminte agentic-init [--force]` | nothing | no: it reads `AMINTE_EXE` and `AMINTE_DB`, and refuses to run without them |
| `extensions/mcp` | `aminte-mcp` | `aminte mcp [--agent NAME]` | `agent:NAME`, default `agent:mcp` | yes: `AMINTE_URL`, else `--url`, else it refuses; `--source` is refused as today |

`aminte-gphotos` reads `AMINTE_URL` before its `--url` flag, so `aminte gphotos` works as the other two do.

## The invariants that keep the rings, as tests

Both are tests of `cmd/aminte` (`boundary_test.go`), over the Go source of `internal/` and `cmd/` that is not a test:

1. **The core names no third party.** No file holds a word of the list, in any case: `google`, `apple`, `telegram`,
   `claude`, `codex`, `opencode`, `anthropic`, `openai`, `modelcontextprotocol`. The list is in the test; a new
   third party is added to it in the change that moves it out.
2. **The core dials only its own server.** Outside `internal/client`, no file holds `http.Client`,
   `http.DefaultClient`, `http.Get(`, `http.Post(`, `http.NewRequest`, `net.Dial` or `net.Dialer`. So every
   integration with the outside world is an extension by force, not by discipline.

And the existing rule that Go enforces: an extension never imports `aminte/internal`.

## What changes where

### Step 1: the discovery, and the contract of an extension

- `cmd/aminte/main.go`: `parse` switches to the extension mode after a first word that is no command of the table
  (only `--db` and `--url` are read after it). `runContext` runs `extension(ctx, e, name, args)` for such a word:
  the lookup, the temporary server when the file exists, the environment, the wait. The usage gets the line
  `aminte NAME [ARGS...]   runs the extension aminte-NAME beside this program or on PATH (extensions/README.md)`.
  The error of an unknown name says both places it looked.
- `extensions/README.md`: the contract above, the list of extensions, and how to build one. It names Google Photos as
  the worked example.
- `README.md`: the section "Extensions" says the command line `aminte NAME` and links `extensions/README.md`.
- Tests (`cmd/aminte/extension_test.go`): a fake extension (a Go program under `testdata/`, built by the test into a
  temporary folder beside a copy of the program's path, or found on a `PATH` the test sets) prints its arguments and
  its environment. The test holds: the words and flags after the name reach it in order, `--db` and `--url` do not;
  `--source` and `--human` are refused; `AMINTE_URL` answers `GET /actions` while the extension runs and is gone
  after; with `--url`, `AMINTE_URL` is that URL and no file is opened; with no file, no `AMINTE_URL` and no error;
  the exit status is the extension's; an unknown name without a binary is refused with the two places; the folder of
  the program wins over `PATH`.

### Step 2: `agentic-init` out

- `extensions/agentic/`: `go.mod` (`aminte-agentic`, the Go version of the root), `agentic/` (the package and its
  templates, moved as they are), `cmd/aminte-agentic-init/main.go` (the flag `--force`; `AMINTE_EXE` and
  `AMINTE_DB` from the environment, refused when absent: "run it as aminte agentic-init"; the output of today),
  `README.md` (what it writes, the build line).
- The templates say the same command lines as today (`aminte mcp --agent NAME`, `aminte agentic-init`), which the
  discovery keeps. A new skill `aminte-extension` says, in one page, how to build an extension: the discovery, the
  environment, the `Aminte-Source` header, the owner-only rule, the loopback rule, and that `GET /actions` of the
  server is the catalog. A test holds that it names the three environment variables `extensions/README.md` names.
- Deleted: `internal/agentic`, `cmd/aminte/agentic.go`, `cmd/aminte/agentic_test.go`, the row `agentic-init` of the
  table of commands, and `errWriter` if nothing else reads it.
- The four tests of `cmd/aminte/agentic_test.go` keep their intent in the module: the files and the database beside
  the program; the MCP configs start the MCP server of this program (the test builds `aminte` and `aminte-mcp`
  into one folder and runs the config's line through the SDK's command transport, after step 3; until then it
  holds the line only); the texts name what the program has (the built `aminte help` and `aminte actions`, the
  tools of the built `aminte-mcp`); a failure or a kept file.

### Step 3: the MCP server out

- `extensions/mcp/`: `go.mod` (`aminte-mcp`, the dependency `modelcontextprotocol/go-sdk` at the root's version),
  `siren/` (the types `Entity`, `Action`, `Field`, `Link`, the error entity, `WriteJSON`, `AgentPrefix`: copied,
  with the fields the server reads), `client/` (`Remote`, `Get`, `Catalog`, `Do` with the multipart of a file field:
  copied), `mcp/` (`mcp.go`, `tools.go` and their tests, moved as they are), `cmd/aminte-mcp/main.go` (the flags
  `--agent` and `--url`; `--source` refused with today's sentence; stdio), `README.md`.
- `cmd/aminte`: the row `mcp` of the table, `e.stdin` if only `mcp` read it, the columns `mcp` and `mcp-remote` of
  `TestParityOfTheSurfaces` and `openMCP`, `openMCPRemote`, `mcpToolNames`. The comment of
  `internal/api/owner_test.go` names the extension's test. `go mod tidy` removes the SDK from the root.
- `README.md`: "One handler, three surfaces" says that the CLI calls the handler in-process and that the MCP server is
  the extension `aminte-mcp`, a client over HTTP; the table "Every action on every surface" keeps its MCP column
  (its test compares strings and imports no `mcp`); the table of packages drops `internal/mcp` and `internal/agentic`
  and names the two modules; the install block builds the three binaries.
- Tests in the module: the tests of `tools` and `mcp` as they are, on a fake catalog; an end-to-end test that builds
  `aminte` and `aminte-mcp` into one folder, runs `aminte mcp --agent test --db TMP/life.db` through the SDK's
  command transport, and holds: `find` is a tool and `snapshot` is not; `capture` writes a day page that
  `aminte get /days/2031-05-20 --db TMP/life.db` reads back with the source `agent:test`; `save_body` with a stale
  version answers the error entity with `stale_version`; `add_file` sends a file by its path; `get` follows a link;
  a line with `--source` is refused. The parity rows of the core that named MCP are covered by these.

### Step 4: the invariants, the rules and the site

- `cmd/aminte/boundary_test.go`: the two tests above.
- **The four rings in the rule books** (the owner's word, 2026-10-19). `AGENTS.md` gets, under "Go implementation",
  the diagram of the rings and the test that decides the ring, with the two invariants as the rule a change
  preserves, and the table of the repo gets the row `extensions/`. `README.md` gets, under "Decisions", the bullet
  "The four rings" with the same diagram, what lives in each ring, and the links to this plan and
  `extensions/README.md`. The diagram is drawn once in each file; the prose says only what it cannot.
- `AGENTS.md`: the rule "Every capability on every surface" names the MCP server as the extension `aminte-mcp`,
  generated from the catalog; the section on the live database says that an extension's temporary server is the
  same writer. `README.md` line 6 and the site's install snippet name the three builds. `deploy/README.md` installs
  the three binaries beside each other.

## Costs the owner accepts

- One `go build` becomes three for a machine with MCP and `agentic-init`. The README shows the lines.
- An MCP session is two processes: `aminte` with the temporary server, and `aminte-mcp`.
- A `aminte` alone, without the extensions beside it, has no `mcp` and no `agentic-init`; the error names the
  binary it looked for.
- The parity table of the core loses its MCP columns; the extension's end-to-end test holds the MCP surface.

## Checks

- At the root: `gofmt -l`, `go vet ./... && go test ./...`, `staticcheck ./...`, `govulncheck ./...`, and
  `go test -count=1 -shuffle=on ./...` for the changes to the CLI's dispatch.
- In each of `extensions/agentic`, `extensions/mcp` and `extensions/googlephotos`: `gofmt -l .`, `go vet ./...`,
  `go test ./...`, `staticcheck ./...`, `govulncheck ./...`.
- Linux is the OS tested. Windows is not tested in this plan; the lookup uses `exec.LookPath` and `filepath`, and the
  `.exe` suffix is its concern.
- By hand, with the owner: build the three binaries into the install folder, restart the service, run
  `aminte agentic-init` in a temporary folder with a copy of nothing (a fresh `aminte init`), and
  `aminte mcp --agent test --db` that file with an MCP client; `aminte gphotos` reaches the service through
  `AMINTE_URL`.

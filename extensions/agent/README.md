# lifelog-agent

`lifelog-agent` answers the owner in the conversation of lifelog. The owner says something on any surface: the
browser, WhatsApp, `lifelog do say` or an MCP tool. The agent reads the conversation page, calls a model, and writes
the answer on the page. The model reads and writes `life.db` through the action catalog of the API, as the
[MCP server](../mcp/README.md) offers it.

It is a client of the lifelog HTTP API ([API.md](../../API.md)). It is a separate Go module, `lifelog-agent`. It
imports `lifelog/siren`, `lifelog/client` and `lifelog/client/tools` of the root module (a `replace` directive), and
nothing of `lifelog/internal`. The core calls no model: this program is the only one that does. It writes as
`agent:lifelog`. The API refuses the owner-only actions to such a writer.

```mermaid
flowchart LR
  S["a say: the browser, WhatsApp, lifelog do say, an MCP tool"] --> L["lifelog serve: the owner's turn on today's conversation page"]
  L -->|"GET /days/today/conversation every 3 seconds"| A["lifelog-agent: one turn at a time"]
  A -->|"GET /settings at each turn: owner token, no source"| L
  A <-->|"chat completions with tools, net/http"| M["the model named in Settings"]
  A -->|"the tools, and save-body: Lifelog-Source agent:lifelog, bearer token"| L
  L -->|"the browser polls the page; lifelog-whatsapp sends each new turn"| C["the chat of the owner"]
```

## How it works

- **The page is the state.** The agent keeps no file. Every 3 seconds it reads today's conversation. When the last turn
  of the page is the owner's, nobody has answered it: that is a turn of the agent. After a restart the agent answers a
  page that waits, and it does not answer a page that it answered. The core owns the convention of the page (the
  title, the category, the first line, the paragraph of a turn: see the conversation class in
  [API.md](../../API.md#conversation)). The agent holds no title and no category.
- **Only `say` writes a turn of the owner.** Text that `save-body` adds to a page is an edit. It is never a turn. Text
  that an edit adds after a reply is part of that reply, and an edit of an old turn changes nothing. So the agent
  answers a message once, whatever surface sent it.
- **One turn at a time.** A turn has at most 16 model steps and 5 minutes. The model reads the whole page of the day.
  The agent writes the tool lines and the reply in one `save-body`, so the page has the whole answer or none. The
  reply is `**lifelog** (HH:MM): text`, with the tool lines before it.
- **The owner may write while the model works.** When the owner adds text with an edit, the reply goes after that text.
  When the owner says a new turn, the reply goes before it, so the new turn is the last one on the page, and the next
  look answers it. A page that changes during each of three saves keeps the reply off the page.
- **A reply that cannot be saved is not run again.** A new run of the model would repeat the tool calls of the first
  run. The agent logs the failure, and it answers that turn again only when the page changes or the agent restarts.
- **A turn before midnight is answered on its own page.** On its first look and on each new day, the agent reads the
  day before too. A turn that waits there is answered there. The page of the new day is not made by the agent.
- **The tools are the catalog, read at each turn.** The agent reads `GET /actions` at the start of each turn and builds
  the tools of the model from it, without the owner-only actions and without an action that has a file field. An app
  that `lifelog serve --with` connects can register after the agent started. Its tools are there at the next turn. A read
  that fails keeps the tools of the last read and writes one line in the log.
- **No file.** An action with a file field (`add-file`, `set-cover`) is not a tool of the model. Its value would be the
  path of any file the process can read, and the model acts on text that any surface wrote to the page.
- **The model is set in lifelog.** The agent reads `GET /settings` at the start of each turn, through a second client:
  the owner's token and no `Lifelog-Source`. Every other request, the tools of the model too, carries
  `Lifelog-Source: agent:lifelog`, so the model's own `get /settings` is refused (403): the key of the model never
  reaches the model or the page. A change in Settings is seen at the next turn. With no model set, the reply of the turn
  is one line, "No model is set: open Settings." The agent keeps running.
- **A reply only.** The agent sends nothing on its own.
- **Earlier days** are pages. The model reads them with `get` on `/days/YYYY-MM-DD/conversation`.

## Build

```
go build -o lifelog-agent ./cmd/lifelog-agent
```

From the repository root: `go build -C extensions/agent -o ../../lifelog-agent ./cmd/lifelog-agent`. Put `lifelog-agent`
beside `lifelog`. The build needs no cgo.

## Run

The way to run it always is the server: `lifelog serve --with agent` (for the service: `lifelog serve --token-file F
--with agent`). The server starts `lifelog-agent` beside itself with its own address and token, and keeps it running. See
"Run by serve" in [extensions/README.md](../README.md). The agent takes no argument then: the settings of lifelog hold
everything.

| line | what happens |
|---|---|
| `lifelog agent [--db FILE]` | A try by hand. `lifelog` serves the file on a temporary loopback server for it ([extensions/README.md](../README.md)). |
| `lifelog-agent --url URL` | The same against a running `lifelog serve` at `URL` (default `$LIFELOG_URL`). `$LIFELOG_TOKEN` is the bearer token. With no URL, it refuses: a guessed address could be the server of another file. |

`--source` is not a flag: the agent always writes as `agent:lifelog`.

The model is not set here. Set it on the Settings page of lifelog (`/settings`), or with
`lifelog do settings model_url=http://HOST/v1 model=ID model_key=KEY`. `model_url` is the base URL of a server that
speaks the OpenAI chat-completions protocol, ending in `/v1`. `model` is the model id. `model_key` is a bearer
credential for the model server, and it is optional. The settings are in a file beside `life.db` (mode 0600). The key
is never a flag, never an environment variable and never in a log line.

The agent needs no state folder. It makes no file.

## Exit codes

| code | meaning |
|---|---|
| 0 | An interrupt or SIGTERM. `serve` does not start it again. |
| 1 | An error, for example a catalog that cannot be read. `serve` starts it again after 10 seconds. |
| 2 | A crash (the Go runtime ends a program that panics with 2). `serve` starts it again after 10 seconds. |
| 78 | The catalog has no `say` or no `save-body`: the server is too old, or it is not a lifelog server. The owner acts first. `serve` does not start it again. |

## No unit of its own

The agent has no service unit. The unit of `lifelog serve` carries `--with agent`
([`deploy/lifelog.service`](../../deploy/lifelog.service)). The model comes from the Settings of lifelog.

## Privacy

- The text of the conversation, and the text of the pages that the model reads, go to the model server that the owner
  sets in Settings. A hosted model sees them.
- The conversation pages are in `life.db`, so every snapshot of it holds them.
- A model writes with no approval step. Its rows carry `agent:lifelog`. Nothing is ever deleted.

## Tests

`go vet ./... && go test ./...` in this folder. The tests of `agent/` run on a fake catalog and a fake model: they
hold the turns, the page, the saves and the days. `e2e_test.go` builds the real `lifelog` and `lifelog-agent` into one
folder and runs `lifelog serve --token-file F --with agent` on a new file, with a fake model server. It says as the
owner with `lifelog do say`, and reads the page back through the API. A `say` gets its reply on the page, a tool call
of an owner action is refused, and with no model set the reply is the one line. It needs the `go` command and the
sources of the repository.

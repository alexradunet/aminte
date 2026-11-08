# aminte-agent

`aminte-agent` answers the owner in the conversation of aminte. The owner says something on a surface that has the
action `say`: the browser, WhatsApp or `aminte do say`. (`say` is an owner action.) The agent
reads the conversation page, calls a model, and writes the answer with the action `reply`. The model reads and writes
`life.db` through the action catalog of the API, as the [MCP server](../mcp/README.md) offers it.

It is a client of the aminte HTTP API ([API.md](../../API.md)) and a Go module of its own, `aminte-agent`, as
[the contract of an extension](../README.md#what-an-extension-is) says. The core calls no model: this program is the
only one that does. It writes as `agent:aminte`, so the API refuses it the
[owner-only](../../API.md#reaching-a-server) actions.

```mermaid
flowchart LR
  S["a say: the browser, WhatsApp, aminte do say"] --> L["aminte serve: the owner's turn on today's conversation page"]
  L -->|"GET /days/today/conversation every 3 seconds"| A["aminte-agent: one turn at a time"]
  A -->|"GET /settings at each turn: owner token, no source"| L
  A <-->|"chat completions with tools, net/http"| M["the model named in Settings"]
  A -->|"the tools, and reply: Aminte-Source agent:aminte, bearer token"| L
  L -->|"the browser polls the page; aminte-whatsapp sends each new turn"| C["the chat of the owner"]
```

## How it works

- **The page is the truth.** The agent keeps no file. Every 3 seconds it reads today's conversation. When the last turn
  of the page is the owner's, nobody has answered it: that is a turn of the agent. After a restart the agent answers a
  page that waits, and it does not answer a page that it answered. The core owns the convention of the page (the
  title, the category, the first line, the paragraph of a turn: see the conversation class in
  [API.md](../../API.md#conversation)). The agent holds no title and no category.
- **The core writes both turns.** `say` writes the turn of the owner, and `reply` writes the turn of the agent. The
  agent cannot save the body of a conversation page, so it writes with `reply`. The core puts the turn in its place,
  makes its lines safe and writes the page in one transaction, so the agent does not answer its own reply. The format,
  the escape of a marker line, the key of a turn and the refusals are in [API.md](../../API.md#conversation). An edit of
  the owner that types a marker line adds a turn of the owner, and the agent answers it when it is the last turn.
- **One turn at a time.** A turn has at most 16 model steps and 5 minutes. The model reads the whole page of the day.
  The agent writes the tool lines and the reply in one `reply` request, so the page has the whole answer or none. The
  request has the fields `day`, `turn`, `text` and `tools`. `turn` is the key of the last owner turn that the model
  read: the `key` of that turn in `turns` of the conversation. A key is an opaque string that the core makes from what
  the turn is, not from its place, so the agent never counts turns. `tools` holds the tool lines, one for each line.
  Each tool line is one line: a control character in a tool name, in the code of an error or in an address becomes a
  space, because the core refuses a tool line that is not one line, and the whole reply would be lost. A backtick in a
  tool name becomes an apostrophe. The name stands in a code span. A backtick would close the span, and the rest of the
  name would read as the words of a call that went well, with an address.
- **The owner may write while the model works.** The core puts the reply right after the owner turn that has the key.
  A turn that the owner says meanwhile stays after the reply, so it is the last turn of the page, and the next look
  answers it. The key of the answered turn does not change when a reply is written, when the owner says a new turn, or
  when the owner edits or removes an earlier turn. So the reply stays right after the turn that it answers.
- **The owner may edit the turn that the model answers.** An edit of the words (or of the time or the relay) of a turn
  gives that turn a new key, and text that the owner adds below a turn is part of the turn. The core then refuses the
  reply to the old key (409, `conflict`), because the page changed, and it writes nothing. The agent reads the page
  again at its next look and answers the turn that waits, with a new run of the model: its words are not the words that
  the first run read. The tool calls of the first run were made already, and the page does not show them. This holds
  for the page of the day before too (see below).
- **A reply that fails with a 5xx or a lost connection is sent again.** The core answers 503 when the file is busy, and
  a connection can end before the answer comes. A second send of the same reply is safe, because the core refuses a
  second reply to a turn that has one (409). So the agent sends the same reply again, up to 4 sends in all, with a wait
  of 1, 2 and 4 seconds, and all within the 30 seconds of a reply. A 409 on a second or later send means that an earlier
  send was written and only its answer was lost: the turn is answered. This holds only when an earlier send may have
  been written: it ended with no status, or with a 5xx other than a busy file. The core writes nothing for a busy file
  (503, code `busy`). After only such sends, a 409 means that the owner edited the turn between two sends. It is a
  refusal like any other, and the log says so. A 4xx on the first send is the answer of the core about this reply, and it
  is not sent again.
- **A reply that is refused or fails is not run again.** A new run of the model would repeat the tool calls of the first
  run. When the sends fail (or a 4xx refuses the reply), the agent logs one line, and the tool lines and the reply are
  lost: the page does not show them. The agent keeps in memory the page that it read, for today and for the day before,
  and it answers a page again only when that page changes or the agent restarts.
- **One agent for one file.** Do not run `aminte agent` by hand while `aminte serve --with agent` runs on the same
  file. The core refuses the second reply to a turn (409), but both models run, and both make their tool calls.
- **A turn before midnight is answered on its own page.** On its first look and on each new day, the agent reads the
  day before too. A turn that waits there is answered there. It reads that page at each look until no turn waits on it,
  so a reply that the core refused with 409 (the owner edited the turn) is answered at a later look. A page whose reply
  the core refused in another way is read and not run again, and it does not stop the agent from answering today's
  page. The page of the new day is not made by the agent.
- **The tools are the catalog, read at each turn.** The agent reads `GET /actions` at the start of each turn and builds
  the tools of the model from it, without the owner-only actions, without `reply` (the agent writes the turn, not the
  model) and without an action that has a file field. An app that `aminte serve --with` connects can register after the
  agent started. Its tools are there at the next turn. A read that fails keeps the tools of the last read and writes one
  line in the log.
- **No file.** An action with a file field (`add-file`, `set-cover`) is not a tool of the model. Its value would be the
  path of any file the process can read, and the model acts on text that any surface wrote to the page.
- **The model is set in aminte.** The agent reads `GET /settings` at the start of each turn, through a second client:
  the owner's token and no `Aminte-Source`. Every other request, the tools of the model too, carries
  `Aminte-Source: agent:aminte`, so the model's own `get /settings` is refused (403): the key of the model never
  reaches the model or the page. A change in Settings is seen at the next turn. With no model set, the reply of the turn
  is one line, "No model is set: open Settings." The agent keeps running.
- **A reply only.** The agent sends nothing on its own.
- **Earlier days** are pages. The model reads them with `get` on `/days/YYYY-MM-DD/conversation`.

## Build

```
go build -o aminte-agent ./cmd/aminte-agent
```

From the repository root: `go build -C extensions/agent -o ../../aminte-agent ./cmd/aminte-agent`. Put `aminte-agent`
beside `aminte`. The build needs no cgo.

## Run

The way to run it always is the server: `aminte serve --with agent` (for the service: `aminte serve --token-file F
--with agent`). The server starts `aminte-agent` beside itself with its own address and token, and keeps it running. See
"Run by serve" in [extensions/README.md](../README.md). The agent takes no argument then: the settings of aminte hold
everything.

| line | what happens |
|---|---|
| `aminte agent [--db FILE]` | A try by hand. `aminte` serves the file on a temporary loopback server for it ([extensions/README.md](../README.md)). |
| `aminte-agent --url URL` | The same against a running `aminte serve` at `URL` (default `$AMINTE_URL`). `$AMINTE_TOKEN` is the bearer token. With no URL, it refuses and exits 78: a guessed address could be the server of another file. |

`--source` is not a flag: the agent always writes as `agent:aminte`.

The model is not set here. Set it on the Settings page of aminte (`/settings`), or with
`aminte do settings model_url=http://HOST/v1 model=ID model_key=KEY`. `model_url` is the base URL of a server that
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
| 78 | The owner acts first: there is no `AMINTE_URL` and no `--url`, or the catalog has no `say` or no `reply` (the server is too old, or it is not an aminte server). `serve` does not start it again. |

## No unit of its own

The agent has no service unit. The unit of `aminte serve` carries `--with agent`
([`deploy/aminte.service`](../../deploy/aminte.service)). The model comes from the Settings of aminte.

## Privacy

- The text of the conversation, and the text of the pages that the model reads, go to the model server that the owner
  sets in Settings. A hosted model sees them.
- The conversation pages are in `life.db`, so every snapshot of it holds them.
- A model writes with no approval step. Its rows carry `agent:aminte`. Nothing is ever deleted.

## Tests

`go vet ./... && go test ./...` in this folder. The tests of `agent/` run on a fake catalog and a fake model: they
hold the turns, the page, the replies and the days. The fake API keeps the rules of the real one that the agent depends
on (the key of each turn, where `reply` puts a turn, its 404, 409 and 422, the escape of a marker line, and the 403 of
`save-body` on a conversation page). `e2e_test.go` builds the real `aminte` and `aminte-agent` into one folder and runs
`aminte serve --token-file F --with agent` on a new file, with a fake model server. It says as the owner with
`aminte do say`, and reads the page back through the API. A `say` gets its reply on the page, a tool call of an owner
action is refused, and with no model set the reply is the one line. A say with a code fence that it does not close, and
a model reply with a line of the form of an owner turn, each get one reply, and no more. A tool name with a new line
or a backtick gets its reply, with the tool line made one line and its backtick an apostrophe. An edit of the turn that the model works on (a `save-body` of the owner)
makes the real core refuse the first reply, and the turn is answered once, by a second run. It needs the `go` command
and the sources of the repository.

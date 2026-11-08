# 102 — A conversation with aminte: the action `ask`, an agent inside the writer, a conversation as a page

> Dated implementation record. Tests use temporary folders, synthetic data and a fake model; no test calls a real
> model.

## Status

- **Date / baseline:** 2026-10-14, `fa2cf64` (master).
- **Priority:** P1. **Effort:** M. **Risk:** MEDIUM: a model writes through the catalog with no approval step; its
  writes carry `agent:chat`, never the owner-only actions, and nothing is ever deleted. Nothing for the schema.
- **Status:** REJECTED (2026-10-28): superseded by [plan 116](116-strip-to-the-core.md) and
  [RFC 0015](../rfcs/0015-no-conversation.md), which removed the conversation from the writer.
- **Resolves:** issue [0071](../issues/0071-the-owner-cannot-talk-to-aminte.md), by
  [RFC 0012](../rfcs/0012-a-conversation-with-aminte.md) option A.

## The owner's decisions (2026-10-14)

| question | decision |
|---|---|
| the model | an OpenAI-compatible endpoint the owner configures; for now the self-hosted model of Pi |
| the conversations | in `life.db`; option A first: a conversation is a page |
| confirmation | the model writes; it asks the owner in the chat when it is not sure |
| the surfaces | full parity: the API, the CLI, the MCP server and the browser |
| owner-only actions | refused to the chat's agent |

## The design

```mermaid
sequenceDiagram
  participant S as a surface (API, CLI, MCP, browser)
  participant A as POST /conversations (ask)
  participant G as the agent (internal/chat)
  participant M as the model
  participant C as the catalog, in-process, agent:chat
  S->>A: question, conversation (optional)
  A->>G: Ask
  G->>C: GET the conversation page (when it continues one)
  loop at most 16 steps
    G->>M: messages and tools
    M-->>G: a reply, or tool calls
    G->>C: each tool call
    C-->>G: the Siren entity
  end
  G->>C: create-page and link part-of Conversations (a new one); save-body with the turn
  G-->>A: the page id, the reply, the steps
  A-->>S: the conversation page, with the result
```

- **The action.** `ask`, `POST /conversations`: `question` (required), `conversation` (the id of a conversation
  page to continue; blank for a new one). The answer is the conversation's page; its `result` is the reply and the
  steps. On every surface by the catalog: `aminte do ask`, the shortcut `aminte ask QUESTION... [--conversation
  ID]`, the MCP tool `ask`, the form on Home and on each conversation page.
- **The tools.** One list for the MCP server and the agent, built in `internal/client` from `GET /actions`: one tool
  for each action that is not `owner`, plus `get_day` and `get`. The agent leaves out `ask` itself, so a model does
  not call a model.
- **The model.** `AMINTE_MODEL_URL` (the base URL, ending in `/v1`), `AMINTE_MODEL` (the model id) and
  `AMINTE_MODEL_KEY` (optional), read by every command that opens the file. The OpenAI chat-completions protocol
  over `net/http`; no new dependency. Without them, `ask` answers 503 `no_model`; a model that fails answers 502
  `model`.
- **The conversation.** A page titled `Conversation YYYY-MM-DD HH.MM.SS` (the server's local time), filed by
  `part-of` in the page `Conversations`, which the first conversation creates. Each turn is appended to the body:

  ```markdown
  **Owner** (14.32): What did I eat on Monday?

  - `get_day` → [/days/2026-10-12](/days/2026-10-12)

  **aminte:** On Monday you wrote ...
  ```

  A continued conversation sends its whole body to the model as the conversation so far, then the new question. The
  body is the only memory: there is no format to parse back, and an edit by the owner is what the model reads next.
- **Bounds.** At most 16 model steps a question; a tool result is cut at 64 KiB, with a note; a model answer is read
  up to 8 MiB. The loop runs in the request: a cancelled request stops it, and no goroutine outlives it.

## The judgment calls

- **The transcript is written once, after the loop.** A model that fails before any tool call leaves nothing. A
  model that fails after a tool call wrote rows: the turn is still written, with the error, so the page says what
  was done.
- **The owner's words and the model's words are ordinary prose of a page.** A `[[Title]]` or `#tag` in them links
  as in any page. The system prompt tells the model to write `[[Title]]` only for a page it read.
- **An outside agent may call `ask`** (parity). The rows the chat's model writes say `agent:chat`, since that model
  wrote them.
- **`conversation` must name a page filed in `Conversations`**; any other page is refused with 422, so a turn is
  never appended to the journal or a note.
- **No streaming yet.** The browser waits for the whole answer. Streaming is a later change if the wait hurts.

## The change

| what | where |
|---|---|
| The tool list and the call of a tool, shared | `internal/client/tools.go`; `internal/mcp/mcp.go` uses it |
| The model client and the loop | `internal/chat` |
| The action `ask`, its route, 503 without a model; on Home and on a conversation page | `internal/api/siren.go`, `internal/api/conversations.go`, `internal/api/days.go`, `internal/api/pages.go` |
| The model from the environment; `aminte ask` | `cmd/aminte/main.go` |
| The action and its result | `API.md`; `README.md` |
| The non-goals row "AI generation" | `docs/architecture/non-goals.md` |

## Tests

- **A fake model** (`httptest`) returns scripted answers: tool calls, then a reply. It records what it received.
- **The loop:** a capture tool call writes the entry with `source = 'agent:chat'`; the transcript page has its
  title, its `part-of` link to `Conversations`, the owner's question, the step line and the reply; a second `ask`
  with `conversation` appends, and the model receives the first turn; owner-only tools are absent and `ask` is
  absent; the step limit; a tool error goes back to the model as a result; a malformed tool call; a cancelled
  context; a page outside `Conversations` refused; no model configured.
- **Surfaces:** `ask` through the API, the CLI (in-process), the MCP tool and a browser form (redirect to the page).
- **The MCP tools** are the same list as before plus `ask`.
- Changed with the behaviour, never to pass: `reference_test.go` (the result of `ask`).

## Done criteria

- `go generate ./... && go vet ./... && go test ./...` green; `gofmt`; `staticcheck ./...` and `govulncheck ./...`
  when available.
- One `ask` against the owner's model on a synthetic database, not on the owner's `life.db`.

## Result

Built as designed above. One detail was found in the work: the hypermedia client decodes numbers as `json.Number`,
so the agent reads the id of a new conversation page as one.

- **Checks** (Linux, Debian 13, Go 1.27.2): `go generate ./... && go vet ./... && go test ./...` green; `gofmt`
  clean; `go test -count=1 -shuffle=on` of `internal/chat`, `internal/client`, `internal/mcp` and `cmd/aminte`
  green. One rule was broken on purpose and caught: with `ask` given to the model as a tool, the chat test failed.
- **Not run:** `staticcheck` and `govulncheck` (not installed here), the race check, a Windows run, and the one `ask`
  against the owner's model (the session was not allowed to reach it). That last check is still open, so the plan
  stays IN PROGRESS until the owner runs it.

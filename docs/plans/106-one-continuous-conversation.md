# 106 — One continuous conversation: a page per day, compacted into the daily note

> Dated implementation record. Tests use temporary folders, synthetic data and a fake model; no test calls a real
> model. The owner's live `life.db` is never opened by a test.

## Status

- **Date / baseline:** 2026-10-14, `e2b930f` (master).
- **Priority:** P1. **Effort:** M. **Risk:** MEDIUM: the model writes a summary into the owner's journal page with
  no approval step, as `agent:chat`, once a day. Nothing for the schema or the contract: a conversation stays a page
  ([RFC 0012](../rfcs/0012-a-conversation-with-aminte.md), option A). `ask` loses its `conversation` field on every
  surface: a visible change, by the owner's word.
- **Status:** DONE.
- **Resolves:** no issue, no RFC. The owner's words are the incident.

## The owner's words (2026-10-14)

"I don't like that we have a chat with different sessions. I want one continuous conversation, not multiple ones.
The previous day will always be compacted and every day we continue the conversation; compact the previous day and
save them to the daily note. The new day AI will get its context based on the previous day notes."

The owner's answers to the three open questions:

| question | decision |
|---|---|
| when compaction runs | at the first `ask` of a new day, in that request; no timer, no goroutine, no new catalog action |
| the model's context at a new day | the journal page of the nearest earlier day within seven days, whole |
| the chat screen above today's turns | one line from Aminte: the previous day's conversation is summed up in that day's page, with a link to the transcript |

## The design

```mermaid
sequenceDiagram
  participant S as a surface (API, CLI, MCP, browser)
  participant A as POST /conversations (ask)
  participant G as the agent (internal/chat)
  participant M as the model
  participant C as the catalog, in-process, agent:chat
  S->>A: question
  A->>G: Ask(question)
  G->>C: GET /conversations (today: its page, previous, memory)
  G->>C: follow previous to the conversations not yet compacted
  loop the oldest three, oldest first, stopped by a failure
    G->>M: sum up day D (no tools; the memory of D given)
    G->>C: capture on /days/D: the summary, with [[Conversation D]]
    G->>C: save-body on Conversation D: the trailer "**Compacted** into [[D]]"
  end
  G->>C: GET /conversations again when a summary was written
  loop at most 16 steps
    G->>M: system, the memory page, today's turns, the question; tools
    M-->>G: a reply, or tool calls
    G->>C: each tool call
  end
  G->>C: create-page "Conversation <today>" and link part-of Conversations, or save-body (the turn appended)
  G-->>A: the reply, the steps (the compactions first)
  A-->>S: today's conversation (/conversations)
```

- **The day is the server's.** `localDay` of the API decides which page is today's; the agent reads it from
  `GET /conversations`. `Agent.Now` gives the turn's `HH.MM` and the time in the prompt. Both are read once at the
  start of a turn, so a question asked at 23:59 lands on the day it was asked, and the next question compacts it.
- **One page per day:** `Conversation YYYY-MM-DD`, filed `part-of` in `Conversations`, created at the first question
  of the day. A conversation's day is the ten characters after `Conversation ` when they are a day: the pages of
  plan 102, `Conversation YYYY-MM-DD HH.MM.SS`, have a day too and are compacted like any other. A page filed in
  `Conversations` whose title names no day is read, never compacted, and never the previous conversation.
- **The compacted mark** is a trailer line of the transcript, `**Compacted** into [[2031-05-13]]`, written and read
  in `internal/api/conversations.go`, the format's one home. It belongs to no turn, wherever the owner moved it. Its
  `[[day]]` is an ordinary wikilink: the transcript links the day page.
- **The summary** goes on the journal page of day D through `capture` as `agent:chat`:
  `**Conversation with aminte** ([[Conversation D]]): ` and the model's words. The wikilink links the day page back to
  the transcript, so the day shows the conversation among its backlinks.
- **The chain.** The conversations not yet compacted are always the newest ones: compaction goes oldest first and
  stops at the first failure, so an older page is never skipped and hidden behind a newer one. At most three a
  question, so a long silence is caught up a few days at a time. At most 100 `previous` links are followed.
- **The memory** of the conversation of day D is the nearest live journal page in the seven days before D, the link
  `memory` of the conversation. The compaction of D gets the same memory, so the summary of D carries forward what
  the summary of D-1 left open. The memory is read again after a compaction wrote: it may be a page that did not
  exist before.
- **Bounds:** a transcript sent to be summed up is cut at 64 KiB, a memory page at 16 KiB; the 16 steps and the
  64 KiB tool result of plan 102 stand.

## The judgment calls

- **The summary first, the trailer second.** A trailer refused as stale (the owner edited the page meanwhile) leaves
  the summary on the day page and the conversation to be summed up again: the owner sees the summary twice, and
  never loses one. The other order could lose a summary and mark the page done.
- **A failed compaction is a step, not a refusal.** The question is answered; the turn says `compact` and the error;
  the next question tries again. The rule of plan 102, "write nothing when the model fails first", counts the
  compaction steps: when a summary was written and the model then failed, the turn is kept and says what was done.
- **`/conversations` is a resource, not a redirect:** the owner's one URL, every day. `/conversations/{id}` keeps its
  own address for the conversation of an earlier day, read in full with no composer and a link to today.
- **The note has two forms.** Before the first question of a day the previous conversation is not compacted yet, so
  the note says "at your first question of today"; after it, "is summed up in that day's page".
- **Two questions at once on a new day:** the second `create-page` answers 409; the agent reads today again and
  appends. Two at once on the day's page: the second `save-body` is stale and its turn is lost with an error, as in
  plan 102.
- **`entities.day` of a conversation page** is the real clock's day at `create-page`; the conversation's day is its
  title, and nothing reads the stamp. The service's `TZ` sets the local day, as for every day page.

## The change

| what | where |
|---|---|
| The trailer, the day of a title, `previous`, `memory`, today's conversation as `GET /conversations` | `internal/api/conversations.go`, `internal/api/serve.go`, `internal/api/siren.go`, `internal/api/html.go` |
| `NewOn` exported: the chat tests fix the server's day | `internal/api/serve.go` (`export_test.go` removed) |
| The compaction, the memory, the day's page; a request with no tools names none | `internal/chat/agent.go`, `internal/chat/model.go` |
| `aminte ask` takes the question alone | `cmd/aminte/main.go` |
| The chat page without the list, the note, no composer on an earlier day; the sidebar without New | `internal/api/html/conversation.html`, `app.js`, `app.css`, `root.html`, `page.html` |
| The classes and the result | `API.md`, `README.md` |

## Tests

- The format: a compacted page (the trailer is no part of the last reply), prose and the trailer, a trailer moved
  into the middle; `FormatCompacted` and a `compact` step read back; the day of a title, the old title included.
- The agent, on a fake model with a day the test moves: the first question of a day sums up yesterday (the request
  with no tools holds the transcript; the day page holds the summary as `agent:chat`, both pages link each other,
  the turn says `compact`, the model's memory holds the summary; the next question asks nothing more); at most
  three, oldest first, each with the memory of its day; a failed compaction answers the question and is tried
  again, and a model that fails after a compaction keeps the turn; an old-format title is compacted and a page with
  no day is not; a stale trailer writes the summary twice; a page that appears during the turn gets the turn
  appended; a day that turns during the model request keeps the turn on the day it was asked.
- The resources: today's conversation before and after its first question, `previous`, `memory`, `today`, `ask` on
  today's alone, a tombstoned previous, 404s, the 303 of a conversation's page.
- The surfaces: the CLI's second question lands on the same page and `--conversation` is refused; the MCP tool
  `ask` takes `question` alone; the browser's form on Home and the chat page, the note in both forms, no composer on
  an earlier day, no list, no New; the script fetches `/conversations` and keeps open or closed alone.

## Done criteria

- `go generate ./... && go vet ./... && go test ./...` green; `gofmt`; `staticcheck ./...` and `govulncheck ./...`
  when available.
- The installed binary rebuilt and the service restarted; the owner's first question of the next day writes the
  summary of today into today's page.

## Result

Built as designed, with one thing found in the work: the memory of a conversation must be read again just before it
is summed up, since the compaction of the day before has just made the page that is its memory. The design above
says so.

- **Checks** (Linux, Debian 13, Go 1.27.2): `go generate ./... && go vet ./... && go test ./...` green; `gofmt`
  clean; `go test -count=1 -shuffle=on ./internal/chat ./internal/api ./cmd/aminte ./internal/mcp` green.
- **Not run:** `staticcheck` and `govulncheck` (not installed here), the race check, a Windows run, the script in
  a real browser.

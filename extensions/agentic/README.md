# aminte-agentic-init

`aminte-agentic-init` makes the folder of the `aminte` program ready for a coding agent that is opened in it
([plan 090](../../docs/plans/090-agentic-init.md)). It is the extension behind `aminte agentic-init`.

It is a separate Go module, `aminte-agentic`. It uses the standard library only. It imports nothing from aminte. It
opens no database and calls no API: it reads two variables that `aminte` sets for an extension
([extensions/README.md](../README.md)), `AMINTE_EXE` and `AMINTE_DB`, and writes files beside `AMINTE_EXE`.

## What it writes

The texts are templates embedded in the program (`agentic/templates`). They name the paths of the program and of the
database.

| file | for |
|---|---|
| `AGENTS.md`, `CLAUDE.md` | what is in the folder, the rules for the agent; `CLAUDE.md` imports `AGENTS.md` |
| `.agents/skills/*/SKILL.md`, `.claude/skills/*/SKILL.md` | the skills: import notes, keep photos, build an extension |
| `.mcp.json`, `.pi/mcp.json`, `opencode.json`, `.codex/config.toml` | the MCP config of Claude Code, Pi, OpenCode and Codex. Each starts `aminte mcp --agent NAME --db FILE`. |

A file that does not exist is written. A file with the same text is `unchanged`. A file with another text is `kept`,
unless `--force`, which `replaces` it. Every write goes to a temporary file first, then a rename.

## Build

```
go build -o aminte-agentic-init ./cmd/aminte-agentic-init
```

From the repository root: `go build -C extensions/agentic -o ../../aminte-agentic-init ./cmd/aminte-agentic-init`.
Put `aminte-agentic-init` beside `aminte`.

## Run

```
aminte agentic-init [--force]
```

`aminte` finds `aminte-agentic-init` beside itself and runs it. Run alone, without `AMINTE_EXE` and `AMINTE_DB`,
it refuses and says so.

## Tests

`go vet ./... && go test ./...` in this folder. The tests of `agentic/` run on a temporary folder. `e2e_test.go` builds
the real `aminte`, `aminte-mcp` and `aminte-agentic-init`, copies them into a folder of its own for each test, and
holds: the files and the database beside the program, the MCP configs (each line is run, and the server lists its
tools), the texts against the built `aminte help` and `aminte actions`, a failure and a kept file. It needs the `go`
command and the sources of the repository.

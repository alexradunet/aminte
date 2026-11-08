# 0059 — An agent opened in the aminte folder knows nothing about it

- **Date:** 2026-10-09
- **Status:** resolved
- **Seen in:** the owner's first imports with a local agent (Pi), the day the import process was removed (plan 089)

## What happened

The owner keeps the writer and the database in one folder, and imports with a coding agent opened in that folder.
The agent knew nothing about it: what `life.db` is, which program writes it, which tools it has, what it may do and
what it must ask. The coordinator wrote by hand, for one agent only:

- an instruction file (`AGENTS.md`) with the import procedure and the rules;
- the agent's MCP config, pointing at the database with `--db`;
- a start script that refuses to run without a database.

Each agent framework reads these from other places: Claude Code reads `AGENTS.md` (or a `CLAUDE.md`), skills in
`.claude/skills/` and MCP servers in `.mcp.json`; Codex reads `AGENTS.md`, `.agents/skills/` and
`.codex/config.toml`; OpenCode reads `AGENTS.md`, `.agents/skills/` and `opencode.json`; Pi reads `AGENTS.md`,
`.agents/skills/` and `.pi/mcp.json`. A new user has no way to set up any of them, and every command needs `--db`
or `AMINTE_DB`, even though the database lives beside the program.

Expected: one command of the writer makes the folder ready for any of these agents, and the writer finds the
database beside itself.

## Reproduce

1. A folder with `aminte` and a `life.db` made by `aminte init --db life.db`.
2. Open an agent framework in the folder and ask it to import a note: it has no aminte tools and no instructions.
3. `aminte day` in the folder: "no database: pass --db PATH or set AMINTE_DB".

## Rules involved

- the writer's decisions: README, "Every capability on every surface" (AGENTS.md) and "Owner-only actions"
- [imports](../contract/imports.md): an importer is a program, or an agent working with the owner

## Resolution

Resolved by [plan 090](../plans/090-agentic-init.md): `aminte agentic-init` writes `AGENTS.md`, a `CLAUDE.md` that
imports it, three skills in `.agents/skills/` and `.claude/skills/`, and the MCP config of Claude Code, Codex,
OpenCode and Pi into the folder of the executable; a file with another text is kept unless `--force`. Without `--db`,
`AMINTE_DB` or `--url`, aminte uses the `life.db` beside the executable, and `aminte init` makes it there.

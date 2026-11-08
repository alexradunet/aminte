# 082 — A snapshot from any surface: the owner-only `snapshot` action

> Dated implementation record. The schema and the snapshot's contract ([take a snapshot](../cookbook/take-a-snapshot.md),
> [D25](../decisions/D25-snapshots.md)) are untouched; this plan moves the command behind the catalog.

## Status

- **Date / baseline:** 2026-10-08, `37767bb` (master).
- **Priority:** P2. **Effort:** S. **Risk:** LOW; the copy, the naming, the git-work-tree refusal and the restore
  check are the same code, called from one more place.
- **Status:** DONE.
- **Resolves:** the last exception to the parity rule ([AGENTS.md](../../AGENTS.md), "Every capability on every
  surface"): `aminte snapshot` was a CLI command only, so a phone or a browser on a `--public` listener could not
  take a backup. The owner agreed on 2026-10-08.

## The decision

Replace the README bullet **"Snapshots are the owner's"** with:

> **Snapshots are the owner's, from any surface** ([take a snapshot](docs/cookbook/take-a-snapshot.md)). `snapshot`
> is an owner-only action of the catalog (`POST /snapshots`): refused to an `agent:*` writer, never an MCP tool, offered
> at the root to the owner. It takes no path: the file lands in the folder `serve --snapshots DIR` names, or beside
> `life.db`, named by the local day and never overwritten; it refuses a folder inside a git work tree, and its restore
> check opens the snapshot with `Close` skipping `PRAGMA optimize`. The answer carries the path and the check.
> `aminte snapshot [--to DIR]` is the action in-process, `--to` choosing the folder; with `--url` the server's
> folder is the one, and `--to` is refused. Restoring stays the manual procedure of the cookbook.

## The change

| what | where |
|---|---|
| **One operation.** `core.Snapshot(ctx, from, dir, now)` is the command's `takeSnapshot` moved: the copy, then the restore check on a connection whose `Close` skips `PRAGMA optimize`. `Store.SnapshotDir` names the folder; empty means beside the file, which `db.DB.Path()` now exposes. `Store.TakeSnapshot(ctx, now)` is the action's entry. | `internal/core/snapshot.go`, `internal/db/db.go` |
| **The action.** `snapshot` in the catalog, `owner: true`, no fields; `POST /snapshots` behind `ownerOnly`. The answer is class `snapshot` with `snapshot` (the path) and `restore_check` (the integrity result) in its properties and in `result`, and a `self` of `/` so a browser form is answered home with the result as feedback. The root offers it. | `internal/api/siren.go`, `handler.go`, `html/root.html` |
| **The CLI.** `serve --snapshots DIR` (an existing folder) sets the store's folder. `aminte snapshot` runs the action through the client it already builds: in-process with `--to` as the folder, remote without it; the exit status is still an error when the restore check fails. | `cmd/aminte/main.go` |
| **Docs.** README bullet as above; AGENTS.md drops the exception; `API.md` gains the class; `aminte help`. | `README.md`, `AGENTS.md`, `API.md` |

## Tests (with their subject; synthetic databases only)

1. `internal/api`: the owner's `snapshot` writes `life-<day>.db` in the store's folder, with a passing check in the
   answer, and a second one the same day gets the time suffix; an `agent:*` source is 403 and writes no file; the
   root offers the action to the owner and the browser's form answers home with the path in the feedback.
2. `internal/mcp`: `snapshot` is in the owner-only list that must never be a tool.
3. `cmd/aminte`: the existing snapshot tests call `core.Snapshot`; `aminte snapshot --to DIR` through `runContext`
   writes the file and reports it; `--url` with `--to` is refused; `serve --snapshots` with a missing folder is
   refused before listening.
4. The reference test covers the class.

## Done criteria (all met; checks on Windows: gofmt, go generate, go vet, go test -count=1 ./... green; staticcheck and govulncheck not run)

1. `snapshot` reaches every surface the owner uses, is refused to agents, and is not an MCP tool.
2. The CLI's behaviour (naming, refusals, exit status) is unchanged for the owner at a terminal.
3. README, AGENTS.md and API.md say so once each; nothing under `docs/` changes.
4. Baseline `go generate ./... && go vet ./... && go test ./...` green and `gofmt` clean on Windows; the commit says
   which checks ran; this plan marked DONE in that commit.

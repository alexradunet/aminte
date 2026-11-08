# 079 — Client contract hygiene: error codes, list pages, a connect timeout, one parameter

> Dated implementation record. The schema is untouched; these are application fixes of `aminte` for the clients
> [plan 078](078-network-server.md) lets in. No RFC: no decision changes.

## Status

- **Date / baseline:** 2026-10-08, `b28ee22` (master).
- **Priority:** P2. **Effort:** S. **Risk:** LOW; every change is additive on the wire.
- **Status:** DONE.
- **Resolves:** the review of 2026-10-08 (the owner's ask for Flutter, Android, web and React clients): a client
  cannot tell a stale version from an invalid title without parsing prose; the people, places, files and ghost lists
  return every row and the days and search lists a fixed count, with no way to ask for the rest; the remote client
  dials without a timeout; `linkKinds` concatenates a page type into SQL.

## The change

| what | where |
|---|---|
| **Error codes.** Every error entity carries `code` beside `status` and `message`: `core.Error` gains an optional `Code`, and `errorEntity` fills it from the error or, failing that, from the status (`bad_request`, `unauthorized`, `forbidden`, `not_found`, `conflict`, `too_large`, `invalid`, `timeout`, `internal`). Specific codes where a client branches: `stale_version` (a body save with an old version), `exists` (a title in use, with its `existing` link as before), `cross_origin` (the browser guard), `token_required` (a public listener). Messages are unchanged. | `internal/core/core.go`, `write.go`; `internal/api/handler.go`, `public.go` |
| **List pages.** `/days`, `/people`, `/places`, `/files`, `/ghosts` and `/search` take `limit` (1–500; defaults: days 60, search 50, the rest 200) and `offset` (≥ 0). The entity carries `limit` and `offset` in its properties and a `next` link when a further page exists (one row more is read to know), a `prev` link when `offset > 0`. Orders are unchanged. The readers take `limit, offset`; a bad value is 422. The HTML `list` and `search` views show the two links. Metrics, sessions and periods keep their filters and stay whole: a metric registry is small, and the other two carry day filters a client pages with. | `internal/core/read.go`, `internal/api/handler.go`, `html/root.html` |
| **Connect timeout.** `client.Remote` dials and handshakes with a 10-second bound through its own transport (proxy from the environment as before); the response stays unbounded, since a replay or an upload may take minutes and the command's context carries the interrupt. | `internal/client/client.go` |
| **One parameter.** `linkKinds` calls a new `core.Store.LinkKindsFrom(ctx, typ)` that binds the type, instead of concatenating it into an ad-hoc query. Same rows, same order. | `internal/core/read.go`, `internal/api/handler.go` |

## Tests (with their subject; synthetic databases only)

1. `internal/api`: a table of refusals and their codes (stale save, taken title, unknown page, bad day, missing
   field, cross-origin form, public listener without a token), each asserting `code` and `status` together.
2. `internal/api`: a list with more rows than one page is walked by its `next` links without a gap or a repeat,
   the last page has none, `prev` appears from the second page; `limit=0`, `limit=501`, `offset=-1` and
   `offset=x` are 422; the defaults equal today's counts; the browser views show the links.
3. `internal/core`: the readers' `limit, offset` against hand-counted rows; `LinkKindsFrom` for each page type
   against the registry read directly.
4. `internal/client`: the transport is configured (the timeout is bounded in code, not observed against a black
   hole: no network test).

## Done criteria (all met; checks on Windows: gofmt, go generate, go vet, go test -count=1 ./... green; staticcheck and govulncheck not run)

1. Every error answer carries a `code`; the listed specific codes appear where stated; no message changed.
2. The six list routes page as described; the existing action-offer and view tests pass unchanged.
3. `client.Remote` has a bounded dial; `linkKinds` has no concatenated SQL.
4. README's hypermedia bullet names `code` and the list paging in one sentence each; no change under `docs/`.
5. Baseline `go generate ./... && go vet ./... && go test ./...` green and `gofmt` clean on Windows; the commit says
   which checks ran; this plan marked DONE in that commit.

# 080 — An API reference for client authors, kept true by a test

> Dated implementation record. The schema is untouched; this is application documentation of `aminte`'s wire
> shape, for the clients [plan 078](078-network-server.md) lets in.

## Status

- **Date / baseline:** 2026-10-08, `68ca8d1` (master).
- **Priority:** P2. **Effort:** S. **Risk:** none to data; the risk of a reference is drift, which the test removes.
- **Status:** DONE.
- **Resolves:** the review of 2026-10-08: `GET /actions` documents every input, but nothing documents what a
  `day`, `page` or `series` entity's `properties` contain, so a typed client (Dart, TypeScript) has to guess.

## The change

| what | where |
|---|---|
| **The page.** `API.md` at the repo root, linked from the README: reaching a server (local, `--public`, provenance, representation, how writes answer), the Siren entity's fields, the errors and their codes, the pages of a list, then every entity class with its links, embedded links, actions and a property table (`property`, `type`, `meaning`; nested paths as `name.field` and `name[].field`), and the result shapes of the writes (sync, kept, relocation). The import classes are named and left to the importing guide. | `API.md`, `README.md` |
| **The test.** `TestAPIReferenceMatchesTheCode` in `internal/api` reads the tables and checks, per section, that the documented paths equal the paths of the Go property type (reflection over the JSON tags, embedded structs flattened), and that every route's live JSON on a synthetic fixture carries only top-level keys the section names. A section without a shape in the test fails the test. | `internal/api/reference_test.go` |

## Done criteria (all met; checks on Windows: gofmt, go generate, go vet, go test -count=1 ./... green; staticcheck and govulncheck not run)

1. Every class the API emits outside `/import` has a section with every property it sends, nothing more.
2. Removing a row, renaming one or adding a property to a Go type fails the test (checked by mutation once).
3. The README links the page; nothing under `docs/` changes.
4. Baseline green; this plan DONE in the commit of the change.

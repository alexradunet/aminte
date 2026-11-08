# 078 — Serve the API beyond this machine: a token, allowed origins, optional TLS

> Dated implementation record. The schema is untouched; this is an application decision of `aminte`
> ([README decisions](../../README.md#decisions)), recorded here so another agent can carry it out.

## Status

- **Date / baseline:** 2026-10-08, `05790ec` (master).
- **Priority:** P1. **Effort:** M. **Risk:** LOW for the schema (untouched), HIGH for privacy if done wrong
  (`life.db` holds health data): the default must stay exactly as it is today.
- **Status:** DONE.
- **Resolves:** the owner's requirement of 2026-10-08 that `aminte` run as the backend of clients on other
  devices and origins (a phone app, a browser app served from elsewhere) without a second implementation of
  any write. No issue: nothing broke; the API was never reachable from elsewhere by design.

## Why and boundaries

`aminte serve` is local-only on purpose: it binds a loopback address, checks that every request's `Host` is that
listener (so a DNS-rebinding page cannot reach it), refuses cross-origin browser writes, and trusts nothing but the
socket. That is the right default for a file of health data on one machine. It also means no client on another
device can reach the API at all, and no browser application served from another origin can even read it.

The owner's goal is one handler and many clients ([README](../../README.md#decisions), "One handler, three
surfaces"). The handler is ready for that; the listener is not. This plan gives the listener one way to be reached
from elsewhere, opted into explicitly, and keeps everything else as it is.

What changes nothing: the in-process CLI and MCP paths, the loopback default, the owner-only rule
(`register-metric`, `replay`), the `Aminte-Source` provenance, the action catalog, the HTML face on this machine.

Out of scope: user accounts or more than one credential (one owner, one file, one token), an agent-role token
(agents reach the API over MCP on stdio, or over the network as the owner's client), rate limiting, audit
logging, a built-in tunnel, certificate issuance (the owner brings a certificate or a private network),
pagination and error codes (the next plan), planning routes.

## The decision

Replace the README bullet **"`serve` is local-only"** with this, rewritten in place:

> **`serve` is local-only unless the owner says `--public`.** Without the flag `serve` is exactly what it was: the
> default bind is `127.0.0.1:7777`, `--addr` accepts a loopback IP or `localhost` with a numeric port and nothing
> else, the `Host` check and the cross-origin guard stand, and `--token-file`, `--allow-origin`, `--tls-cert` and
> `--tls-key` are refused as flags of `--public`. **`--public`** opens the listener and requires a **token**:
> `--token-file PATH` (a file holding one line of at least 32 characters; the command line never carries the
> secret), or `AMINTE_TOKEN`; `--public` without one is refused before the listener opens. With `--public` the
> default bind is `0.0.0.0:7777` and `--addr` may name any IP of this machine; every request must carry
> `Authorization: Bearer <token>` or the browser cookie `/login` sets, a request without one is answered 401 with
> no entity of the database, and the `Host` check is dropped: it existed to keep an unauthenticated local server
> from a rebinding page, and a bearer token defends against that page on its own. A token holder is the owner:
> owner-only actions are refused by `Aminte-Source: agent:*` as before, not by the token. **Origins**:
> `--allow-origin ORIGIN` (repeatable, exact scheme://host[:port]) answers CORS preflights and marks the origin
> trusted for browser writes; any other origin is refused as today. **TLS**: `--tls-cert` and `--tls-key` serve
> HTTPS with the standard library; without them a token on a network is sent in clear, which is acceptable only on
> a private network (a VPN, a mesh such as Tailscale) and the startup line says so. Forwarded headers still confer
> no trust; a reverse proxy that terminates TLS is supported only because it forwards the bearer header unchanged.
> The CLI and the MCP server with `--url` send `AMINTE_TOKEN`.

The browser cookie: `GET /login` shows a form (the only page served without a credential, with the home link);
`POST /login` with the token sets `aminte_token` (HttpOnly, SameSite=Strict, Secure when served over TLS, path `/`)
and redirects to `/`; `POST /logout` clears it. The cookie carries the same token, compared the same way. Cross-site
requests with the cookie are what the existing `CrossOriginProtection` guard refuses, so forms stay safe. The cookie
is a presentation convenience for the HTML face on a phone; a program sends the header.

## The change

| what | where |
|---|---|
| **Flags and startup.** `--public` (boolean), `--token-file`, `--allow-origin` (repeatable), `--tls-cert`, `--tls-key` in `parse`. `--public` without a token is refused before the listener opens, with a message naming the two ways to supply one; any of the four other flags without `--public` is refused, so nothing is silently ignored. A token shorter than 32 characters, or with a trailing newline other than one, is refused. With `--public` the startup line names the bind, whether TLS is on, and "token required". `AMINTE_TOKEN` on `--url` commands is sent by the remote client. | `cmd/aminte/main.go`, `internal/client/client.go` |
| **Bind validation.** `LoopbackAddress` stays for the local case. A new `PublicAddress(authority)` admits any numeric IP with a numeric port, used only under `--public`; its default is `0.0.0.0:7777`. No DNS, no service names, as before. | `internal/api/authority.go` |
| **The guard.** `NetworkAuthority` stays the local guard, unchanged. A new `Public(next, Options{Token, Origins, TLS})` is mounted instead of it under `--public`; it (1) answers `OPTIONS` preflights for allowed origins (`Access-Control-Allow-Origin: <origin>`, `Vary: Origin`, methods `GET, POST`, headers `Authorization, Content-Type, Accept, Aminte-Source`, max-age), (2) adds `Access-Control-Allow-Origin` and `Vary: Origin` to every answer for an allowed origin, (3) exempts `GET /login` and `POST /login` only, (4) requires the bearer header or the cookie, compared with `crypto/subtle.ConstantTimeCompare`, answering 401 with `WWW-Authenticate: Bearer` and an error entity that holds nothing but the status and "token required", (5) still refuses absolute-form targets. Allowed origins are also passed to `CrossOriginProtection.AddTrustedOrigin`. The in-process client never sees any of this. | `internal/api/authority.go`, `internal/api/handler.go` (`New` takes the trusted origins) |
| **Login pages.** `GET /login`, `POST /login`, `POST /logout`, one small template in `html/`. Mounted by `Public` only; a 404 without `--public`. | `internal/api/login.go`, `internal/api/html/login.html` |
| **Docs.** The README bullet above, the usage text of `aminte serve`, the `Layout` row of `internal/api`. Nothing in `docs/` changes: the contract knows no listener. | `README.md`, `cmd/aminte/main.go` |

## Tests (with their subject; synthetic databases only)

1. `internal/api`: `NetworkAuthority` and the existing `authority_test.go` and `TestBrowserOriginProtection` pass
   unchanged. Under `Public`: a request without a credential is 401 with no
   database content, with a wrong token 401, with the right header 200, with the cookie 200; a token that differs
   only in its last byte is refused; `Host` is not checked; a cross-site form with the cookie is refused;
   `GET /login` is 200 without a credential and `GET /` is not.
2. CORS: a preflight from an allowed origin gets the headers and 204; from another origin no CORS headers and the
   write is refused; a GET from an allowed origin carries `Access-Control-Allow-Origin` and `Vary: Origin`.
3. Parity: a write sent with the token through `client.Remote` against an `httptest` server persists the same rows,
   with the client's own source, as the in-process client (`public_test.go`).
4. `cmd/aminte`: `serve --addr 0.0.0.0:0` without `--public` is refused as today; `serve --public` without a
   token exits 1 before listening; `--token-file` without `--public` is refused; `parse` reads the flags; a short
   token file is refused. One process smoke test: `serve --public --addr 127.0.0.1:0 --token-file`, a request
   without the header is 401 and with it 200. Loopback keeps this test free of the Windows firewall prompt; the
   non-loopback bind is covered by `PublicAddress` unit tests, and the OS actually tested is named in the commit.
5. TLS: `httptest.NewTLSServer` is enough to show the cookie gets `Secure` over TLS and not otherwise; the
   `--tls-cert/--tls-key` path is exercised once with a test certificate generated in `t.TempDir()`.

## Done criteria (all met; checks on Windows: gofmt, go generate, go vet, go test -count=1 ./... green; staticcheck and govulncheck not run)

1. `aminte serve` without `--public` behaves exactly as at the baseline: same bind, same refusals, same tests;
   the new flags are refused rather than ignored.
2. With `--public` and a token file, a client on another device reaches every route with the header,
   and a browser on that device uses the HTML face after `/login`; a request without a credential learns nothing
   but 401.
3. A browser application on an allowed origin can read and write with the header; one on any other origin is
   refused as today.
4. `AMINTE_TOKEN` makes `aminte --url` commands and `aminte mcp --url` work against a token listener.
5. README decision rewritten as above; usage text current; no change under `docs/`.
6. Baseline `go generate ./... && go vet ./... && go test ./...` green and `gofmt` clean on Windows; the commit says
   which checks ran; this plan marked DONE in that commit.

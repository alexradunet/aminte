---
name: aminte-extension
description: Build an extension of aminte: a program aminte-NAME that reads and writes the life log through the API. Use when the owner asks for an importer, a bridge to a service, or any new program around aminte.
---

# Build an extension of aminte

An extension is a client of the API of aminte. It is a separate program. It does not change `aminte`.

## What it is

- It talks HTTP to the server at `AMINTE_URL`. `GET /actions` of that server is the catalog of what it can do.
- Every answer is Siren JSON: an entity with properties, links and actions. An action names its method, its address
  and its fields. Follow the links and the actions. Do not build addresses by hand.

## How it starts

`aminte NAME ARGS...` runs the program `aminte-NAME`. `aminte` looks in the folder of `{{.Exe}}` first, then on
`PATH`. A name that is a command of `aminte` is never an extension.

| variable | meaning |
|---|---|
| `AMINTE_URL` | the API. `aminte` starts a temporary server on loopback when the file exists. |
| `AMINTE_EXE` | the path of `aminte` |
| `AMINTE_DB` | the absolute path of the file, `{{.DB}}` here |
| `AMINTE_TOKEN` | when set, send it as a bearer token |

## Rules

1. Send the header `Aminte-Source: import:NAME` (an importer) or `Aminte-Source: agent:NAME` (a model). The
   owner-only actions are refused to such a writer.
2. Never open `{{.DB}}`. Write only through the API.
3. Keep the state of the extension (a token, a cursor) in its own folder, for example `os.UserConfigDir()/aminte-NAME/`.
   Never keep it in `life.db`.
4. A page that the extension serves listens on loopback (`127.0.0.1`) only.
5. In Go, make it its own module. Use the standard library, unless a protocol needs an SDK. Import nothing from
   `aminte/internal`. Build it as `aminte-NAME` and put it beside `{{.Exe}}`.
6. Test it with synthetic data. Fake the third party. Never use the owner's `life.db`.
7. Ask the owner before you build it, and before you run it on real data.

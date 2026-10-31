# Lifelog

`lifelog` is one Go binary that keeps a lifetime-scale, single-user SQLite database, `life.db` — a life log and
its backup: a journal of day pages, notes, the people in them, health readings, and the
files you keep — a recording's transcript, a PDF's text, a photo's small picture. It serves the file two ways: a hypermedia API (with an HTML face for
the browser) and a CLI. Everything that names a third party is an extension beside it: the MCP server, the setup of a
coding agent, Google Photos ([extensions](#extensions)).

The database it writes is specified independently of it, so another program — in any language — can read
`life.db`, or write its own:

- **[docs/](docs/README.md)** — the contract of `life.db`: goals, the canonical [schema.sql](docs/schema/schema.sql),
  the storage contract, the decision log, the query cookbook, research, and the process (issues, proposals, plans).
  It names no implementation; [building a writer](docs/guides/building-a-writer.md) is the way in for another one.
- **[tests/](tests/README.md)** — the validation suites: every executed claim of the docs, run against the schema and
  against this writer's own extraction and save. `go test ./tests`.
- **[API.md](API.md)** — the wire shape for client authors: every entity class and its properties, the errors, the
  pages, how to reach a server; a test keeps it equal to the code.
- **[AGENTS.md](AGENTS.md)** — the rules for anyone, human or agent, working in this repo.

`lifelog` implements the contract and never defines it: a rule found missing or ambiguous here is an
[issue](docs/issues/README.md) against the docs, not a fix in this code alone.

```
go build -o lifelog ./cmd/lifelog   # Go >= 1.27, no cgo
go build -C extensions/mcp -o ../../lifelog-mcp ./cmd/lifelog-mcp   # the MCP server, beside lifelog (an extension)
go build -C extensions/agentic -o ../../lifelog-agentic-init ./cmd/lifelog-agentic-init   # agentic-init, beside lifelog (an extension)
go build -C extensions/whatsapp -o ../../lifelog-whatsapp ./cmd/lifelog-whatsapp   # the WhatsApp agent, beside lifelog (an extension)
go build -C extensions/ticktick -o ../../lifelog-ticktick ./cmd/lifelog-ticktick   # TickTick, the first connected app, beside lifelog (an extension)
lifelog init                               # life.db beside the program; --db PATH or $LIFELOG_DB names another
lifelog agentic-init                       # AGENTS.md, skills and MCP configs here, for a coding agent opened here

lifelog capture "Ran 5k with [[Sam]] #running" --mood 4
lifelog do settings model_url=http://HOST/v1 model=ID   # the model of the agent: kept in life.settings.json, beside life.db
lifelog day --human
lifelog serve                              # http://127.0.0.1:7777 — open it in a browser; this machine only
lifelog serve --public --token-file ~/life/token   # for your other devices: every request needs the token
lifelog serve --public --token-file ~/life/token --with whatsapp   # the server with the WhatsApp agent beside it
lifelog mcp --agent lmstudio               # MCP on stdio (the extension lifelog-mcp); rows are written as agent:lmstudio
lifelog snapshot --snapshots ~/snapshots --human  # life-YYYY-MM-DD.db and its restore check
lifelog file memo.m4a --text memo.txt     # keep a file: its text, the original hashed and never stored
lifelog file IMG_0001.HEIC --preview IMG_0001.jpg   # a photo lifelog cannot read: send a JPEG of it
lifelog cover 12 lake.jpg                  # a new photo as page 12's cover; `lifelog cover 12 --title Lake.jpg` takes a kept one
```

To run `lifelog serve` as a system service with a nightly snapshot, see [deploy/](deploy/README.md): the unit files
and the snapshot script, as a reference to compare with the installed copies before you replace them.

An import is done by a program that loads rows itself ([imports](docs/contract/imports.md)), or by a coding agent
with the owner, in a conversation: the agent reads a source with its own tools and writes through the catalog's MCP
tools, and the owner says yes or no to each person ([plan 089](docs/plans/089-no-import-process.md)).
`lifelog agentic-init` prepares the folder for it ([plan 090](docs/plans/090-agentic-init.md)); then open Claude
Code, Codex, OpenCode or Pi in the folder.

```
lifelog snapshot                                   # the owner, before an import session
```

## Decisions

These are this application's own engineering decisions ([D14](docs/decisions/D14-ui-and-tools.md)).

- **The four rings** ([plan 135](docs/plans/135-extensions-outside-the-core.md)). A thing lives in the innermost ring
  that can hold it. Ring 0 is the file: `schema.sql` is the domain model, and every rule about the data is a CHECK or
  a trigger there. Ring 1 is the writer, `internal/core` and what it reads: transactions and only what SQLite cannot
  say. Ring 2 is the catalog and its surfaces, `internal/api` and `cmd/lifelog`: one catalog, one handler, the forms
  and the CLI's `do`, all generated from the catalog. Ring 3 is the extensions: one program `lifelog-NAME` each, a
  client of [API.md](API.md), run as `lifelog NAME`, in any language, and the only ring that names a third party
  ([the contract of an extension](extensions/README.md)); it may also serve a catalog that ring 2 merges into its own
  ([connected apps](#decisions)). Two tests of `cmd/lifelog` keep the rings: the core names
  no third party, and the core dials only its own server. The browser stays in ring 2: it draws the entity on the
  origin of the API, with the forms that post to it.

  ```mermaid
  flowchart TB
    subgraph R0["ring 0: the file"]
      S["schema.sql: the domain model and every rule about the data"]
    end
    subgraph R1["ring 1: the writer"]
      C["internal/core, db, text, photo, preview"]
    end
    subgraph R2["ring 2: the catalog and its surfaces"]
      K["internal/api and cmd/lifelog: the API, the browser, the CLI"]
    end
    subgraph R3["ring 3: extensions"]
      X["lifelog-mcp, lifelog-agentic-init, lifelog-gphotos, lifelog-whatsapp, lifelog-ticktick: clients of API.md; they may serve a catalog of their own"]
    end
    S --> C --> K --> X
  ```
- **Connected apps through the catalog** ([plan 154](docs/plans/154-connected-apps-through-the-catalog.md)). The
  interface between `lifelog` and an app is the Siren catalog and nothing else. An extension that `serve --with NAME`
  runs may serve its own `GET /actions` on the loopback address `LIFELOG_LISTEN`. The server puts its actions in
  `GET /actions` as `NAME-action` and forwards `/with/NAME/...` to it, as the writer of the request. So the CLI's `do`,
  the MCP tools and the browser carry an app with no further work. The core names no app and no domain: the code of
  a third party stays in the connector. A ring 3 program may serve a catalog that ring 2 aggregates. Only two files of
  the core may call `client.Remote`: `cmd/lifelog/with.go` and `internal/api/with.go`, besides `cmd/lifelog/main.go`
  and the test helper. The protocol is in [the contract of an extension](extensions/README.md#a-catalog-of-its-own),
  and the wire shape is in [API.md](API.md#connected-apps).
- **One handler, three surfaces.** `internal/api` is the only thing the surfaces call. `serve` mounts it on a
  socket; the CLI calls it in-process (`client`), or a running server with `--url`. The MCP server is the
  extension `lifelog-mcp` ([extensions/mcp](extensions/mcp/README.md)): a client of the HTTP API, run as
  `lifelog mcp`, which gives it a temporary server on the file.
  Writes go through `internal/core` only: no generic UPDATE or DELETE exists anywhere.
- **Server shutdown drains admitted requests.** An interrupt or a SIGTERM closes the listener, then allows ten seconds for active
  requests to finish before closing their connections. Shutdown completes before the database closes; expiration
  is reported as an error.
- **Hypermedia (HATEOAS).** Every response is a Siren entity: properties, links, and the actions legal on that
  resource, with their fields prefilled (a page's `save-body` carries its body and `version`). `GET /actions` is
  the catalog; the CLI's `do` and the MCP tools are generated from it, so a new action needs no client change. A
  browser cannot fill a path from a form, so `/actions` draws an action with path fields as a form of those fields,
  and `GET /actions?new=<action>&id=12` answers with that action's href filled in and its form open: every action is
  reachable in a browser ([plan 131](docs/plans/131-the-pareto-pass.md)). A file field takes the path of a local file
  in the client (the CLI's `do`, the MCP tools), which streams it; a browser sends the file it picked.
  An error is an entity too: `status`, a `code` a client branches on (`stale_version`, `exists`, `not_found`, `snapshot_refused`, `restore_check_failed`,
  `invalid`, `cross_origin`, `token_required`, or the status's general name) and the message. A fault of the server (500
  `internal`) says only "internal error": the server's log has the cause. The lists `/days`,
  `/browse` and `/search` take `limit` (1–500) and `offset` and link `next` and `prev`
  ([plan 079](docs/plans/079-client-contract-hygiene.md)).
- **The browser gets the same entity.** `Accept: text/html` renders it with a template per class (day, page,
  person, metric, search, lists; `internal/api/view/html/`, embedded) or a generic one for any other. A
  template reads only the properties the JSON carries and places only the actions the entity offers, so a form
  shows where the action is legal and nowhere else. A body is CommonMark with goldmark's safe defaults (raw HTML
  omitted), its `[[wikilinks]]` and `#tags` linked. A browser form posts and is answered 303 to the resource it
  changed, so a reload never posts twice. A refused one is answered with the error page: the alert, the text it
  sent and a link Back to the page it came from ([plan 131](docs/plans/131-the-pareto-pass.md)). A form may name the
  page to return to in `back` (a path on this server, not a field of any action): a Done button on a list returns to
  the list. The path may end in an id, which names the row that the page opens at. A value that is not a plain path is ignored, and a JSON client never follows it
  ([plan 091](docs/plans/091-the-browser-for-daily-use.md)).
- **The page is semantic HTML, and the catalog says how to draw it.** A field is a `label` around its input (a flag,
  the options 0 and 1 in a field that the action does not require, one checkbox that sends 1 or nothing; a field of 2
  to 5 short options a `fieldset` of radio buttons; a `pages` field with `options` a text input with a `datalist` of
  those titles, so the reader can also type a new title), a card is an `article`, a callout an `aside`, an error a
  `role="alert"`, a series a `table` with a header row, a write's result and the search an open or a modal `dialog`,
  the phone's sheet a popover; a field the browser refuses once the owner has typed in it (`:user-invalid`) shows the
  same danger edge as one the server refused (`aria-invalid`), and the select is the app's own where the browser lets
  a sheet draw it (`appearance: base-select`); no id or class is generated for a template to find, but the id of a
  `datalist`, which the `list` of its input names (the form's id, or else the action's name, then the field's name).
  What a template cannot know comes from the entity: `danger` marks a destructive action's button, `owner` an
  owner-only one, `rows` a textarea's height, `options` a field's choices, `pages` a field whose value is a page's
  title and an error's `code` its first words, so no view decides any of them by an action's or a field's name.
- **The browser for daily use** ([plan 091](docs/plans/091-the-browser-for-daily-use.md)). The root is the entry point
  of the API, with the five newest day pages; a browser's `GET /` is answered with the page of today
  ([plan 129](docs/plans/129-the-ux-pass.md)), which holds the same list in its margin. A day carries its mood and the `save-body` of its day page, so the day view
  edits in place. These are properties and actions of the entities, so every client gets them ([API.md](API.md)). `/months/{YYYY-MM}` is the calendar of a month (each day, whether it has a
  page, its first line, its cover, its mood and its readings; the day view holds the rest), and the catalog action
  `open-day` goes to a day by its date, or to today with no date. Every field of the catalog whose type is `date`
  reads the word `today` as the server's local day. The handlers read today from one clock, which a test fixes.
  `/years/{YYYY}` is the year (twelve months);
  the People list names each person with the pages that mention it and the day of the last one
  ([plan 097](docs/plans/097-the-almanac-reflects.md)). A person's row starts with a round picture, its cover,
  else the icon of a person.
- **Period pages** ([D30](docs/decisions/D30-period-pages.md)). A week (`2026-W43`), a month, a quarter and a year may
  have a page: a plain page whose title is the key. `/weeks/{week}`, `/months/{m}` and `/years/{y}` show the page of
  their period. A period with no page offers `create-page` with its key as the title, the one action that makes it.
- **A cover for every page** ([plan 127](docs/plans/127-a-cover-for-every-page.md)). A page's cover is the first live
  file with a preview that its body embeds (`![[Ana.jpg]]`); a day page's first photo is its cover with no action. A
  file page has none: its own picture stands for it. The browser draws the cover as a square at the head of the
  margin of the page and the day (first on a phone), with one link to the picker under it, and leaves that embed out
  of the body; a Browse row
  starts with it as a small square (a person's is round), and the row's `picture` gives its href. The calendar draws
  a day's cover behind its square cell, under a veil of the parchment that keeps the words readable. The schema has no link for it, by the owner's choice (2026-10-15, again
  2026-10-17): an embed is how any page shows a picture, so every surface can set it. `set-cover` sets it: a new
  photo (the browser, and `lifelog cover PAGE-ID PHOTO`) or a kept picture by its title (`lifelog cover PAGE-ID
  --title T`, and an agent by either, a new photo by its path).
  `GET /pages/{id}/cover` is the picker, the one way to set a cover in a browser: the kept pictures as tiles, each a
  form of its title (one the body embeds already trades places with the cover's embed), and the form of a new photo.
  The new photo goes in no day page.
- **The app shell: the Almanac design system** ([plan 092](docs/plans/092-the-app-shell.md)). Every page a browser
  gets is in one shell: a sidebar with the brand and today's date, the search, New, the sections of every day (Today,
  Calendar, Metrics and Browse) and the owner's links. This month is on Calendar only: the sidebar does not draw it
  a second time. Under 900 px it is a phone's layout
  ([plan 093](docs/plans/093-words-phone-and-today.md)): a top bar (the brand, the date, Search, New), a bottom bar of
  the sections of every day and More, and the rest as a sheet over the page (`#sections`): a popover that More
  opens and Close, Escape or a tap outside closes, with no script. The chat column (`#chat`, [plan 146](docs/plans/146-settings-and-the-chat-column.md)) is a popover the same way: a panel at the right edge of a wide screen and a sheet under 900 px, opened by the Chat button (at the corner, or in the phone's bottom bar), with today's turns and the composer; the conversation view has the column's content at full size and no column. Inside the page a layout asks the width of its column (`@container column`), not of the
  screen, so a day beside an open sheet lays out as in a narrow window; the title, the leaf and the sheet's padding
  scale with the column by `clamp()`, with no step at a breakpoint
  ([plan 108](docs/plans/108-the-browser-made-current.md)). The sidebar is the one part of a page that is not the
  page's entity: it reads the clock, for today's date, and the Today strip
  ([plan 137](docs/plans/137-pixel-wood-and-the-focus-pass.md)). The strip says "2 of 4 done" over the entry, the mood
  and the chips. A chip is a daily metric with no unit ([D31](docs/decisions/D31-daily-metrics.md)). Each chip is a link
  to the form that does the thing. The strip also shows the days written in a row ([the streak](docs/cookbook/streaks.md)).
  On a phone the strip is in the sheet. While the entry box has the focus, the rest of the page steps back (writing
  mode). A page drawn before the handler
  (the login, a refusal of the token) has no sidebar, so it shows nothing of the database. The look is the Almanac
  bound in Hearthwood, the materials of the owner's Balaur design system
  ([plan 096](docs/plans/096-bound-in-hearthwood.md)): the page is a slab of oak on a floor, centred in a
  frame of 1512 px on a wide screen, the sidebar and the phone's bars are wood, and everything written is parchment
  with ink; square corners, 2 px edges, bevels and 3 px hard drops, no
  blur and no gradient (the glows of the firelight floor are the one exception); the floor is the owner's choice
  (Settings: boards, oak, firelight or embers), and the last two move only when the system does not ask for less motion; gold marks where the owner is and what is today, ember is the one loud button of a screen,
  teal is a link. Words are in Atkinson Hyperlegible, every day, time and number in IBM Plex Mono,
  and a page's title, the leaf's number and a reading's value in Jersey 15; dark is the identity and light the
  daylight study of the same room, by `prefers-color-scheme`. Its tokens are custom properties of `base.css`; a
  material sets its own text through aliases (`--text`, `--text-link`, …), and a test holds every text pair at
  4.5:1 and every edge and the focus ring at 3:1 in both themes. The motions, all off under reduced motion: a press
  sinks; the step to the previous or next day moves the leaf and cross-fades the sheet through a cross-document view
  transition ([plan 097](docs/plans/097-the-almanac-reflects.md)); the search dialog and the phone's sheet
  enter and leave by `@starting-style` and a discrete transition; the phone's top bar takes its drop once the
  page has scrolled under it, by a scroll-driven animation
  ([plan 108](docs/plans/108-the-browser-made-current.md)).

  Do and don't:
  - Show one loud button a screen.
  - Show no streak, no chain and no badge for consistency.
  - Write in sentence case. Use no capitals for effect.
  - Use mono for days, times and numbers only.
  - Let a material set its own text through the aliases.
  - Use no colour outside the one token block. Its `light-dark()` pairs are held by the contrast test at 4.5:1, with
    the APCA Lc logged beside it.
  - Draw a value by its type, never by its name.
  - Make every form work with no script.
  - Answer a refused form with the error page: the alert first, the text it sent, and Back.
  - Make every empty state name the way forward.

  Each part
  of an entity has one component: the title and the `prev`/`next` links a page header, properties facts and
  panels, sub-entities rows, an action with no visible field a button, one with
  fields a button that opens its form, an error an alert, and a write's `result` a toast at the top of the page,
  which stays until the next page because it may name what the write did not do. A day and Home (the page of today) are a calendar
  leaf, a sheet and its margin ([plan 095](docs/plans/095-the-leaf-and-its-margin.md)): the leaf (the month on a
  band, the day's number in mono) heads the page; the sheet holds what the page is for (the journal, then Capture);
  the margin holds what the page asks of the owner (the readings), and comes after the
  sheet on a narrow screen. A page and a list use the same sheet. One button a screen is filled:
  the main write. A view can fold an
  action's optional fields under "N more fields", inside the form, so a browser sends them with no script.
  A value is drawn by its type, never by its property's name ([plan 093](docs/plans/093-words-phone-and-today.md)):
  a day in words near today ("yesterday · Sun 11 Oct"), an instant in the clock of the server's day ("today, 09:48",
  the exact instant kept in its `<time>` element), an object of the generic view as facts, and a write's result in
  sentences by the result shapes of API.md. A day's reading tile is a metric read that day and its last value.
  `GET /manifest.webmanifest` and the icons (`/assets/icon.svg`, and `/assets/icon-192.png` and
  `/assets/icon-512.png` for Android) make the app installable: two shortcuts (Today, Add to today) and a share
  target that posts shared text into Capture (`POST /days/today/capture`). There is no service worker, since the
  server is the owner's own. A public listener serves the manifest, the icons and the four pictures of `internal/pixelart` (`GET` and `HEAD`) without the
  token, because a browser fetches them without its cookie and they hold nothing of the database. The fonts stay
  behind the token.
- **One script, and every form works without it** ([plan 108](docs/plans/108-the-browser-made-current.md); the
  owner's rule of 2026-10-14: as native as HTML can be, even at the cost of a small convenience;
  [plan 146](docs/plans/146-settings-and-the-chat-column.md)). A page carries its two sheets and at most one
  `script` element: `/assets/chat.js`, loaded with `defer` on every page of the shell, under 80 lines, with
  no framework and no build step. A test holds both. It does one thing on every page: when a dialog of the ⋯ menu
  closes, it gives the focus back to the ⋯ button (the button that opened the dialog is in a popover that is hidden
  again), or, for a form that the page's link asked for, to its head button. It does three things for the column. While
  the last turn of the column is the owner's, it asks `GET /days/today/conversation` every 3 seconds and, when the
  `version` differs from the column's `data-version`, fetches the current page and swaps the column's content. It
  posts the composer with `fetch` and swaps the column from the answer, so the page does not reload. It keeps the open
  state of the column in `localStorage` and opens it again on load. Any error leaves the column as it is; a request
  carries the cookie of a public listener (`credentials: 'same-origin'`). Without the script every form still posts and
  redirects, and the column opens by its button (a popover); the page that comes back shows the new turn when the
  column is opened, and the reply shows on the next load. What the old script once did ([plan 091](docs/plans/091-the-browser-for-daily-use.md),
  [plan 094](docs/plans/094-the-command-list.md)), the browser does by itself: the **search dialog** is a `dialog`
  the server draws on every page of the shell, with the search form inside, the one search box of the shell: the
  rail's Search and the top bar's open it by their command (`commandfor`), and Escape or a tap outside closes it
  (`closedby="any"`); `/search` lists the pages whose title
  starts with the words before the pages whose words match, so a page is one search away by its title. The phone's
  sheet of sections is a popover; the toast is an open `dialog` in the flow of the page, which Escape and its own
  Close close. The keys are the browser's **access keys**, with its own modifier (Alt in Chromium, Alt+Shift in
  Firefox, Ctrl+Alt on a Mac): `k` the search, `n` New, `t` Today, `c` the capture, `[` and `]` the previous and
  next page, and `l` the Lock button when the lock is on; the footer and the dialog say so. A form that writes a body offers **Preview**, a second button that
  sends it to `POST /render` in a new tab (`formaction`, `formtarget`), where the body shows as its page will show
  it, and the form keeps its words. The list that deletes several rows counts the checked boxes on its button by a
  CSS counter. A page of the shell carries the `Speculation-Rules` header, so Chromium draws the previous and next page on
  hover before the click, with no element on the page and nothing on another engine. `Vary: Accept` says that one URL has
  two representations ([plan 149](docs/plans/149-the-browser-on-the-current-platform.md)). **Dropped, by the owner's word:** the names after `[[`, the live preview as one types, Ctrl+Enter,
  Escape to leave a field, the question before the browser leaves text not sent, the second press of a danger
  button (a boxed danger already takes two presses, and a tombstone has Restore), and the command list's ranking of
  this page's actions.
  `POST /render` is presentation, not a capability: no catalog action names it and it answers HTML only, so it, the
  fonts, the manifest, the icon, the world outline and the pictures (`GET /pages/{id}/preview`, `GET /previews`) are the
  routes the CLI and the MCP server do not carry. **Rejected:** a separate web client. It would be a second
  implementation of every view to keep equal to the catalog, it would hold the token where JavaScript can read it,
  and it would bring a toolchain that changes every year.
- **The stylesheet is this application's own: two embedded sheets, three embedded faces, five embedded pictures (the
  wood tile, the ground tile, the sparks of the embers floor, the sprite sheet and the icon sheet, drawn by `internal/pixelart`), no vendored framework and no CDN.** `base.css` holds the tokens and styles ordinary HTML by element — type, colour, forms, tables, buttons, a
  print sheet, light and dark — and `app.css` only what this app's own vocabulary needs (the shell and its
  components, `.wikilink`, `.chart`); a test keeps the app's names out of the base sheet. The two sheets are one
  cascade in four layers, `tokens`, `base`, `components` and `utilities`, declared once at the top of `base.css`
  and shared by the two `style` elements, and every rule is in logical properties; a test holds both
  ([plan 108](docs/plans/108-the-browser-made-current.md)). The three faces are open
  fonts (SIL OFL 1.1), latin and latin-ext, embedded with their licences and served by exact name at
  `GET /assets/fonts/{file}` with a cache of a year; like `/render`, the route is presentation and not in the catalog.
  Nothing is fetched from another host, so a page looks the same offline in fifty years. The New menu and the chat screen's menu hang from their buttons by anchor positioning, with the centred box as the fallback ([plan 149](docs/plans/149-the-browser-on-the-current-platform.md)). **Rejected:** a vendored classless sheet (Simple.css, Pico). Its
  `aside` is a floated sidebar and ours is a callout in flow, its `section` a bordered band, its `h2` 2.6rem where
  ours labels a section, its tables bordered and zebra-striped where ours are dense, its form controls carry blanket
  margins where ours sit two to a row, and its `body` is a 45rem grid: keeping one meant overriding most of it, and
  re-pinning a file by hand at every upstream change.
- **Provenance per surface.** `source` is `cli`, `api`, `ui` (a browser form) or `agent:<name>` (MCP);
  the `Lifelog-Source` header sets it.
- **Optimistic saves.** A body save sends the `version` (the decimal `entities.revision` token) it read; a newer one is a 409.
- **The readings of a metric** ([plan 100](docs/plans/100-one-page-a-metric.md), [plan 131](docs/plans/131-the-pareto-pass.md), [plan 144](docs/plans/144-one-view-for-a-metric.md)).
  A metric has one view in the browser, `/metrics/{name}`: its page (the note, categories, links, backlinks and
  other actions; a metric is a page, D27) and its readings (Add a reading, Ask for it every day, then the readings
  newest first in a fold a year). The readings are `(from, to]`; with no `from`, every admitted day through `to`, on
  every surface. The All readings link leads a window back to the whole. The series carries the page, so the API, the
  CLI and the MCP tools read both in one answer. A browser request of a live metric's page (`/pages/{id}`) is
  redirected to the series with its query; JSON is not, and the page of a deleted metric stays.
- **Connections** ([connection setup](docs/contract/connections.md)): one writing connection with the pragmas
  set by DSN and read back by a connection hook (a wrong value refuses the connection), `SQLITE_DBCONFIG_DEFENSIVE`,
  `BEGIN IMMEDIATE` via `_txlock=immediate`, the 3.51.3 floor checked; readers use `mode=ro` with `trusted_schema=OFF`, read back the same way.
- **Unicode.** Registry name keys use `x/text`'s full case fold; the `Cn` rule uses a pinned Unicode 15.0 assigned table,
  not the tables Go ships, so the titles this writer accepts do not change with a Go upgrade.
- **One transaction type.** Every write is a method on `core.Tx`; an operation alone is one transaction, and an
  operation that writes several rows (`file-pages`) runs several of the same methods in one. Nothing writes
  around them.
- **The folder of the program** ([plan 090](docs/plans/090-agentic-init.md)). Without `--db`, `LIFELOG_DB` or
  `--url`, a command uses the `life.db` beside the executable, and `lifelog init` with no path makes it there; a
  missing file is still refused. `lifelog agentic-init [--force]` is the extension `lifelog-agentic-init`
  ([extensions/agentic](extensions/agentic/README.md)). It writes into that folder, from templates embedded in the
  extension: `AGENTS.md` (the folder, the modes, the tools, the rules and a privacy note), a `CLAUDE.md` that
  imports it, three skills in `.agents/skills/` and `.claude/skills/`, and the MCP config of Claude Code
  (`.mcp.json`), Codex (`.codex/config.toml`), OpenCode (`opencode.json`) and Pi (`.pi/mcp.json`), each starting
  `lifelog mcp --agent NAME` on that `life.db` with absolute paths. A file with another text is kept unless
  `--force`; run it again after the folder moves. Like `init`, it is a setup command of the machine, not a catalog
  action: it writes files beside the program.
- **Owner-only actions.** The actions that the catalog marks `owner` are
  refused to an `agent:*` or `import:*` writer and are never MCP tools. The table [every action on every surface](#every-action-on-every-surface)
  lists each one (MCP column: "none, the owner's"), and a test holds the table equal to the catalog. The full-text check of `integrity-check` holds the write
  lock while it runs, so an agent must not start it. `rebuild-search` (`POST /search/rebuild`) is the repair of the
  full-text index, which the check finds: the FTS5 `'rebuild'` in one write. `resync-links` (`POST /links/resync`)
  rewrites the links of every live page whose links differ from its body (`link_drift`), by the code of a body save
  ([integrity checks](docs/contract/integrity-checks.md)). `GET /integrity` offers the two repairs, so the owner
  reads what needs them and runs them on one page. `GET /settings` and `GET /snapshots/{name}` are owner-only too, and are the owner-only reads that are not actions. An agent that imports with the owner asks for them in the conversation.
- **No conversation inside the writer** ([plan 116](docs/plans/116-strip-to-the-core.md), [RFC 0015](docs/rfcs/0015-no-conversation.md)).
  The owner's decision (2026-10-15): `ask` and the voice note are removed, with the model and transcriber
  endpoints they needed. The core calls no model. The owner talks to `life.db` through a coding agent's MCP tools, and
  a recording is kept as a file with its text ([D9](docs/decisions/D09-binary-files.md)). A conversation is a plain
  page that an agent writes to (the decision "The conversation of a day", below).
- **Renames select a preferred name on the same identity**, retaining direct aliases as
  [titles and wikilinks](docs/contract/titles-and-wikilinks.md) ("Renames") requires and
  [rename a page](docs/cookbook/rename-a-page.md) writes; `lifelog rename` and the API's `rename` action run it.
- **The schema is embedded.** `schema.go` at the root embeds `docs/schema/schema.sql`, and `internal/db` reads it; a change to the DDL
  is live in the binary at the next build. `lifelog init` refuses an existing file; `Open` refuses a file without Lifelog's
  `application_id`.
- **Snapshots are the owner's, from any surface** ([take a snapshot](docs/cookbook/take-a-snapshot.md),
  [plan 082](docs/plans/082-snapshot-in-the-catalog.md)). `snapshot` is an owner-only action of the catalog
  (`POST /snapshots`): refused to an `agent:*` or `import:*` writer, never an MCP tool, offered at the root to the owner, and in a browser by the rail's Snapshot link (`/actions?new=snapshot`). It takes
  no path: the file lands in the folder `serve --snapshots DIR` names, or beside `life.db`, named by the local day
  and never overwritten; it refuses a folder inside a git work tree, and its restore check opens the snapshot with
  `Close` skipping `PRAGMA optimize`. The answer carries the path, the check and the link `download`:
  `GET /snapshots/{name}` sends the file, so a browser on another device keeps a copy of `life.db`. It sends only a
  good snapshot of the folder by its name (opened through `os.Root`), never the live file; the toast of the answer
  says "Download it", and Settings links to the snapshot form under Backup. `lifelog snapshot [--snapshots DIR]` is
  the action in-process, `--snapshots` choosing the folder, as it does for `serve`; with `--url` the server's folder is
  the one, and `--snapshots` is refused.
  Restoring stays the manual procedure of the cookbook.
- **The settings file** ([plan 146](docs/plans/146-settings-and-the-chat-column.md)). The settings of the application
  are a JSON file beside the database, `life.settings.json` for `life.db` (the path with `.db` cut and
  `.settings.json` added), mode 0600, written through a temporary file and a rename, and never in git (`.gitignore`). They are not in `life.db`: the
  model key is a secret, and a snapshot copies the database. They are not in `docs/` either: they belong to this
  application, not to the contract. Now they are `model_url` (the base URL of a server that speaks the
  chat-completions protocol, ending in `/v1`), `model` (the model id), `model_key` (a bearer credential, optional), `owner_number` (the digits of the owner's phone number with the
  country code: the one number that the agent on the phone answers; a value that is not digits is 422) and `ground` (the floor under the
  pages of the browser: `boards`, `oak`, `firelight` or `embers`; empty is `boards`; another value is 422), `lock_after` (the idle minutes of the lock: `off`, `5`, `10`, `15`, `30` or `60`; empty is `off`) and `lock_pin` (4 to 8 digits, else 422; the file keeps only the salted PBKDF2 hash, `lock_pin_hash`; no read shows the PIN or the hash, and an empty `lock_pin` keeps the stored one; `lock_after` other than `off` with no PIN is 422; the lock is read from the file at each request); a setting is added when a feature needs it. The action `settings` (`POST /settings`) and the resource
  `GET /settings` are the owner's: refused to an `agent:*` or `import:*` writer, and the action is never an MCP tool.
  `GET /actions` offers `settings` with no value, because any writer reads the catalog; only `GET /settings` fills
  the values, the key too, so the owner's form sends the key back as it is. A field of the type `password` is drawn
  as one. A field left out of `settings` stays, and an empty one clears the setting. A store with no settings path (a
  test) refuses a read and a write. The core calls no model: the agent reads the settings.
- **The conversation of a day** ([plan 146](docs/plans/146-settings-and-the-chat-column.md), after
  [plan 145](docs/plans/145-whatsapp.md)). The conversation of a day is the plain page `Conversation YYYY-MM-DD`, filed
  in `Conversations`, with the first line `[[YYYY-MM-DD]]`. A turn runs from a marker line, `**Owner** (HH:MM): text` or
  `**lifelog** (HH:MM): text` (outside a fenced code block), to the next marker line, with the tool lines of the agent
  (`` - `tool` ``) just before its marker. The time is
  the server's local time. The marker may name the relay, `**Owner** (HH:MM, via whatsapp): `: `say` writes it when the
  request names `via`, and the turn carries it as `via` (and the addresses of its tool lines as `refs`). [API.md](API.md#conversation) states the format for an agent in any language, and
  `internal/api/conversation.go` holds it in this program. `say` (`POST /days/{day}/say`, `today` allowed) adds the
  owner's turn and makes the page on the first turn of the day, in one write; it is for today only (another day is
  422, and that day's conversation offers no `say`); it is the owner's alone: an
  `agent:*` or `import:*` writer is refused (403), and it is never an MCP tool, so no agent speaks as the owner. The
  WhatsApp extension relays the owner's words as the owner, with `via=whatsapp`; the paragraph then reads `**Owner** (HH:MM, via whatsapp): `. `GET /days/{day}/conversation` gives the turns, parsed from
  the body, with the page's properties, links and actions when the page exists (as a series carries a metric's page,
  [plan 144](docs/plans/144-one-view-for-a-metric.md)), and `say`. A browser's `GET /pages/{id}` of a live conversation
  page is a 303 to it, so there is one view of a conversation. A live conversation page keeps its title: it offers no
  `rename` or `promote`, and `rename` to such a title is 409. The agent (`lifelog whatsapp`) watches today's page
  and answers on it; the core calls no model. In the browser, a right column on every page shows today's turns and the
  composer, and one script, `/assets/chat.js`, polls for the answer while the last turn is the owner's and lets Enter send (it also returns the focus to the ⋯ button when a menu dialog closes); every form
  works without it. The conversation view is the chat screen, not a page ([plan 148](docs/plans/148-the-chat-screen.md)): a day bar with the chips of the last days and a menu (a popover) of the page's own actions, links and backlinks, the thread, and the composer
  at the bottom. The conversation names each address in the tool lines of a turn as a `ref` (at most six a turn), and `?embed=ref` ([embedding](API.md#embedding)) replaces it by the entity that the server's own handler answers (the newest 36 addresses of the screen and the newest six of the column; the older draw their link; the request carries the source `agent:embed`, so an owner-only read such as `/settings` or `/integrity` is refused and draws its link)
  and the browser draws the entity that answers as a card by its class (a day, a page, a metric, a search); the column draws the same cards on one line.
- **One delete, of one row or several** ([plan 107](docs/plans/107-delete-several-at-once.md),
  [plan 131](docs/plans/131-the-pareto-pass.md)). `tombstone-pages` (`POST /pages/tombstone`) takes `ids`, separated
  by spaces or commas, and tombstones them all in one write or none: a missing id is a 404 that writes nothing. A page
  offers it with its own id as Delete, which returns to the page and its Restore. Browse offers it on pages (the ghost
  pages too), media and documents, so the browser draws a box on each row or tile and one danger button, which counts
  the checked boxes (a CSS counter, no script) and returns to the list with the count; the CLI's `do` and the MCP
  tools carry it as any action. It is a danger, not the owner's alone, and a revive undoes it.
- **Metrics as cards, filed in groups** ([plan 112](docs/plans/112-metrics-as-cards.md)). `/metrics` draws each
  group as a grid of cards, with a jump bar to the groups: a card is a metric's latest value, its day and the line
  of its newest 30 readings, muted when the latest is older than 60 days. Every card has the same four lines, so the
  cards of a row are of one height: the name, the value (a dash and the unit with no reading), the line (a dashed
  rule with none), and a foot with the day in short words; the whole day and the count of readings are the day's
  title ([plan 120](docs/plans/120-a-tidy-board.md)). `file-pages` (`POST /pages/file`) files several pages in a category page in one write, made when it is
  missing; `move` 1 takes them out of their other categories, and `from` takes them out of one category. The page's pick bar checks the cards to file. The CLI's
  `do` and the MCP tools carry it as any action; it is not the owner's alone, as `link` is not.
- **The stream, the zoom, a random thing and search by kind** ([plan 130](docs/plans/130-the-explore-pass.md)). The
  Calendar opens on the stream, `GET /days`: the days of the record as cards, the newest first, 30 a page, then Older
  days. A day is in the stream when it holds something: its day page, a page or a file with its day, or a current
  reading. A day after today is not in it. It pages by `limit` and `offset`, as every list. A card holds
  up to four plain lines of the journal, the mood, the cover only (a preview is 100–300 KB, so no other picture), the
  people as chips, the birthdays, and three pages, then a count. A day with no day page is a slim card
  with no words. Every read of a page of the stream takes its days as one set, not a window: 30 sparse cards can span
  decades. Days, Month and Year are the zoom of the Calendar: one row of chips, with Today, and the links `days`,
  `month` and `year`. `random-page` (`GET /pages/random`, `kind`) is a read that answers 303 to a thing picked at
  random, and with `kind=day` to a day: a quiet button on the stream and on Browse. `search` takes `kind` (day, page,
  person, file): the page draws Pages by title first, then the chips of the kinds, then the hits in groups by
  kind, each with its cover or the icon of its kind.
- **Browse** ([plan 117](docs/plans/117-one-browse-view.md), [plan 132](docs/plans/132-browse-the-notion-way.md)). A category is a page, and anything is filed in it by
  `part-of` (D26); the owner's word (2026-10-15) is "category" everywhere, a thing may be in several, and a category
  shows everything filed in it, whatever its type. `GET /browse` (the catalog action `browse`) is one list with two
  filters: the kind of its rows (Pages, People, Media, Documents, Metrics, each chip with its count; All
  inside a category, where it is the first chip and the default) and where it looks (Everything, what is filed
  directly in a category, or Not filed, which follows the kind). The category tree, by path with a count each, is one
  fold: beside the list and always open in a wide column, a closed fold above the chips on a phone, its summary
  naming the location. The head of the list is the location: a line of crumbs (Browse › Family › Cousins, each a link
  to that category's list, the last one plain; Everything draws none) and an `h1` that says Everything, the
  category's title, Not filed, Ghost pages or Deleted. A category adds the link Open its page. Each category row of
  the tree has a quiet + (New page in that category, a link to the Pages list with New page open), shown on hover or
  focus and always on a touch screen, and the foot of the tree has New category (it opens the Select bar of the pages: a page becomes a category when something is filed in it), beside Not filed and Deleted with its count. Each row
  shows the categories it is filed in as small muted badges (inside a category, the other categories), and has a
  quiet Move link, shown on hover or focus and always on touch, that opens the list with Select on and that row
  checked: a link to the one pick bar, not a second form. Each kind keeps its row and its order: a person
  has a face, the mentions and the last day; media are tiles, newest day first, with the picture a file keeps or the
  icon of its kind, one media kind by `media=`, and the count of each media kind in words; documents are the other
  files, as rows. The ghost pages are a filter of Pages, and so are the deleted (`deleted=1`), the newest
  deletion first, each row opening its page, where Restore is. The pick bar says N selected; its category box lists every category by path
  (Family › Cousins) and takes a new name; its Move box is checked inside a category. Its main button is Move to
  inside a category and Add to elsewhere. Inside a category a further button, Remove from here, takes the checked
  rows out of it. The bar also deletes pages, media and documents. After a write the browser returns to the quiet
  list, with no bar open. Metrics keeps its own board at `/metrics`. Days are not in Browse: a day is the time axis, found in the Calendar and by
  Search, and it meets the things through its mentions (the owner's word, 2026-10-15); a day page is never filed
  ([D26](docs/decisions/D26-metric-categories.md)). The sidebar has Today, Calendar,
  Metrics and Browse; a phone's bottom bar is the same four with More. A category page has one section, Filed here: the
  sub-categories first, then the rest by title, a type badge on what is not a page, and Open in Browse; its
  margin's Backlinks does not list `part-of`. One way files a thing ([plan 131](docs/plans/131-the-pareto-pass.md),
  [plan 132](docs/plans/132-browse-the-notion-way.md)): `file-pages` files a thing and, with `from`, takes it out
  (the x of a category badge, Remove from here in the pick bar). `create-page` takes an optional `category`: the page is made and filed there in one write, a missing
  category made as `file-pages` makes it, and a day as the category refuses the whole write (`entities_day_page`
  when the day has no page, `links_endpoint_types` when it has one). Browse in a category fills it in its New page,
  and a category page offers New page here.
  Rejected by the owner: exclusive membership, a manual order and a syntax in the body that files a page.
- **The ⋯ menu** ([plan 159](docs/plans/159-the-class-of-an-action.md)). A page draws the actions that no template
  places by name in one ⋯ menu in its head, by the `class` of the action: the `edit` actions first, then the others,
  then the `danger` actions last and apart. Each item opens a dialog with the action's form, with no script; a page
  with no such action draws no button. The head is one template for every view but the day. Under the title it has one
  quiet line, the meta line: a person's name and dates, a file's type and short SHA-256, then when the page was
  created; on a page of an app, its scalar properties. A title edit (`rename`) is the pencil beside the title, and each
  `create` action is a button of the head (the first is the primary one); the pencil and each button open a popover
  anchored under them, with the form in it. `?new=NAME` opens any of these forms as an open dialog.
- **Fold and find** ([plan 118](docs/plans/118-fold-and-find.md); the owner's choice, 2026-10-15). In the browser
  only, and with no script, Browse and the Metrics board fold; the JSON and the catalog do not change. The tree folds
  each category that has categories under it, open on the path to the current branch. A category is a row of the
  list with its badge, and a long list pages ([plan 131](docs/plans/131-the-pareto-pass.md)). The Metrics board
  folds each group and each category under it, and the line of a fold says its count and the day of its newest
  reading; the link to the category is the foot of the fold.
- **One way to make a thing** ([plan 114](docs/plans/114-one-way-to-do-a-thing.md)). The rail's New is a menu of the
  kinds; each kind is made on its list of Browse, whose header has its New button (a file on the media list, a
  metric on `/metrics`).
  `?new=<action>` opens that action's form on any page, and a query parameter that names a field fills it in. The
  catalog's titles say one verb for one meaning: New makes a thing, Add adds to one, Remove takes away, Restore
  brings back. The action names do not change. A reading is added on the day (the boxed Add a reading of the day;
  New's Reading opens it on the page of today) and on the metric's readings. A page shows its categories as badges,
  each with x, and + Add (`file-pages` with this page). The lists of Browse and the Metrics board act on several things the same way: Select shows the boxes, and one bar holds
  the count and a button for each action the list offers.
- **New metric** ([plan 113](docs/plans/113-new-metric-on-metrics.md)). New metric is one form in the header of
  `/metrics`, not on the page of today: a name, a unit (kg; a range such as 1-5 for a score; nothing for done or not
  done), a note, and Ask for it every day (a box for it on every day page; the metric's view changes it). The pick bar of the board files metrics in a category, made when it is new.
- **Files** ([keep a file](docs/cookbook/keep-a-file.md), D9). `add-file` (`POST /files`, `lifelog file`) streams
  the original through SHA-256 and drops it; nothing of it is stored. `internal/preview` makes every picture itself —
  from a JPEG, PNG or GIF, or a JPEG sent for a HEIC or a video frame: scaled to 1280 px, turned upright by EXIF,
  re-encoded as a JPEG at quality 80 (lower only when it would pass 1 MB) with no metadata (no GPS). The contract
  allows 1600 px ([D9](docs/decisions/D09-binary-files.md)), which `preview.Check` holds; 1280 px at quality 80 is
  this writer's choice, about half the bytes ([plan 128](docs/plans/128-smaller-pictures.md)). A picture kept
  before stays as it was: a JPEG encoded again loses more, and its original may be gone. An agent sends the path of a local file, which the MCP server reads and sends, or `sha256`, `mime` and the text. A
  file with no `title` takes its original's file name. `![[Title]]` renders the
  picture; `GET /pages/{id}/preview` and `GET /previews?title=` serve it. A photo's day comes from its EXIF
  (`internal/photo`: a JPEG's APP1, a HEIC's Exif item, read from the original's first megabyte); its position is
  never read ([D21](docs/decisions/D21-location-history.md), [plan 116](docs/plans/116-strip-to-the-core.md)).
  `lifelog file` keeps one file, with `--dry-run`. Unknown capture-local day follows [keep a file](docs/cookbook/keep-a-file.md).
- **`serve` is local-only unless the owner says `--public`.** Without the flag `serve` is what it was: `life.db`
  holds health data, and the API is for this machine. The default bind is `127.0.0.1:7777`; `--addr` accepts numeric
  loopback IPs or `localhost` with a numeric port (0 selects an ephemeral port, printed at startup), and nothing else.
  `localhost` binds to `127.0.0.1` without DNS resolution. Network requests must name a loopback IP or ASCII
  case-insensitive `localhost` at the actual listener port, with brackets for IPv6. An omitted HTTP port means 80, not
  the listener's port. Custom hostnames, service-name ports and proxy deployments are unsupported. Forwarded headers
  confer no trust; absolute-form request targets are refused. The network guard protects reads, previews and writes;
  in-process CLI dispatch is unchanged. Browser-origin protection inside it rejects cross-origin writes while
  keeping same-origin forms and non-browser clients that send no browser origin headers working. `--token-file`,
  `--allow-origin`, `--tls-cert` and `--tls-key` are refused without `--public`, never ignored.
  `--with NAME` (repeatable, [plan 147](docs/plans/147-serve-with-an-extension.md)) runs the extension `lifelog-NAME`
  as a child of the server and keeps it running; [the contract of an extension](extensions/README.md#run-by-serve)
  says how. The child gets a free loopback address in `LIFELOG_LISTEN`, and the server merges its catalog, if it serves
  one, into the catalog of the server ([connected apps](#decisions)). An unknown name refuses the command before the
  file opens, and `--with` with `--tls-cert` is refused.
- **`--public` opens the listener to other devices** ([plan 078](docs/plans/078-network-server.md)) and requires a
  **token**: `--token-file PATH` (one line of at least 32 characters; the command line never carries the secret) or
  `LIFELOG_TOKEN`; `--public` without one is refused before the listener opens. The default bind becomes `0.0.0.0:7777`
  and `--addr` may name any numeric IP of this machine, in brackets only when it is IPv6. Every request must carry `Authorization: Bearer <token>` or
  the cookie `/login` sets; one without is answered 401 with nothing of the database, and a browser is sent to
  `/login`. The `Host` check is dropped: it kept an unauthenticated local server from a DNS-rebinding page, and the
  token defeats that page on its own; absolute-form targets stay refused. A token holder is the owner: owner-only
  actions are still refused by `Lifelog-Source: agent:*` or `import:*`, not by the token. **Origins:** `--allow-origin ORIGIN`
  (repeatable, exact `scheme://host[:port]`) answers CORS preflights, adds the CORS headers for that origin and makes
  it a trusted origin for browser writes; any other origin is refused as before. **TLS:** `--tls-cert` and `--tls-key`
  serve HTTPS with the standard library; without them the token travels in clear, which is acceptable only on a
  private network (a VPN, a mesh such as Tailscale), and the startup line says so. A reverse proxy that terminates
  TLS works only because it forwards the bearer header unchanged. **The browser:** `GET /login` is the one page served
  without a credential; `POST /login` with the token sets `lifelog_session` (HttpOnly, SameSite=Strict, Secure over
  TLS, valid for one year so that an installed app stays logged in; a new token file ends it) and `POST /logout`
  ends the session and clears the cookie; cross-site forms with the cookie are what the origin guard refuses. The
  cookie holds a random id and its HMAC, not the token. The older cookie `lifelog_token` is not a credential
  any more: a browser that has only it logs in again, and any request that carries it gets it cleared. Every answer
  of a session carries `Cache-Control: no-store`, the pictures too; only `/assets/` keeps its cache. The CLI and
  the MCP server with `--url` send `LIFELOG_TOKEN` as a bearer header, and a bearer request never locks.
  **The lock** ([plan 160](docs/plans/160-the-lock.md)): when the settings `lock_after` and `lock_pin` are set, the
  listener locks a browser session that is idle for `lock_after` minutes plus one, and one that the owner locked with
  `POST /lock`. A locked session gets nothing of the database: an HTML `GET` goes to `/locked?next=PATH`, which asks
  for the PIN, and any other request is 401 `locked`. `next` is the path alone: a query, such as the words of a
  search, would show on the lock page. `POST /unlock` with the right PIN opens the session; the fifth
  wrong PIN in a row ends it, and the browser logs in with the token again. A try is counted before the PIN is
  compared, so requests sent at once or held open get no more tries together. A restart of the server locks every
  session, because the state of the sessions is in memory; it also gives a session that it does not know 5 tries
  again, and so does a day with no request of that session. The lock hides the pages, not the browser's own history:
  the titles of the pages that were open stay in the history and in the suggestions of the address bar. The server counts as activity a navigation (not a
  prefetch or a prerender), a form post and `POST /awake`; the poll of the chat column does not count. `/lock`,
  `/unlock`, `/locked` and `/awake` are routes of the listener beside `/login` and `/logout`, not actions of the
  catalog: they are about one browser, not about the data. `/lock` with the lock off is 409. The server cannot
  clear a screen by itself: the script `/assets/lock.js`, which the layout adds only when the lock is on, does it. It
  keeps the time of the last input of the owner in `localStorage`, shared by every tab, and a load of the page counts
  as input. It sends `POST /awake` at most once in 30 seconds while the owner types, clicks or scrolls. When the idle
  time is over, it sends `POST /lock` and replaces the page with the lock page, in every tab. The Lock button does
  the same at once, and the other tabs follow at their next check. A page without the script stays on the screen
  until its next request. The Lock button is in the foot of the More sheet of the rail, which is part of the rail on
  a wide screen, so the access key `l` works there; on a phone it is two taps away.

## Every action on every surface

The catalog (`GET /actions`) is the one list of what `lifelog` can do, and each action of it is reachable on every
surface ([AGENTS.md](AGENTS.md#go-implementation)): the API by its route, the CLI by `do ACTION field=value` (and a
shortcut that wraps it), the MCP server by a tool of the same name with `_` for `-`, and the browser by a form. Each
thing has one way to do it: one action, one route, one tool, one form where its resource is. A link may lead to that
form, but no second form, field, flag, route or alias does the same. `/actions` draws every action for a browser,
the ones with an id or a day too ([plan 131](docs/plans/131-the-pareto-pass.md)). The owner's actions are never MCP
tools and are refused to an `agent:*` or `import:*` writer. `TestTheTableOfTheSurfacesIsTheCatalog` keeps this
table equal to the catalog; `TestParityOfTheSurfaces` runs the actions on each surface.

| action | API | CLI | MCP tool | browser |
|---|---|---|---|---|
| `capture` | `POST /days/{day}/capture` | `do capture`, `capture` | `capture` | a day: Add to the day |
| `say` | `POST /days/{day}/say` | `do say` | none, the owner's | every page: the chat column; the rail: Chat |
| `create-page` | `POST /pages` | `do create-page` | `create_page` | Browse, Pages: New page, a button in the head of the list (a category page: New page here) |
| `save-body` | `POST /pages/{id}/body` | `do save-body` | `save_body` | a day or a page: Edit |
| `create-person` | `POST /people` | `do create-person` | `create_person` | Browse, People: New person, a button in the head of the list |
| `add-file` | `POST /files` | `do add-file`, `file` | `add_file` | Browse, Media: New file, a button in the head of the list |
| `set-cover` | `POST /pages/{id}/cover` | `do set-cover`, `cover` | `set_cover` | the picker of a day or a page (`/pages/{id}/cover`) |
| `promote` | `POST /pages/{id}/promote` | `do promote` | `promote` | the ⋯ menu of a page: Make it a person |
| `edit-person` | `POST /pages/{id}/person` | `do edit-person` | `edit_person` | the ⋯ menu of a person: Edit name and dates |
| `edit-metric` | `POST /pages/{id}/metric` | `do edit-metric` | `edit_metric` | a metric: Ask for it every day (the sheet of its view); the ⋯ menu of its page |
| `revive` | `POST /pages/{id}/revive` | `do revive` | `revive` | a deleted page (Browse, Deleted): Restore |
| `tombstone-pages` | `POST /pages/tombstone` | `do tombstone-pages` | `tombstone_pages` | the ⋯ menu of a page: Delete; the pick bar of a Browse list |
| `file-pages` | `POST /pages/file` | `do file-pages` | `file_pages` | a page: + Add and the x of a category badge; a Browse list: Move on a row, and the pick bar (Move to, Add to, Remove from here); the Metrics board: the pick bar |
| `record` | `POST /measurements` | `do record` | `record` | a day: Add a reading; a metric's readings: Add a reading |
| `correct` | `POST /measurements/{id}/correct` | `do correct` | `correct` | a reading: Correct |
| `retract` | `POST /measurements/{id}/retract` | `do retract` | `retract` | the ⋯ menu of a reading: Retract |
| `rename` | `POST /pages/{id}/rename` | `do rename`, `rename` | `rename` | the pencil beside the title of a page |
| `register-metric` | `POST /metrics` | `do register-metric` | none, the owner's | Metrics: New metric, a button in the head of the board |
| `settings` | `POST /settings` | `do settings` | none, the owner's | Settings |
| `integrity-check` | `GET /integrity` | `do integrity-check` | none, the owner's | the sidebar: Integrity checks |
| `search` | `GET /search` | `do search`, `search` | `search` | the sidebar: Search (the search dialog) |
| `browse` | `GET /browse` | `do browse` | `browse` | the sidebar: Browse |
| `find` | `GET /pages` | `do find`, `page` | `find` | a `[[wikilink]]` |
| `open-day` | `GET /days/go` | `do open-day`, `day` | `open_day` | Calendar: Go to a day |
| `random-page` | `GET /pages/random` | `do random-page` | `random_page` | Calendar: A random day; a Browse list: A random one |
| `snapshot` | `POST /snapshots` | `do snapshot`, `snapshot` | none, the owner's | the sidebar: Snapshot |
| `rebuild-search` | `POST /search/rebuild` | `do rebuild-search` | none, the owner's | Integrity checks, the ⋯ menu: Rebuild search |
| `resync-links` | `POST /links/resync` | `do resync-links` | none, the owner's | Integrity checks, the ⋯ menu: Resync links |

This table lists the core alone. The actions of a connected app ([plan 154](docs/plans/154-connected-apps-through-the-catalog.md))
are in the catalog while the app runs, and each is on every surface by its name `NAME-action`:

| surface | an action of the app NAME |
|---|---|
| API | `GET /actions` lists `NAME-action`; the route is `GET` or `POST /with/NAME/...` ([API.md](API.md#connected-apps)) |
| CLI | `do NAME-action field=value --url URL`, over a running `serve` |
| MCP | the tool `NAME_action` over `lifelog mcp --url URL`; none for an owner's action |
| browser | the form on `/actions` and on the pages of the app; the rail has one entry for each app that runs |

One child of `serve --with` may be the **task service** ([plan 158](docs/plans/158-one-task-service-behind-tasks.md)).
It is not an app: it has no `/with/NAME`, no rail entry and no `NAME-` prefix. `lifelog` draws its tasks under `/tasks`
(the rail has Tasks), and six actions are in the catalog while it runs: `add-task`, `edit-task`, `complete-task`,
`reopen-task`, `delete-task` (the owner's) and `sync-tasks`. Each is on every surface like any action. The routes, the
503 while no service runs and the protocol are in [API.md](API.md#tasks) and
[the contract of an extension](extensions/README.md#a-task-service).

The routes that are not actions are reads of a resource (`GET /`, `/days`, `/days/{day}`, `/weeks/{week}`, `/months/{m}`,
`/years/{y}`, `/days/{day}/conversation`, `/settings`, `/pages/{id}`, `/pages/{id}/cover`, `/metrics`, `/metrics/{name}`, `/measurements/{id}`, `/actions`),
which the CLI's `get` and the MCP tool `get` read, or the browser's presentation (`/render`, the fonts, the manifest,
the icon and the pictures), which the CLI and the MCP server do not carry.

## Layout

| path | what | test it at |
|---|---|---|
| `schema.go` (root) | `lifelog.Schema`: `docs/schema/schema.sql`, embedded so the binary carries the DDL; `internal/db` reads it | `internal/db` applies it in every test; `tests/` builds from the file itself |
| `cmd/lifelog` | the binary: `init`, `serve`, `get`, `do`, `actions` and the shortcuts (`file`, `snapshot` among them); the table of commands names what each shortcut wraps, and `TestTheTableOfTheSurfacesIsTheCatalog` holds the README table to it; `with.go` is the handshake of `serve --with`, and `testdata/withapp` is the connected app of its end-to-end test | the parity table `TestParityOfTheSurfaces` for a surface; `runArgs` with `testEnv` for a command line in process; `TestExecutableServeDrainsActiveRequest` for the process |
| `internal/db` | open, pragmas, `BEGIN IMMEDIATE`, the embedded schema; `dbtest` makes the fresh file of a test | `db.Init` and `db.Open` on a file in `t.TempDir()` |
| `text` | the title predicate, `title_key`, wikilink and `#tag` extraction, the parse of a day and an instant, the markers that a plain-text line cuts (`BlockMark`) and the marks of a match in a snippet of the search (`SnippetOpen`, `SnippetClose`); tested against the vectors of [titles and wikilinks](docs/contract/titles-and-wikilinks.md) | the pure functions with table tests, and the vectors of the contract page (`TestWikilinkVectors`, `TestTitleKeyVectors`) |
| `internal/core` | the cookbook's writes and reads; the save contract; metrics; renames; files | `core.Store` on `dbtest.New`, with a fixed `Clock` for a test of today |
| `internal/preview` | the picture a file page keeps: decode, scale to 1280 px (the contract allows 1600), EXIF orientation, a JPEG at quality 80 of at most 1 MB with no metadata | `preview.Make` and `preview.Check` on pictures that the test makes |
| `internal/photo` | what a photo's metadata says: the day and time taken and the orientation (JPEG and HEIC); `phototest` builds synthetic ones | `photo.Read` on the photos of `phototest`; `FuzzRead` |
| `internal/api` | the action catalog, the routes, Siren and HTML; the local and the `--public` network guards; `view` draws an entity as a page (the templates, the sheets and the fonts are in its `html/`) and imports no store; `apitest` makes a fresh handler and its clients for a test | `api.New` on a store, through `apitest.Fresh`, or `apitest.FreshOn` for a store with a fixed clock; a test inside package `api` cannot import `apitest` (an import cycle), so it makes its store with `dbtest.New`; the pure functions of the handler (`backOf`, `errorEntity`, `browseQueryOf`) with table tests; `view.Draw` and the pure functions of `view` with table tests |
| `siren` | the Siren types, the `Lifelog-Source` header, the error codes, the JSON writer and the addresses of the resources; standard library only; public: the Go extensions import it | the tests of the package and of `internal/api`, which both use it |
| `client` | the hypermedia client, transport only; public: the Go extensions import it; in-process or remote; `DoFiles` sends file parts from readers, and `Do` sends paths through it; of this module it imports only `siren`, so it builds no `core`, no `db` and no SQLite | `client.InProcess` and `client.Remote` on one handler: `TestTheTwoTransportsGiveEqualAnswers` |
| `app` | the package a connected app serves its catalog with: `Catalog`, `Listen`, `Fail`, `StateDir`; public; standard library and `siren`; it serves and never dials | its tests |
| `lifelogtest` | the test kit of the extensions: `lifelogtest.New(t, day)` makes a fresh synthetic file, the real handler over it with a fixed clock, and a server on a loopback socket; public, for `_test.go` files of an extension only | its own test |
| `client/tools` | the catalog as the functions of a model: a tool for each action that is not the owner's, its JSON Schema from the fields, the arguments flattened to form values; public: the MCP server and the chat agent build their lists from it | its table tests |
| `extensions/mcp` | its own module, `lifelog-mcp`: the catalog as MCP tools, over HTTP; it imports `lifelog/siren`, `lifelog/client` and `lifelog/client/tools` of the root module (a `replace` directive), and nothing of `lifelog/internal` | `mcp.New` on a fake catalog (`go test ./...` in the folder); `TestTheMCPServerOnTheRealLifelog` builds the real `lifelog` and `lifelog-mcp` and runs `lifelog mcp --agent test` |
| `extensions/whatsapp` | its own module, `lifelog-whatsapp`: an always-on agent; the owner writes on WhatsApp, a model answers through the action catalog; it imports `lifelog/siren`, `lifelog/client` and `lifelog/client/tools` of the root module (a `replace` directive), and nothing of `lifelog/internal` | the tests of its packages (`go test ./...` in the folder); `e2e_test.go` builds the real `lifelog` |
| `extensions/ticktick` | its own module, `lifelog-ticktick`: TickTick, the task service ([a task service](extensions/README.md#a-task-service)); it polls the done tasks into the day pages as `import:ticktick` and serves the task protocol, which `lifelog` draws under `/tasks`; it imports `lifelog/siren`, `lifelog/client` and `lifelog/app` of the root module (a `replace` directive), and nothing of `lifelog/internal` | the tests of its packages against a fake TickTick (`go test ./...` in the folder); `e2e_test.go` builds the real `lifelog` and runs `lifelog serve --with ticktick` |
| `extensions/agentic` | its own module, `lifelog-agentic`: `lifelog agentic-init`, the texts and MCP configs for a coding agent opened in the folder of the program; it reads `LIFELOG_EXE` and `LIFELOG_DB`, and imports nothing of `lifelog/internal` | `agentic.Files` and `agentic.Init` on the `Paths` of a temporary folder (`go test ./...` in the folder); `e2e_test.go` builds the real `lifelog`, `lifelog-mcp` and `lifelog-agentic-init` |
| `internal/scaletest` | the synthetic personas and the lifetime workload of `tools/lifescale`: generators, an oracle and the timed checks | `scaletest.Run` with the profile `small`, in the baseline |
| `tools/lifescale` | the lifetime and stress runner over `internal/scaletest` ([tests/README.md](tests/README.md)) | `internal/scaletest`: the runner has no test of its own |
| `internal/pixelart` | the drawings a gradient cannot be, deterministic: the wood tile of the rail, the ground tile of the page, the sprite sheet (the seal, the mood faces, the inkwell) and the icon sheet (14 glyphs of 10 by 10 pixels, used as a mask) ([plan 137](docs/plans/137-pixel-wood-and-the-focus-pass.md), [plan 138](docs/plans/138-pixel-art-elsewhere.md), [plan 139](docs/plans/139-pixel-icons.md)) | determinism, sizes, seams and a known pixel of each sprite; `view` compares each embedded file with its drawing pixel by pixel |
| `tools/pixelart` | writes the four pictures to `internal/api/view/html/` after a change to a drawing | the view's comparison: a stale file fails it |
| `tests` | the validation suites of the docs ([tests/README.md](tests/README.md)) | the contract suites `TestSuites` and the mutants `TestMutants`, on the DDL of `docs/` |

## Checks

`go vet ./... && go test ./...` — every test builds throwaway databases in a temporary folder:
the packages from the embedded schema (`schema.go`), the suites in `tests/` from `docs/schema/schema.sql` itself (and from broken
copies of the docs, the mutants). `go test -short ./...` skips the mutants.

### Request metadata limits

Action JSON and URL-encoded bodies are limited to 20 MiB of encoded data; JSON
must contain exactly one object. Multipart uploads allow up to 64 parts, 16 MiB
per text field and 16 MiB total text (including repeated fields; an unknown name is refused (422) before its bytes are read),
and at most one original and one preview part. A supplied preview may be up to
64 MiB. Over-limit requests return HTTP 413 without writing; after a JSON or
URL-encoded body over the limit, the server also closes an HTTP/1.1 connection (over
HTTP/2 it resets the stream, and the connection stays open). Originals are hashed as streams without a total size limit; originals above the 64 MiB picture
budget are kept without an automatically generated preview.

## Extensions

An extension is a client of the HTTP API ([API.md](API.md)). It lives in its own folder with its own Go module. It is
not built into `lifelog`, and `go build ./...` here does not build it.

The command line `lifelog NAME [ARGS...]` runs the program `lifelog-NAME`, found beside `lifelog` or on `PATH`. It gives
the program `LIFELOG_URL` (the API of the file), `LIFELOG_EXE` and `LIFELOG_DB`. The contract, for an author, is
[`extensions/README.md`](extensions/README.md).

- [`extensions/googlephotos`](extensions/googlephotos/README.md): a local page where the owner picks, for each day
  that has no cover, one photo of that day from Google Photos.
- [`extensions/mcp`](extensions/mcp/README.md): the MCP server, `lifelog mcp [--agent NAME]`. Every action of the
  catalog that is not the owner's is a tool.
- [`extensions/agentic`](extensions/agentic/README.md): `lifelog agentic-init [--force]`. It writes `AGENTS.md`, the
  skills and the MCP configs for a coding agent opened in the folder of the program.
- [`extensions/whatsapp`](extensions/whatsapp/README.md): an always-on agent on WhatsApp, `lifelog whatsapp`. It answers
  the owner's number through a model at an OpenAI-compatible endpoint and writes through the catalog as
  `agent:whatsapp`.
- [`extensions/ticktick`](extensions/ticktick/README.md): TickTick, the task service. What you finish in
  TickTick comes back as a `Done:` entry of its day, once. It is the task service:
  `lifelog` draws its tasks under `/tasks`, with `add-task` and the other actions of [Tasks](API.md#tasks). Run it with `lifelog serve --with ticktick`.

## Engineering references

These primary sources inform the application policies of [AGENTS.md](AGENTS.md); they do not override this
repository's database contract or turn optional techniques into universal Go rules.

- Go: [review comments](https://go.dev/wiki/CodeReviewComments), [module layout](https://go.dev/doc/modules/layout),
  [error wrapping and API boundaries](https://go.dev/blog/go1.13-errors), [context](https://pkg.go.dev/context).
- Testing: [test review comments](https://go.dev/wiki/TestComments), [testing APIs](https://pkg.go.dev/testing),
  [fuzzing](https://go.dev/doc/security/fuzz/), [synctest](https://pkg.go.dev/testing/synctest),
  [race detector and prerequisites](https://go.dev/doc/articles/race_detector).
- Tools: [Staticcheck](https://staticcheck.dev/docs/),
  [govulncheck and its limitations](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck).
- Measurement: [benchmark loops](https://go.dev/blog/testing-b-loop),
  [benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat), [profiling](https://go.dev/doc/diagnostics),
  [SQLite performance methodology](https://sqlite.org/cpu.html), [query-plan caveats](https://sqlite.org/eqp.html).
- Model-based testing: [stateful reference-model example](https://hypothesis.readthedocs.io/en/latest/stateful.html)
  (the technique, not a Python dependency).

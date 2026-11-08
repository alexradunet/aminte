# Implementation Plans

A plan is a dated record ([how a change happens](../process.md)): a change large enough to hand to another agent,
written as a self-contained brief with done criteria. It cites the docs as they were at the commit it names. One file
per plan, `NNN-short-slug.md`, numbered on from the last.

Earlier closed plans live in git history (`git log -- docs/plans`). The next plan number is **163**.

| Plan | Title | Priority | Effort | Depends on | Status |
|------|-------|----------|--------|------------|--------|
| [162](162-the-simple-architecture.md) | The simple architecture: one home for each thing, and the agent on its own | P2 | XL | owner word 2026-10-31; plan 161; `cf5d373` | DONE |
| [161](161-the-browser-as-an-extension.md) | The browser as an extension: a core with no HTML | P2 | XL | owner word 2026-10-31; plan 135; `4899fbe` | DONE |
| [160](160-the-lock.md) | The lock: a journal left open closes itself | P2 | M | owner word 2026-10-30; `ff0f1c7`; A `6d43b9e`, B `6f665f3`, review `c926d91` | DONE |
| [159](159-the-class-of-an-action.md) | The class of an action: the browser places an action by what it does, not by where Siren lists it | P2 | L | owner word 2026-10-30; plan 157; `5046b38`; A `016adb5`, C `57016f9`, B1 `8c865ad`, B2 `1124bdf` | DONE (live 2026-10-30 at `9d0e15f`; not seen in a browser) |
| [155](155-ticktick-the-first-connected-app.md) | TickTick: the first connected app, in this repo | P1 | M | owner word 2026-10-29; plan 154; `054138b`; live 2026-10-29 | DONE |
| [156](156-browse-the-tasks-through-the-catalog.md) | Browse the tasks of TickTick inside aminte, through the catalog | P2 | S | owner word 2026-10-29; plan 155; `b3d0a89`; built `9f1c584`; live 2026-10-30 | DONE |
| [157](157-a-task-manager-feel-through-the-catalog.md) | A task manager's feel, through the catalog | P2 | M | owner word 2026-10-30; plan 156; `875fc5d`; built 2026-10-30; live 2026-10-30 | DONE |
| [158](158-one-task-service-behind-tasks.md) | One task service behind Tasks | P2 | L | owner word 2026-10-30; plan 157; `53fcdd1` | DONE |
| [154](154-connected-apps-through-the-catalog.md) | Connected apps through the catalog: no plan lines, and an extension may serve a catalog the core aggregates | P1 | L | owner word 2026-10-29; issue 0092; RFC 0025; plan 147; `860e9b3`; live 2026-10-29 | DONE |
| [153](153-the-ground-in-settings.md) | The ground in Settings: four floors under the frame, two of them moving | P2 | M | owner word 2026-10-29; plan 152; `983a342`; live 2026-10-29 | DONE |
| [152](152-the-frame.md) | The frame: the app centred in a slab of oak on the floor | P2 | S | owner word 2026-10-29; plan 096; `49d2006`; live 2026-10-29 | DONE |
| [151](151-no-places.md) | No places: the type `place` and the link kind `at` go; a place is a plain page | P2 | M | owner word 2026-10-29; issue 0091; RFC 0024; `577fde7`; live 2026-10-29 | DONE |
| [150](150-one-code-set-one-catalog.md) | One code set, one catalog: the duplication between the surfaces goes | P1 | L | owner word 2026-10-28; plan 135; `bc46b76` | DONE |
| [149](149-the-browser-on-the-current-platform.md) | The browser on the current platform: HTML and CSS in place of a library | P2 | M | owner word 2026-10-28; plan 108; `f59554d` | DONE |
| [148](148-the-chat-screen.md) | The chat screen: the conversation as a chat, with a card for each thing the agent touched | P1 | L | owner word 2026-10-28; plan 146; `87a7133` | DONE |
| [147](147-serve-with-an-extension.md) | serve --with: the server runs an extension beside itself, so the agent and the API are one unit | P1 | M | owner word 2026-10-28; plan 146; `9cc622c` | DONE |
| [146](146-settings-and-the-chat-column.md) | Settings and the chat column: the model set in the app, and today's conversation on every page | P1 | L | owner word 2026-10-28; plan 145; `2d7315f` | DONE |
| [145](145-whatsapp.md) | WhatsApp: an always-on agent outside the core, a client of the API, reached from the owner's phone | P2 | M | owner word 2026-10-28; `dbeea3d` | DONE |
| [144](144-one-view-for-a-metric.md) | One view for a metric: the readings and the page in one place, at /metrics/{name} | P1 | M | owner word 2026-10-23; `07380db` | DONE |
| [143](143-daily-metrics.md) | Daily metrics: a mark on a metric, and its box on every day page | P1 | M | owner word 2026-10-23; `543ba89` | DONE |
| [142](142-the-fall-back-pass.md) | The fall-back pass: the parsers at their edges, and what a refusal must not write | P2 | M | owner word 2026-10-22; `1b3d78e` | DONE |
| [141](141-the-access-pass.md) | The access pass: every task has a way in, every mistake has a way out | P2 | L | owner word 2026-10-20; `39d37fc` | DONE |
| [140](140-the-plans-page.md) | The Plans page: every open plan line in one list, by day or by page | P2 | M | owner word 2026-10-20; `78b086a` | DONE |
| [139](139-pixel-icons.md) | Pixel icons: the icons of the rail, the New menu and the kinds as 10 by 10 glyphs | P3 | S | owner word 2026-10-19; `5dce8b1` | DONE |
| [138](138-pixel-art-elsewhere.md) | Pixel art elsewhere: the ground, the seal, the mood faces and the empty states | P3 | S | owner word 2026-10-19; `284e955` | DONE |
| [137](137-pixel-wood-and-the-focus-pass.md) | Pixel wood and the focus pass: a drawn rail, two heading levels, a writing mode, and a Today strip | P2 | M | owner word 2026-10-19; `cdc9240` | DONE |
| [136](136-plan-lines-and-period-pages.md) | Plan lines and period pages: the planning axis without a table | P1 | L | owner word 2026-10-19; `9cde014` | DONE |
| [135](135-extensions-outside-the-core.md) | Extensions outside the core: what names a third party is a subprocess found by name | P2 | L | owner word 2026-10-19; `7637f66` | DONE |
| [134](134-a-cover-from-google-photos.md) | A cover from Google Photos: an extension outside the core picks each day's cover from that day's photos | P3 | M | owner word 2026-10-18; `ed046b2` | DONE |
| [133](133-what-the-code-holds-twice.md) | What the code holds twice, what it holds for nothing, and four defects: the review against AGENTS.md | P2 | M | owner word 2026-10-18; `9b056bc` | DONE |
| [132](132-browse-the-notion-way.md) | Browse the Notion way: crumbs and a heading, a tree with +, Move on every row, and file-pages takes a thing out | P2 | M | owner word 2026-10-18; `2a03d2c` | DONE |
| [131](131-the-pareto-pass.md) | The Pareto pass: every action on every surface, one way to do a thing, and the browser's extras cut | P2 | L | owner word 2026-10-18; `c172ccd` | DONE |
| [130](130-the-explore-pass.md) | The explore pass: Calendar opens on a stream of day cards, with zoom levels, a random day and page, and search by kind | P2 | L | owner word 2026-10-17; `5e5a0d8` | DONE |
| [129](129-the-ux-pass.md) | The UX pass: plain words, quiet pages, the sentence of a mention, one Today, a capture that is safe to send again | P2 | L | owner word 2026-10-17; `14083e8` | DONE |
| [128](128-smaller-pictures.md) | Smaller pictures: 1280 px at quality 80, inside the contract's 1600 px and 1 MB | P3 | S | owner word 2026-10-17; `29c1628` | DONE |
| [127](127-a-cover-for-every-page.md) | A cover for every page: the first picture of a page, beside it, in the lists and the calendar, set from a picker | P2 | M | owner word 2026-10-17; `2f568fe` | DONE |
| [126](126-the-rule-book-review.md) | The rule-book review: the gaps against the five books, and the actions that close them | P1/P2/P3 | L | owner request 2026-10-17; `01221e5` | DONE |
| [125](125-a-range-is-a-true-range.md) | A range is a true range: the schema refuses a scale's unit with its high number first | P3 | S | owner word 2026-10-17; `63e0a36` | DONE |
| [124](124-true-comments-in-the-file.md) | True comments in the file: the comments inside CREATE TABLE say what is true | P3 | S | owner word 2026-10-17; `cdb7b8e` | DONE |
| [123](123-the-owners-contract-decisions.md) | The owner's contract decisions of 2026-10-17: today in every day field, a place keeps its type, true meta rows, the faults of a save | P2 | M | owner word 2026-10-17; `c867e6c` | DONE |
| [122](122-what-121-left.md) | What 121 left: four defects and gaps, and the small tidy ones | P2 | M | owner word 2026-10-17; `d407bb7` | DONE |
| [121](121-deep-modules-for-agents.md) | Deep modules for agents: one place a concept, fewer lines, every part testable | P2 | XL | owner request 2026-10-16; `fd4d82a` | DONE |
| [120](120-a-tidy-board.md) | A tidy board: every metric card has the same four lines, and the repeated buttons are quiet | P3 | S | owner word 2026-10-16; `4e0219e` | DONE |
| [119](119-the-finish.md) | The finish: the cards, the pick bar, the letter index, the folders and the margin lists look finished | P3 | M | owner request 2026-10-16; `c8c7baa` | DONE |
| [118](118-fold-and-find.md) | Fold and find: the category tree, the Browse list (folders, letter groups) and the Metrics board (folds, Cards or List) fold | P2 | M | owner request 2026-10-15; `0f85dda` | DONE |
| [117](117-one-browse-view.md) | One Browse view: the kinds and the categories in one place; Metrics keeps its board | P2 | L | owner request 2026-10-15; `d7194c8` | DONE |
| [116](116-strip-to-the-core.md) | Strip to the core: remove the conversation, read-only SQL, readings from a table, the public listener, photo positions, typed links, habits and tasks | P1 | XL | owner decision 2026-10-15; `518c3bb` | DONE |
| [115](115-categories-to-browse.md) | Categories to browse: the tree with counts and Not filed, a category page's Filed here, and a new page filed where it is made | P2 | M | owner request 2026-10-15; `7083b88` | DONE |
| [114](114-one-way-to-do-a-thing.md) | One way to do a thing, on every page: New on its list, ?new= links, + Add, category badges, one selection, one verb a meaning | P2 | L | owner request 2026-10-15; `ebf3a87` | DONE |
| [113](113-new-metric-on-metrics.md) | New metric, new category and a reading from the metrics page: a kind chooser, register-metric with category and habit_from, a + on each card | P2 | M | owner request 2026-10-15; `725e260` | DONE |
| [112](112-metrics-as-cards.md) | The metrics as cards, filed in groups: a jump bar, a card a metric with its latest value and line, and file-pages | P2 | M | owner request 2026-10-15; `16e40bb` | DONE |
| [111](111-one-metrics-view.md) | One metrics view: Habits becomes a lens of Metrics in the browser | P2 | S | owner decision 2026-10-15; `1697b50` | DONE |
| [109](109-a-voice-note.md) | A voice note: capture takes a recording, a transcriber turns it into words | P1 | S | owner decisions 2026-10-14; `895d2aa` | DONE |
| [110](110-the-media-list.md) | The media list: Files becomes Media, the pictures, the videos and the recordings as tiles; Files the rest | P1 | S | owner request 2026-10-14; `1697b50` | DONE |
| [108](108-the-browser-made-current.md) | The browser made current: layers, containers, and HTML in place of the script | P2 | M | owner decisions 2026-10-14; `28d10ce` | DONE |
| [107](107-delete-several-at-once.md) | Delete several at once: a box on each row of Files and Tasks | P1 | S | owner request 2026-10-14 | DONE |
| [106](106-one-continuous-conversation.md) | One continuous conversation: a page per day, compacted into the daily note | P1 | M | plans 102, 103; owner decision 2026-10-14 | DONE |
| [105](105-the-owners-shape-in-the-synthetic-fixture.md) | The owner's shape in the synthetic fixture: a calibrated workload for tests | P1 | L | owner request 2026-10-14; `bd16083` | TODO |
| [104](104-the-chat-in-basm.md) | The chat in Basm: the conversation drawn as the Chat of the owner's design system | P2 | S | plan 103; owner decision 2026-10-14 | DONE |
| [103](103-the-conversation-on-the-screen.md) | The conversation on the screen: a chat page, and a sidebar that follows the owner | P1 | M | issue 0072; owner decision 2026-10-14 | REJECTED (superseded by plan 116, RFC 0015) |
| [102](102-a-conversation-with-aminte.md) | A conversation with aminte: the action `ask`, an agent inside the writer | P1 | M | RFC 0012; issue 0071; owner decisions 2026-10-14 | REJECTED (superseded by plan 116, RFC 0015) |
| [101](101-the-calendar-holds-every-dated-thing.md) | The calendar holds every dated thing | P1 | M | issue 0070; owner decision 2026-10-13 | DONE |
| [100](100-one-page-a-metric.md) | One page a metric: Record, every reading, and its own page | P1 | M | issue 0069; owner decision 2026-10-13 | DONE |
| [099](099-rules-into-the-schema.md) | Rules into the schema: a Go copy deleted, a writer-held rule made a trigger | P1 | M | issue 0068 | DONE |
| [098](098-the-codebase-made-cohesive.md) | The codebase made cohesive: dead code out, one helper a rule, one convention a concept | P1 | L | issue 0067; owner decisions 2026-10-12 | DONE |
| [097](097-the-almanac-reflects.md) | The Almanac reflects: the past on the page, a refusal on its form, the week and the year | P1 | L | issue 0066; owner decision 2026-10-12 | DONE |
| [096](096-bound-in-hearthwood.md) | Bound in Hearthwood: the Almanac takes Balaur's materials | P1 | L | issue 0065; owner decisions 2026-10-12 | DONE |
| [095](095-the-leaf-and-its-margin.md) | The leaf and its margin: a calm Almanac | P1 | L | issue 0064; owner decisions 2026-10-12 | DONE |
| [094](094-the-command-list.md) | The command list of Ctrl K | P2 | M | issue 0063; owner decision 2026-10-12 | DONE |
| [093](093-words-phone-and-today.md) | Words, not data; the phone first; a richer Today | P1 | L | issue 0062; owner decisions 2026-10-12 | DONE |
| [092](092-the-app-shell.md) | The app shell: the Almanac design system in the browser | P1 | L | issue 0061; owner decisions 2026-10-12 | DONE |
| [091](091-the-browser-for-daily-use.md) | The browser for daily use: planning, the journal, navigation and editing | P1 | L | issue 0060; owner decisions 2026-10-12 | DONE |
| [090](090-agentic-init.md) | `aminte agentic-init`: the aminte folder ready for any agent | P1 | M | issue 0059 | DONE |
| [089](089-no-import-process.md) | No import process: the owner imports with an agent, through the catalog | P1 | L | RFC 0011; issue 0058 | DONE |
| [088](088-the-writer-runs-the-facts-pass.md) | The writer runs the facts pass with a local model: `aminte import run` | P1 | L | RFC 0010; issue 0057 | DONE |
| [087](087-prepared-quantities-rounded-to-whole-units.md) | The owner rounds chosen prepared quantities to whole units | P2 | S | issue 0055 | DONE |
| [086](086-rules-mark-the-ledger-and-status-reads-it-once.md) | The rules mark the ledger, and status reads it once | P2 | M | issue 0054 | DONE |
| [085](085-rejected-names-are-skipped-and-tombstoned.md) | A rejected name is skipped, and its imported row tombstoned | P1 | M | RFC 0009; issue 0053 | DONE |
| [084](084-owner-decides-names.md) | The owner decides each new name in `entities.md` | P1 | L | RFC 0008; issue 0052 | DONE |
| [083](083-check-reports-every-refusal.md) | Check reports every refused write with a class; look-alikes by kind | P1 | M | issues 0050, 0051 | DONE |
| [082](082-snapshot-in-the-catalog.md) | A snapshot from any surface: the owner-only snapshot action | P2 | S | Plan 081; 37767bb | DONE |
| [081](081-planning-on-every-surface.md) | Planning on every surface: tasks, occurrences and deadlines in the catalog | P1 | M | Owner decision 2026-10-08; adea9b8 | DONE |
| [080](080-api-reference.md) | An API reference for client authors, kept true by a test | P2 | S | Plans 078, 079; 68ca8d1 | DONE |
| [079](079-client-contract-hygiene.md) | Client contract hygiene: error codes, list pages, a connect timeout, one parameter | P2 | S | Plan 078; b28ee22 | DONE |
| [078](078-network-server.md) | Serve the API beyond this machine: a token, allowed origins, optional TLS | P1 | M | Owner requirement 2026-10-08; 05790ec | DONE |
| [077](077-ingest-folder-survey-and-passes.md) | An ingest folder: survey without reading, notes folders, added files, a later pass | P1 | M | issues 0043–0046 | DONE |
| [076](076-validation-gaps.md) | Exercise remaining validation gaps | P1/P2 | L | Owner continuation; 6f2bfe8 | DONE |
| [075](075-schema-validation-depth.md) | Schema validation depth | P1/P2 | L | Owner authorization; 90c062e | DONE |
| [074](074-schema-review-findings.md) | Repair schema review findings | P1/P2 | L | Issues 0026–0032; owner authorization | DONE |
| [073](073-review-repairs.md) | Schema, reliability and validation repairs | P1/P2 | L | Reproduced review findings | DONE |
| [071](071-audit-followups.md) | Integrate the 22 residual audit follow-ups | P1/P2 | L aggregate | Existing import/surface behavior retained | REJECTED (the import machinery was removed by plan 089) |
| [072](072-stable-names-candidate.md) | Stable names and life periods implementation candidate | P1 | L aggregate | RFC 0006 candidate approvals; identity-core first | REJECTED (RFC 0006 withdrawn; never merged) |

Status values: TODO | PLANNED | IN PROGRESS | DONE | BLOCKED (with one-line reason) |
REJECTED (with one-line rationale). This table is the one home of the status of a plan. A plan is DONE when it is live.
The plan file holds the record (dates, commits, checks), not the status word. Close a plan with its result in its file
and its status here, retaining the dated record as [the process](../process.md) requires.

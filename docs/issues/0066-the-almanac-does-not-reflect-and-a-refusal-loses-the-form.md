# 0066 — The Almanac never shows the owner their past, and a refused form loses its place

- **Date:** 2026-10-12
- **Status:** resolved
- **Seen in:** the owner's request for a review of the browser against the published guidance after
  [plan 096](../plans/096-bound-in-hearthwood.md): design-system practice, WCAG 2.2, the GOV.UK form patterns, the
  calm-technology principles, the ADHD guidance, and the journaling applications people keep (Day One, Obsidian,
  Logseq); checked against the code at `8691164` on a synthetic database in Edge on Windows

## What happened

The Almanac keeps the strong rules: semantic HTML that works with no script, one loud button a screen, 44 px targets,
a contrast gate in a test, reduced motion honoured, words not data, and habits told as "6 of 14 days" rather than a
streak. The review found the gaps elsewhere. Each row is a fact of the code at `8691164`.

| area | what the browser does | what the guidance asks |
|---|---|---|
| a refused write | a POST that fails (a stale version, a bad field) answers with the error view: a separate page, the text the owner sent in a read-only box, and a Back link. The owner copies the text out and finds the form again. | the error on the form it came from, the form still filled, the first words in the title, the focus on the alert (GOV.UK error summary; WCAG 2.2 3.3.7 Redundant entry) |
| the past | a day shows the day, its 14-day trends and five recent entries. Nothing shows what the owner wrote on this date in earlier years, though the database is lifetime-scale by design. | "on this day" is the one review feature every journaling application has, and the one a lifetime log answers best |
| the calendar's units | a month has a view and a day has one; the leaf says "week 41" as a word. There is no week and no year. | the weekly review is the reflection every source shares; the year is the long view (Day One's calendar, the year in review) |
| a habit's check-in | a check-in is a reading of 1 among the day's `trends`, so the Readings group shows "Walk 1" with a sawtooth line beside the real readings | a habit has its strip; its check-in is not a reading to chart (plan 096 recorded this as not done) |
| empty states | "Nothing is due today or overdue.", "No readings on this day.", "Nothing yet.": the state alone | the state, then the way forward, with the link (NN/g empty states) |
| the lists | People and Places are bare titles | a row says when the owner last wrote of them and how often |
| the tokens | `base.css` carries two token blocks, light and dark, 30 lines each, kept in step by hand; the test holds WCAG 2 ratios only | one line a token with `light-dark()` (Baseline 2024; Balaur writes its own this way); an APCA reading beside the ratio, since WCAG 2 overstates contrast on dark grounds |
| the system itself | the components exist in `app.css` and in the pages; there is no page that shows every component in every state, so a review takes fifteen pages and a screenshot pass takes an hour | a style page, as Storybook is to Balaur |
| the phone | the browser is a tab; the bottom bar can cover a focused field; sharing text from another application has no way in | an installable manifest with shortcuts and a share target; `scroll-padding` under the bar (WCAG 2.2 2.4.11 Focus not obscured) |
| the small HTML | the capture box is a fixed four rows; the rail's search is a `form role=search`; the list of names after `[[` is clipped by its container; the second motion the brief allows (one that answers an action) is unused; the keys have no help key | `field-sizing: content` (Baseline 2026), the `search` element (2023), `popover` (2024), cross-document view transitions on the day's step, `?` for the keys |

## Reproduce

1. Build `aminte` at `8691164`; on a fresh file capture on 12 days across two years with the same month-day in
   both, start a habit and check in, create a task due today.
2. Open a page, open Edit, change the body in a second tab and save, then save the first tab: the 409 is a separate
   page with the draft in a box.
3. Open today: no "on this day" though last year's page exists; the habit's check-in is a tile among the readings;
   "week 41" is a word, not a link; the empty groups say only their state.
4. Open `/people`: titles only. Read `base.css`: two blocks of the same tokens.

## Rules involved

- [README](../../README.md): "The app shell: the Almanac design system", "The browser for daily use", "One script,
  which no page needs", "The stylesheet is this application's own"
- [AGENTS.md](../../AGENTS.md): "Every capability on every surface" (a week, a year and on-this-day are resources
  or properties, so the CLI and the MCP server get them with the browser)
- [API.md](../../API.md): the day, the month and the lists

## Resolution

Resolved by [plan 097](../plans/097-the-almanac-reflects.md): a refused form comes back on its own page, filled, with the alert
first; the day and Home show on this day; the week and the year have their resources and pages; a habit is told by its
strip only; every empty state names the way forward; People and Places say their mentions; the tokens are one block
with the APCA logged beside the ratio; the style page shows every component; the manifest makes the app installable
with a share target; the small HTML is in. Every row of the table above is closed.

# 0062 — The browser still shows data where it should show words, is heavy on a phone, and Today is thin

- **Date:** 2026-10-12
- **Status:** resolved
- **Seen in:** the owner's review of the browser after [plan 092](../plans/092-the-app-shell.md), on the synthetic
  database of plan 091, at 1280 px and 390 px in Edge on Windows

## What happened

The owner asked: "Can we further enhance the design ui and ux?" Of four areas found in the review, the owner chose
three on 2026-10-12: words in place of raw data, a layout made for a phone first, and a richer Today. Each item below is
a fact of the code at `3fa2fb6`.

| area | what the browser does | what the owner expects |
|---|---|---|
| words | A time is the raw instant: a page says "created 2026-10-12T06:48:45.484Z", a reading's "taken at" and an occurrence's completion are the same. | a time in words, in the clock of the server's day: "09:48, Mon 12 Oct" |
| words | A day near today is "7 days ago (Mon 2026-10-05)"; the design system writes "7 days ago · Mon 5 Oct". | the days of the design system |
| words | The result of a write is its JSON text in a `<pre>`: `{"linked": ["Garden project"]}`. | a sentence: "Linked Garden project." |
| words | A class with no view of its own (sessions, a session, periods, integrity, a snapshot, a query) shows its properties as tables of JSON names (`start_utc`, `task_version`), instants and booleans as they are stored. | facts in words, each value drawn by its type |
| phone | At 390 px the sidebar is about 230 px high before the content: the brand, the search, New, a row of 12 sections that must be scrolled sideways, and the owner's links. | the sections of every day always one tap away, and the content first |
| Today | A reading's tile has its value but not its unit, and nothing of the days before. | the unit, and the line of the last two weeks |
| Today | A habit shows today's state only. | the habit's last days at a glance |
| Today | The page of a series that repeats every 3 days lists 28 later rows in its 120-day window. | the next few, then how many more |

## Reproduce

1. Build `aminte` at `3fa2fb6`; on a fresh file, capture on three days, record a weight on two days, start a habit,
   and create a task that repeats every 3 days.
2. `aminte serve`; open a page, the reading, `/sessions`, the task, and `/days/today`, at 1280 px and 390 px.
3. Save a body with a `[[link]]` from the browser and read the result.

## Rules involved

- [README](../../README.md): "The app shell: the Almanac design system", "The page is semantic HTML, and the catalog
  says how to draw it", "The browser gets the same entity"
- [AGENTS.md](../../AGENTS.md): "Every capability on every surface" and client parity
- The design system's section "The Siren contract": a value, by its type

## Resolution

Resolved by [plan 093](../plans/093-words-phone-and-today.md), in three commits. Every value is drawn by its type: days
and times in words, in the clock of the server's day, facts for the generic view, and a write's result in sentences.
On a phone the shell is a top bar, a bottom bar of the sections of every day, and a sheet for the rest, with no script.
The day carries each metric's trend and each habit's last 14 days, so Today shows units, lines and strips, and a series
no longer fills its own page.

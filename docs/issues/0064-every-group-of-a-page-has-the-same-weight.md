# 0064 — Every group of a page has the same weight, and the page asks for many decisions at once

- **Date:** 2026-10-12
- **Status:** resolved
- **Seen in:** the owner's request "I want to refine and enhance the frontend design for this app" after
  [plan 094](../plans/094-the-command-list.md), and a review of the browser on a synthetic database (two weeks of
  captures, five tasks, three metrics, two habits) at 1280 px and 390 px, light and dark, in Edge on Windows

## What happened

The owner asked to refine the design, then said: "you have full control over the design, pages, ui, ux. It should
follow the best practices, be adhd friendly and have full functionality". Each item below is a fact of the code at
`6d0b35e`.

| area | what the browser does | what the owner expects |
|---|---|---|
| hierarchy | On a day, Capture, Journal, Due, Habits and Readings are the same white card with the same border and radius. Nothing says which is the main thing. | the day's writing first; the rest quieter, beside it |
| labels | Each group has a grey label in capitals above it ("ADD TO TODAY", "DUE"); table headers and the kinds of a page's links too. Capitals are slow to read. | headings in sentence case, in a type scale |
| time | The day's title is "Monday" under `2026-10-12 · week 42 · today` in small grey mono. "Time as the spine" (plan 092) is not visible. | the date as the head of the day, as on a calendar leaf |
| attention | Due on Today has a filled blue Done on every row: five of the loudest buttons on the page, in one column. The rule of `app.css` is one primary button a page. | one loud button a screen: the main write of the page |
| decisions | "Record a reading" opens seven fields, among them "Taken at (UTC, 2026-06-09T21:14:03.482Z)" and "Key". "New task" opens ten. | the fields most writes need; the others one step away |
| mono | Words are in mono with the dates: "reminder", the project's title, "mood". Plan 092 put days, times and numbers in mono. | mono for days, times and numbers only |
| words | A backlink from a day page says "2026-10-02". | a day in words, "Fri 2 Oct" |
| overdue | An overdue row says "6 days ago" in the same grey as a row due today. | overdue said in a word, not only by the date |
| phone | Buttons and the mood's choices are 32 to 38 px high. | 44 px targets on a touch screen |

## Reproduce

1. Build `aminte` at `6d0b35e`; on a fresh file capture on 12 days with a mood, record two metrics on 14 days, start
   two habits with check-ins, and create a one-off task due three days ago, one due today and a series every 3 days.
2. `aminte serve`; open `/days/today` at 1280 px and 390 px, light and dark; open "Record a reading"; open a place's
   page.

## Rules involved

- [README](../../README.md): "The app shell: the Almanac design system", "The page is semantic HTML, and the catalog
  says how to draw it", "One script, which no page needs"
- [AGENTS.md](../../AGENTS.md): "Every capability on every surface" and client parity

## Resolution

Resolved by [plan 095](../plans/095-the-leaf-and-its-margin.md), in three commits. A day and Home are headed by a
calendar leaf, and every page is a sheet with its margin: the writing first, what the day asks of the owner beside it.
Headings are in sentence case on a type scale; only the main write of a page is a filled button, a row's Done is an
outline, and an overdue row says so in a word. Long forms fold their optional fields under "N more fields", which a
browser still sends with no script. Days in the remaining views are words, and controls are 44 px on a phone.

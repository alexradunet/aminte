# 0061 — The browser does not look or move like an application

- **Date:** 2026-10-12
- **Status:** resolved
- **Seen in:** the owner's review of the browser after [plan 091](../plans/091-the-browser-for-daily-use.md), on the
  synthetic database of that plan, at 1280 px and 390 px in Edge on Windows

## What happened

Plan 091 made the browser usable every day. The owner then said: "I think I will want an even better UI/UX with a
proper sidebar and for it to look like an app", and asked for "a design system revolved around siren json". These
points stop the browser from feeling like an application. Each one is a fact of the code at `44b661b`.

| area | what the browser does | what the owner expects |
|---|---|---|
| navigation | The top bar shows 4 sections (Today, Calendar, Due, Tasks). The other 12 are in the More menu (`navItems` in `internal/api/html.go`). To go from a day to People is two clicks, and the bar does not say where the owner is when the page is in the menu, except by the menu's word. | a sidebar that holds every section, always in the same place, with the current one marked |
| layout | Every page is one column, at most 54rem wide (`body` in `base.css`). At 1280 px, a third of the window is empty, and the day view stacks seven groups that a wide screen could show side by side. | the width used: a sidebar, and the groups of a page in columns |
| look | The pages are styled HTML elements in the system font: no type scale for days and numbers, and a heading is a small grey word in capitals. There is no set of components: each template draws its own list, row and form. | one look, with a fixed set of components that every page uses |
| time | Today's date, this month and the count of what is due are on no page except their own. | the date, the month and what is due always in view |
| contract | The rules that turn an entity into a page are in the README and in the templates. Nothing says, part by part, which component draws `links`, `properties`, `entities`, `actions`, an error or a `result`. | a design system built on the Siren entity, so each part of an entity has one component |

## Reproduce

1. Build `aminte` at `44b661b`; `aminte init` on a fresh file; capture text on three days, create two tasks and
   record a reading.
2. `aminte serve`; open `/days/today` at 1280 px. Then go to People, then to Calendar.
3. Open `/days/today` at 390 px and count the screens to the habits.

## Rules involved

- [README](../../README.md): "The browser gets the same entity", "The page is semantic HTML, and the catalog says
  how to draw it", "One script, which no page needs"
- [AGENTS.md](../../AGENTS.md): "Every capability on every surface" and client parity
- [D14](../decisions/D14-ui-and-tools.md): a thin custom application for capture and browsing

## Resolution

Resolved by [plan 092](../plans/092-the-app-shell.md), in three commits. The owner chose the Almanac direction of a
design system built on the Siren entity (the artifact Aminte Almanac). Every page is now in one shell: a sidebar with
every section, today's date, the Due count and this month, which becomes a bar over the page on a phone. Each part of
an entity has one component, and the day is drawn in two columns. The look is light first and dark by the system
setting, with two embedded open fonts; a test holds the contrast of every named pair of colours.

# 0060 — The browser cannot carry daily planning, journaling, editing and navigation

- **Date:** 2026-10-12
- **Status:** resolved
- **Seen in:** the owner's attempt to use `aminte serve` in a browser as the daily web application; reproduced on a
  synthetic database (five days of captures, two people, a habit, a metric, five tasks of which two are series)

## What happened

The owner wants to use the browser as the daily surface. Four areas stop that use. Each item below was seen on the
synthetic database at 1280 px and at 390 px (a phone) in Edge on Windows.

| area | what the browser shows | what the owner expects |
|---|---|---|
| planning | `/tasks`, `/tasks/{id}`, `/deadlines` and an occurrence have no template. The generic view shows every property as a table, internal ones too (`task_version`, `virtual`, `reminder_mode`, instants), and a reminder as a nested table. | each task and occurrence in words: what, when, how often, the reminder |
| planning | To mark one slot of a series done: open `/deadlines`, open the occurrence, open "Capture occurrence", fill `state` and `completed_at` among seven fields, send. `/deadlines` offers no action per occurrence. | one "Done" button beside each due occurrence |
| planning | `/deadlines` is one list over 120 days. A series every 3 days fills 40 rows and hides the one-off tasks. A task page lists the same 40 rows. Nothing marks a slot as overdue. | overdue, today, the next days, later, each as a group |
| journal | The day view does not show what is due on the day. A habit is a line of text, and "Check in" opens another page. The mood is a row of a readings table with the columns `scope` (`unassociated`) and `taken at` (`—`). | what is due and the habits on the day itself, with buttons; the mood beside the entry |
| journal | `/days` is a list of dates with no text. There is no way to open a given date, or to see a month. | a month calendar and a date to jump to |
| editing | The editor is a textarea in a closed box under the body. To edit a long page, the owner scrolls to the end. The day view sends the owner to another page to edit. There is no preview, no help to write a `[[link]]` to an existing title, no keyboard save, and no warning before the owner leaves text that is not saved. | the editor near the top, on the day view too; suggestions for `[[`; a preview; Ctrl+Enter; a warning |
| navigation | Home is a capture box and eleven closed forms. It shows nothing about today. The top bar has ten links and the search box: on a phone it takes three rows before the content. | Home as today at a glance; a short top bar |
| navigation | After a form on one page writes another resource, the browser goes to that resource (`303` to its `self`), not back to the page the owner was on. A "Done" button on Home would leave Home. | the browser returns to the page that sent the form |

## Reproduce

1. `aminte init` on a fresh file; capture text on three days; `create-task` with `repeat_unit=day repeat_every=3`,
   and one with `due_day` in the past.
2. `aminte serve`; open `/deadlines`, `/tasks/2`, `/days/today`, `/days` and `/` in a browser.
3. Mark the past one-off task done from the browser: count the pages and the fields.

## Rules involved

- [README](../../README.md): "The browser gets the same entity", "No JavaScript", "The page is semantic HTML, and
  the catalog says how to draw it", "Planning on every surface"
- [AGENTS.md](../../AGENTS.md): "Every capability on every surface" and client parity
- [D14](../decisions/D14-ui-and-tools.md): a thin custom application for capture and browsing
- [planning](../contract/planning.md): occurrences, slots and reminder intent

## Resolution

Resolved by [plan 091](../plans/091-the-browser-for-daily-use.md), in four commits. The planning screens have their
own views, grouped by the due day against today, with Done, Skip and Reopen on each due occurrence. The day view
shows what is due, its mood and its habits with buttons, and edits its day page in place. Home is today at a
glance. A month calendar and "go to a day" join the days, and the top bar holds the daily sections and a More menu.
The editor is under the heading and as tall as its body. One embedded script, which no page needs, adds the names
after `[[`, a preview, Ctrl+Enter, Escape, keys and a question before unsent text is lost. A form returns to the page
that sent it.

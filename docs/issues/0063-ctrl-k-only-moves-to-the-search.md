# 0063 — Ctrl K only moves to the search; an action of another page is many steps away

- **Date:** 2026-10-12
- **Status:** resolved
- **Seen in:** the owner's review of the browser after [plan 093](../plans/093-words-phone-and-today.md), on the synthetic
  database of plan 091, in Edge on Windows

## What happened

Of the four areas of the review of 2026-10-12, the owner first chose three ([issue 0062](0062-the-browser-still-shows-data-not-words.md)).
On the same day the owner chose the fourth: "let's do that one too", the command list of Ctrl K that the Console draft
of the design drafts showed. Each item below is a fact of the code at `9713306`.

| what the browser does | what the owner expects |
|---|---|
| Ctrl K puts the cursor in the sidebar's search, a full-text search of bodies. To open a page by its title, the owner types the search and reads its results. | Ctrl K opens a list that finds a page by the start of its title, as `[[` does in a body |
| To make a new task from a day, the owner goes Home, opens New, then opens New task: three steps and a page change. | the actions that start something are a few keys away from any page |
| On a page with many actions (a page has eight in More), the owner scrolls to the box of one. | the actions of this page are in the same list |
| On a phone, the sections of the record are behind More. | the same list on the phone's Search button |

## Reproduce

1. Build `aminte` at `9713306` and serve a synthetic database.
2. On a day, press Ctrl K and type the start of a page's title.
3. From the same day, make a new task.

## Rules involved

- [README](../../README.md): "One script, which no page needs", "The app shell: the Almanac design system"
- [AGENTS.md](../../AGENTS.md): "Every capability on every surface": the catalog decides what is offered where

## Resolution

Resolved by [plan 094](../plans/094-the-command-list.md). Ctrl+K, and the phone's Search button, open a command list:
a typed day, this page's actions, the sections, the pages by the start of their title, the root's actions and a
search of the bodies. An action opens the form the server drew for it, on this page or on Home.

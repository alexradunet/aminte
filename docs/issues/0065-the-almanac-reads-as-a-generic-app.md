# 0065 — The browser's look is not the owner's own: the Almanac reads as a generic app

- **Date:** 2026-10-12
- **Status:** resolved
- **Seen in:** the owner's request "Let's revamp the almanac design … maybe we can update the almanac based on this
  look", pointing at the owner's own design system, **Balaur — Basm (Hearthwood)**, after
  [plan 095](../plans/095-the-leaf-and-its-margin.md); and a review of the browser on a synthetic database at
  1280 px and 390 px, light and dark, in Edge on Windows

## What happened

Plans 092 to 095 gave the browser a shell, words, a phone layout and a calm hierarchy. The look under them is the
Almanac of plan 092: cool paper, ink blue, white cards with an 8 to 12 px radius, 1 px lines and a soft shadow. The
owner keeps one material language across his projects, Hearthwood: a dark oak page, wood chrome, parchment for
everything written, ink text, candle gold and ember, square corners, 2 px outlines, bevels and hard drops, pixel-hard
construction. The aminte does not share it. Each item below is a fact of the code at `e79b3eb`.

| area | what the browser does | what the owner expects |
|---|---|---|
| identity | `base.css` is any app's tokens: `#f3f5f8` ground, `#2347b5` accent, `#ffffff` cards. Nothing says whose it is. | the owner's own materials: oak, wood, parchment, ink, gold |
| construction | rounded corners on every card, button, field and tag; one soft `rgba` shadow; 1 px lines | square corners, 2 px edges, bevels and 3 px hard drops: woven, not rendered |
| the written thing | the sheet and the margin are the same white and the same grey ground; a journal reads like a form | what is written is parchment, and the page around it is the desk |
| display | the leaf's number and a reading's value are IBM Plex Mono 500; a title is Atkinson bold | the display face of the owner's system for the few large numbers and titles |
| press | a button changes its border colour on hover and nothing on press | a press that sinks: the one motion, and it answers the hand |
| light and dark | two palettes of the same app skin | the dark room as the identity, the daylight study of the same room as the light theme |

What stays right and must not go: Atkinson Hyperlegible for every word, sentence case and nothing in capitals, mono for
days, times and numbers only, one loud button a screen, the leaf, the sheet and its margin, 44 px targets on a phone,
every form with no script, every colour a token with its contrast held by a test, nothing fetched from another host.

## Reproduce

1. Build `aminte` at `e79b3eb`; on a fresh file capture on 12 days with a mood, record two metrics on 14 days, start
   two habits with check-ins, and create a one-off task due three days ago, one due today and a series every 3 days.
2. `aminte serve`; open `/days/today`, `/months/current`, `/deadlines` and a page at 1280 px and 390 px, light and
   dark. Compare with the Balaur Storybook's Life dashboard and Tasks page.

## Rules involved

- [README](../../README.md): "The app shell: the Almanac design system", "The stylesheet is this application's own",
  "The page is semantic HTML, and the catalog says how to draw it"
- [AGENTS.md](../../AGENTS.md): "Every capability on every surface"; the browser rules of plan 095

## Resolution

Resolved by [plan 096](../plans/096-bound-in-hearthwood.md). The Almanac keeps its parts (the shell, the leaf, the
sheet and its margin, one loud button, folded fields, words) and takes the materials of the owner's Balaur design
system: oak, wood and parchment with ink, square corners, 2 px edges, bevels and hard drops; gold for where the owner
is and what is today, ember for the one loud button and "overdue", teal for a link; Jersey 15 for a title, the leaf's
number and a reading. Dark is the identity, light the daylight study, by the system setting. Every token pair is held
by the contrast test; the fonts are embedded like the others.

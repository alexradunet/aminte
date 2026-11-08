# Product concepts

| Product concept | Schema mechanism |
|---|---|
| Journaling | the day page, titled `YYYY-MM-DD`: capture appends to it ([capture](../cookbook/capture.md)); the day view adds what else that day holds ([the day view](../cookbook/day-view.md)) |
| "Which journal days mention Ana or Lakeside?" | the day pages that link `[[Ana]]` or `[[Lakeside]]`; a mention alone does not establish a meeting or visit ([the days that name someone](../cookbook/days-that-name.md)) |
| "Where was I that day?" | the words of the day page, `I was at [[Lakeside]]`; the days at a place are the backlinks of its page ([days that name](../cookbook/days-that-name.md), [D16](../decisions/D16-places.md)) |
| Mood tracking | the `mood` metric in `measurements`, each row optionally pointing at its day page ([mood over time](../cookbook/mood-over-time.md)) |
| Notes, wiki and tags | `entities.body` + `links(kind='wikilink')` kept equal to what the body names ([titles and wikilinks](../contract/titles-and-wikilinks.md), [save a body](../cookbook/save-a-body.md)); `#health` names `health` without rewriting the body |
| Backlinks | `links WHERE to_id = ?` ([backlinks](../cookbook/backlinks.md)) |
| People in prose | `[[Bob Sample]]` links to the person itself, because the person is a page ([D20](../decisions/D20-named-pages.md), [a person](../cookbook/person.md)) |
| Life graph ("everything about my son") | `links` in both directions from his id ([everything about](../cookbook/everything-about.md)) |
| Sleep, workouts and other recorded activities | a reading on the reporting day, under a metric of its own when it must not add to a daily total (`workout-steps` beside `steps`); anything else is day-page prose ([D22](../decisions/D22-events.md)) |
| A span of life (a trip, a job, a home) | a plain page: its body says when; the day pages inside it link it, so its backlinks list those days ([D22](../decisions/D22-events.md)) |
| Habits | a unitless metric with a reading of 1 or 0 a day ([D24](../decisions/D24-habits.md)); its series is the record; the habits the writer asks for on every day carry `metrics.daily` ([D31](../decisions/D31-daily-metrics.md)) |
| Birthdays | a query over `people.birth_day` |
| Biomarkers / quantified self | `metrics` + `measurements` ([a metric series](../cookbook/metric-series.md)), filed in category pages ([metrics by category](../cookbook/metrics-by-category.md), [D26](../decisions/D26-metric-categories.md)) |
| Imported notes (a vault) | `entities.import_key`, unique per `source`: a re-run inserts nothing, a changed note updates its page ([import a row once](../cookbook/import-a-row-once.md)) |
| Photos, recordings, scans | a file page: its text (a transcript, a caption) in the body, a small JPEG in `files.preview`, the original left outside; `![[Lake.jpg]]` in a day page links it ([keep a file](../cookbook/keep-a-file.md), [D9](../decisions/D09-binary-files.md)) |
| Search | `entities_fts` ([full-text search](../cookbook/full-text-search.md)) |
| "Which of my agents wrote this?" | `source` on every entity, link and measurement (`aminte_meta.source`, [D10](../decisions/D10-time-model.md)) |

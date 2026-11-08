# D31 — A daily metric: the writer asks for a reading of it on every day.

**Status:** accepted

- **Context.** The writer guessed which metrics to ask for, from the readings of the last 14 days ([issue 0090](../issues/0090-the-writer-guesses-which-metrics-are-daily.md),
  [RFC 0023](../rfcs/0023-a-daily-mark-on-a-metric.md)). A metric with a unit was never found. A lapsed habit
  disappeared.
- **Decision.** `metrics.daily` is 1 when the owner asks the writer for a reading of the metric on every day, and 0
  otherwise (`metrics_daily`). It is the metric's own ([D27](D27-a-metric-is-a-page.md)). The owner sets it with the
  metric or later. Unlike the unit, it may change at any time, and a change of it advances the page's revision (`metrics_touch`,
  `aminte_meta.edit_revisions`); a no-op does not. A day with no reading still says nothing
  ([D24](D24-habits.md)): the mark is a prompt, not a rule about the readings. Which daily metrics a writer shows,
  and how, is the writer's choice. The block "The readings asked for on this day" of the
  [day view](../cookbook/day-view.md) shows the read.
- **Alternatives.**
  - *The guess from recent readings:* rejected. A metric with a unit is never found, and a lapsed habit disappears.
  - *A category page named Daily:* rejected, as in RFC 0023.
  - *A `aminte_meta` key:* rejected. A rule about one table is a CHECK inside its `CREATE` statement.
  - *A table of prompts or periods:* rejected by [RFC 0018](../rfcs/0018-no-habit-periods.md).
- **Trade accepted.** The owner must set the mark. Nothing holds a daily metric to one reading a day. Two readings
  on one day are both kept ([D7](D07-measurements.md)).
- **Sources.** Executed: the `facts` suite (`metrics.daily` takes only 0 or 1, and the mark may change) and the
  `cookbook` suite (the day-view block).

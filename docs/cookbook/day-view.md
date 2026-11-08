# The day view

The day's page, other pages written that day, and the measurements (through
`measurement_values`, so corrected readings never show).
`ORDER BY (at IS NOT NULL), at` puts undated items — the day page first — before the rest on purpose;
a bare `ORDER BY at` does it by accident.

```sql
SELECT what, at, detail FROM (
  SELECT 'day page' AS what, NULL AS at, e.body AS detail
    FROM entities e
   WHERE e.preferred_name_key = :day AND e.day = :day AND e.deleted_at IS NULL
  UNION ALL
  SELECT 'page' || CASE WHEN e.revision > 1 THEN ' (edited)' ELSE '' END,
         e.updated_at, p.title
    FROM entities e JOIN entity_names p ON p.entity_id = e.id AND p.name_key = e.preferred_name_key
   WHERE e.day = :day AND e.preferred_name_key <> :day AND e.deleted_at IS NULL
  UNION ALL
  SELECT p.title, me.taken_at, CAST(me.value AS TEXT) || ' ' || m.unit
    FROM measurement_values me JOIN metrics m ON m.id = me.metric_id
    JOIN entities e ON e.id = m.id AND e.deleted_at IS NULL
    JOIN entity_names p ON p.entity_id = e.id AND p.name_key = e.preferred_name_key
   WHERE me.day = :day
)
ORDER BY (at IS NOT NULL), at;
```

A page shows on the day it was *written* (a link target the app created has no day, [D5](../decisions/D05-pages-and-day-pages.md)), flagged if
edited since. "Edited" is the edit revision above 1, never two clock values compared: creating a page, with its owned
name and typed row, leaves revision 1, and the clocks of separate statements need not agree
(`aminte_meta.edit_revisions`).

## The readings asked for on this day

The live daily metrics ([D31](../decisions/D31-daily-metrics.md)), in name order, each with the last value of the day.
A daily metric with no value is a row with NULL: the day says nothing about it ([D24](../decisions/D24-habits.md)).

```sql
SELECT p.title, m.unit,
       (SELECT v.value FROM measurement_values v
         WHERE v.metric_id = m.id AND v.day = :day
         ORDER BY v.taken_at DESC, v.id DESC LIMIT 1) AS value
  FROM metrics m
  JOIN entities e ON e.id = m.id AND e.deleted_at IS NULL
  JOIN entity_names p ON p.entity_id = e.id AND p.name_key = e.preferred_name_key
 WHERE m.daily = 1
 ORDER BY e.preferred_name_key;
```

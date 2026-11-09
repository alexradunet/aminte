# Proposals (RFCs)

A proposal is a schema change argued before it is made: which issues it answers, the options, the cost of each
after the freeze, and what it makes redundant ([how a change happens](../process.md)). An accepted proposal
becomes a [decision](../decisions/README.md). One file per proposal, named `NNNN-short-slug.md`, written from the
[template](template.md).

| id | title | status | issues | decision |
|---|---|---|---|---|
| [0001](0001-metric-categories.md) | Metric categories: a closed registry, a tree whose parents never change | accepted | 0008 | [D26](../decisions/D26-metric-categories.md) |
| [0002](0002-categories-are-pages.md) | Metric categories are pages, nested by `part-of` links | accepted | 0008 | [D26](../decisions/D26-metric-categories.md) |
| [0003](0003-a-metric-is-a-page.md) | A metric is a page; anything is filed in a category by `part-of` | accepted | 0008 | [D27](../decisions/D27-a-metric-is-a-page.md), [D26](../decisions/D26-metric-categories.md) |
| [0004](0004-a-file-is-a-page.md) | A file is a page: its text in the body, a small picture kept, the original outside | accepted | 0009 | [D9](../decisions/D09-binary-files.md) |
| [0005](0005-a-place-has-a-point.md) | A place has a point and a radius; a photo's place and day are taken when it is kept | accepted | 0010 | [D21](../decisions/D21-location-history.md) |
| [0006](0006-stable-names-and-life-periods.md) | Stable names, one named-object core, and recorded life periods | withdrawn | [0011–0021](../issues/README.md) | none: each issue was resolved or closed by a later decision (2026-10-28) |
| [0007](0007-personal-tasks-and-occurrences.md) | Personal tasks, recurring occurrences and reminder intent | accepted | [0025](../issues/0025-personal-planning-has-no-durable-task-model.md) | [D23](../decisions/D23-no-tasks.md), [D15](../decisions/D15-recurrence.md) |
| [0008](0008-owner-decides-names-in-imports.md) | The owner decides each new name of an import in one stamped list | accepted | [0052](../issues/0052-names-are-written-on-the-models-word.md) | [D11](../decisions/D11-tombstones.md); the guide's "entities.md" |
| [0009](0009-a-rejected-name-is-skipped-and-tombstoned.md) | A rejected name is skipped, and the row the import wrote under it is tombstoned | accepted | [0053](../issues/0053-a-rejected-name-is-not-removed.md) | [D11](../decisions/D11-tombstones.md), [D13](../decisions/D13-migrations-and-freeze.md); the guide's "entities.md" |
| [0010](0010-the-writer-drives-a-local-model.md) | The writer drives a local model for the facts pass | accepted | [0057](../issues/0057-the-import-loop-lives-outside-the-writer.md) | the non-goals row on AI generation; the guide's "Three parties" |
| [0011](0011-no-import-process.md) | No import process: the owner imports with an agent, through the catalog | accepted | [0058](../issues/0058-the-import-machine-outweighs-the-backfill.md) | [D13](../decisions/D13-migrations-and-freeze.md), [D11](../decisions/D11-tombstones.md); [imports](../contract/imports.md) |
| [0012](0012-a-conversation-with-aminte.md) | A conversation with aminte: an agent inside the writer, and where its conversation is kept | accepted | [0071](../issues/0071-the-owner-cannot-talk-to-aminte.md) | option A: no decision; the non-goals row on AI generation; [plan 102](../plans/102-a-conversation-with-aminte.md) |
| [0013](0013-no-sessions.md) | No sessions: a recorded activity is a reading or prose | accepted | [0073](../issues/0073-sessions-cost-more-than-they-give.md), [0020](../issues/0020-imported-sessions-need-time-and-measurement-scope.md) | [D22](../decisions/D22-events.md), [D7](../decisions/D07-measurements.md) |
| [0014](0014-no-periods.md) | No recorded periods: a span is a plain page | accepted | [0074](../issues/0074-periods-cost-more-than-they-give.md) | [D22](../decisions/D22-events.md) |
| [0015](0015-no-conversation.md) | No conversation inside the writer: the owner talks to aminte through a coding agent | accepted | [0075](../issues/0075-the-conversation-costs-more-than-it-gives.md), 0071, 0072 | none |
| [0016](0016-no-location.md) | No location: a place has no point, and a photo's position is never read | accepted | [0076](../issues/0076-a-places-point-costs-more-than-it-gives.md) | [D21](../decisions/D21-location-history.md) |
| [0017](0017-no-typed-links.md) | Three link kinds: wikilink, at and part-of; no symmetric mirror | accepted | [0077](../issues/0077-relationship-links-cost-more-than-they-give.md) | [D8](../decisions/D08-entities-and-links.md) |
| [0018](0018-no-habit-periods.md) | No habit periods: a habit is a metric with a reading of 1 or 0 a day | accepted | [0078](../issues/0078-habit-periods-cost-more-than-they-give.md) | [D24](../decisions/D24-habits.md) |
| [0019](0019-no-tasks.md) | No tasks: a plan is words in a page | accepted | [0079](../issues/0079-tasks-cost-more-than-they-give.md) | [D23](../decisions/D23-no-tasks.md), [D15](../decisions/D15-recurrence.md) |
| [0020](0020-a-day-page-is-never-filed.md) | A day page is never filed: links_endpoint_types refuses a part-of link from it | accepted | [0080](../issues/0080-a-filed-day-page-is-shown-nowhere.md) | [D26](../decisions/D26-metric-categories.md) |
| [0021](0021-a-capture-key.md) | A capture key: the writer keeps the key of each capture it adds | accepted | [0085](../issues/0085-a-resent-capture-appends-twice.md) | [D28](../decisions/D28-capture-keys.md) |
| [0022](0022-plan-lines-and-period-pages.md) | Plan lines and period pages: the planning axis without a table | accepted | [0087](../issues/0087-a-plan-written-as-words-cannot-be-read-as-a-plan.md) | [D29](../decisions/D29-plan-lines.md), [D30](../decisions/D30-period-pages.md), [D23](../decisions/D23-no-tasks.md), [D15](../decisions/D15-recurrence.md) |
| [0023](0023-a-daily-mark-on-a-metric.md) | A daily mark on a metric: the owner says which readings the writer asks for | accepted | [0090](../issues/0090-the-writer-guesses-which-metrics-are-daily.md) | [D31](../decisions/D31-daily-metrics.md) |
| [0024](0024-no-places.md) | No places: a place is a plain page, and where a day was is the words of the day page | accepted | [0091](../issues/0091-places-cost-more-than-they-give.md) | [D16](../decisions/D16-places.md), [D21](../decisions/D21-location-history.md): rewritten by the change that follows |
| [0025](0025-no-plan-lines-and-connected-apps.md) | No plan lines: a plan lives in the owner's task manager, reached through a catalog the core aggregates | accepted | [0092](../issues/0092-plan-lines-cost-more-than-they-give.md) | [D29](../decisions/D29-plan-lines.md), [D23](../decisions/D23-no-tasks.md), [D15](../decisions/D15-recurrence.md), [D30](../decisions/D30-period-pages.md): rewritten by the change that follows |
| [0026](0026-no-agent-inside-and-no-whatsapp.md) | No agent inside and no WhatsApp: an agent talks to aminte through the CLI and the MCP server | accepted | [0093](../issues/0093-three-ways-for-an-agent.md) | none |

Status values: draft | accepted | rejected (with a one-line reason) | withdrawn.

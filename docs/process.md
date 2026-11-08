# How a change happens

The schema changes only when real use asks for it (principles 6 and 7 of the
[goals](architecture/goals-and-principles.md)). This page is the path a change takes, and where each step is written down.

```
incident ──▶ issue ──▶ proposal (RFC) ──▶ decision (ADR) ──▶ schema.sql + pages + suites ──▶ plan (optional)
```

1. **Issue** — [issues/](issues/README.md). Something real went wrong or could not be done: a failed import, a bug in
   the writing application, a question the data could not answer, a rule that is ambiguous or untestable. One file
   per incident, from the [template](issues/template.md). A wish of an application or a hypothetical writer is not
   an issue.
2. **Proposal** — [rfcs/](rfcs/README.md). The change that would fix one or more issues: the options, what each costs
   after the freeze (additive only, [D13](decisions/D13-migrations-and-freeze.md)), and what it makes redundant. One file per
   proposal, from the [template](rfcs/template.md). A small fix that changes no decision may skip this step.
3. **Decision** — [decisions/](decisions/README.md). An accepted proposal becomes a new decision, or rewrites an existing
   one in place. Numbers are stable and never reused.
4. **Change** — [schema.sql](schema/schema.sql) is edited in place (until the freeze), with the contract pages, the
   cookbook, the diagrams and the suites it touches, in one commit; every suite ends green.
5. **Plan** — [plans/](plans/README.md). When a change is large enough to hand to another agent, it is written as a
   self-contained plan with done criteria. The plans index holds the status of a plan, and a plan is DONE when it is
   live. The plan file holds the record: its dates, its commits and its checks.

## Before the freeze

The freeze is defined in [D13](decisions/D13-migrations-and-freeze.md). Each item is checkable from the repo:

1. Every plan in [plans](plans/README.md) is DONE or REJECTED, and every [issue](issues/README.md) is resolved or won't-fix.
2. The suites in `tests/` are green (`go test ./tests`, on a SQLite ≥ 3.53 with FTS5), at the commit of `schema.sql`
   that will make the file.
3. No [non-goal](architecture/non-goals.md) has the freeze as its "Reopen when": the owner decides each such row first —
   kept out, or reopened through an issue (snapshots: [D25](decisions/D25-snapshots.md)).
4. The text the file will keep has been read once more as a stranger would: the comments inside the `CREATE`
   statements, the `aminte_meta` rows and the `link_kinds` notes ([threat model and the 2075 test](contract/threat-model.md)).
5. The writer that will make the file reproduces every vector of [titles and wikilinks](contract/titles-and-wikilinks.md) and passes the
   four [integrity checks](contract/integrity-checks.md) on a copy.
6. A program import ran on a copy and its counts are recorded; a second run wrote nothing ([imports](contract/imports.md)).
   An import the owner makes with an agent comes after the freeze, since no program can run it again.

The freeze itself is the first kept write that a rebuild would lose (D13); the status line of [the docs index](README.md) records the commit.

## Current truth and history

| folders | what they are |
|---|---|
| `architecture/`, `schema/`, `contract/`, `decisions/`, `cookbook/`, `guides/`, `research/` | **current truth only**: today's rule and its reasons, never the story of how it got there. When something changes it is rewritten in place; git is the log. |
| `issues/`, `rfcs/`, `plans/` | **records**: dated, written once, closed with a status. They cite the docs as they were at their commit. |

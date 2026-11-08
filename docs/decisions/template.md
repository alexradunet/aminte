# Dn — The decision, in one sentence.

**Status:** accepted | deferred

- **Context.** The incident or question that forced the decision ([an issue](../issues/README.md) or
  [a proposal](../rfcs/README.md)), and what was true then.
- **Decision.** What the schema does, citing the constraints, triggers or `aminte_meta` keys that carry it in
  [schema.sql](../schema/schema.sql). The rule itself lives there or in the [storage contract](../contract/README.md);
  this record says *why*.
- **Alternatives.** What else was considered, and why each was rejected.
- **Trade accepted.** What this costs, and the mitigation if the cost ever becomes real.
- **Sources.** Reference ids from [references](../research/references.md); a claim about what SQLite does is
  marked *executed* only when a suite in `tests/` runs it.

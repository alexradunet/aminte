# Goals and design principles

**The goal:** one database that a single human writes to for ~50 years and that remains
readable and meaningful long after the current application is gone. The dominant risk is
not SQLite durability (settled — see [D1](../decisions/D01-single-sqlite-file.md)); it is *meaning living only in application code*,
and *canonical data being corrupted by uncontrolled writers*. A plan lives outside the file
([D23](../decisions/D23-no-tasks.md), [D29](../decisions/D29-plan-lines.md)); nothing turns an intention into an observed fact: a reading is recorded on its own.

**Principles, in priority order:**

1. **Pareto / KISS.** Choose the 20% of mechanism that delivers 80% of the value. Every
   table and column must justify itself against "what if we just didn't have it."
   Complexity that was researched and *deliberately cut* is listed in [non-goals](non-goals.md) so it is not
   silently re-added. The freeze makes this urgent: after it, change is additive only
   (principle 4), so whatever is in the schema then stays.
2. **The file explains its records.** Real tables, real column names, real types, real
   constraints. A stranger in 2075 should be able to read the stored facts and their interpretation limits
   from `.schema` and `SELECT * FROM aminte_meta`: each table's summary is *inside* its `CREATE`
   statement, the only comments the file keeps, and `aminte_meta` holds the few rules that span tables.
   Exact [writer conformance](../guides/building-a-writer.md) also requires the matching contract and vectors;
   the [2075 test](../contract/threat-model.md) checks the in-file summary, not that every algorithm is self-contained.
   See SQLite's "application file format" argument [R1](../research/references.md#r1).
3. **Single writing application.** One application (later with CLI/API/agent
   front-ends) owns all writes to the database. Many
   *processes* are fine — one *writer* owning the conventions. No other app is ever
   pointed at the canonical data with write access (see [D3](../decisions/D03-integer-ids.md)).
4. **Additive-only evolution after freeze.** After the freeze ([D13](../decisions/D13-migrations-and-freeze.md) says what it is), schema changes are
   additive ([D13](../decisions/D13-migrations-and-freeze.md) says what that allows), in numbered forward-only migrations.
   This preserves stored data; reader and writer compatibility still requires the version checks described
   in [D13](../decisions/D13-migrations-and-freeze.md), especially for renames and changed constraints.
5. **Derived data is disposable.** The FTS index can be rebuilt from canonical data. Normalized name keys are computed from their owned spellings; recomputing them must preserve ownership and reject collisions. `life.db` is irreplaceable.
   (A file's preview is not a disposable cache: its original is not in the file, so the preview may be the
   only remaining picture — [D9](../decisions/D09-binary-files.md).)
6. **One home per concept, one home per rule.** A concept is stored once ([D6](../decisions/D06-mood-is-a-measurement.md): mood lives in
   `measurements` and nowhere else; a named entity's preferred name is its owned registry spelling, [D20](../decisions/D20-named-pages.md)), and a rule is
   written once ([the docs index](../README.md) says where each kind of rule lives). A fact that can be derived from another column is not
   stored beside it.
7. **Real use drives change.** A new constraint, trigger or convention needs a real incident behind
   it — a failed import, a bug in the writing application, a question the data could not answer —
   or it must replace something it makes redundant. Hypothetical writers are not incidents.

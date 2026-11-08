# Storage contract

These conventions are part of the schema's meaning but too long or too procedural for a constraint or a
comment. Every rule that fits in the file is there instead: a table's in its `CREATE` statement, a rule that
spans tables — time, identity, provenance, deletion — in a row of `aminte_meta` ([schema](../schema/README.md)).

| page | what it holds |
|---|---|
| [Prose, wikilinks and titles](titles-and-wikilinks.md) | the save contract, the wikilink and `#tag` grammar and its vectors, renames, titles, `title_key`, day pages |
| [Integrity checks](integrity-checks.md) | the four checks that tell whether a file still obeys the schema |
| [Connection setup](connections.md) | the pragmas every writer sets and reads back, the version floor, `BEGIN IMMEDIATE`, read-only readers |
| [Threat model and the 2075 test](threat-model.md) | what is protected and from what; the questions the file alone must answer |
| [Imports](imports.md) | the import path for data that already exists elsewhere, and its traps |

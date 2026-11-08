# Exact calendar days and UTC instants

The storage rules are the named date/instant CHECKs and `aminte_meta.days` / `.instants` in
[the schema](../schema/schema.sql); [D10](../decisions/D10-time-model.md) explains why. These boundary vectors
are shared by writers and storage validation (executed). They are exact values, not partial or uncertain dates.

| type | value | accepted |
|---|---|---|
| day | `0000-01-01` | yes |
| day | `0000-02-29` | yes |
| day | `2000-02-29` | yes |
| day | `9999-12-31` | yes |
| day | `1900-02-29` | no |
| day | `2026-02-31` | no |
| day | `2026-13-01` | no |
| day | `2026-9-03` | no |
| day | `-0001-01-01` | no |
| day | `-001-01-01` | no |
| day | `10000-01-01` | no |
| day | `２０２６-01-01` | no |
| instant | `0000-01-01T00:00:00.000Z` | yes |
| instant | `0000-02-29T23:59:59.999Z` | yes |
| instant | `9999-12-31T23:59:59.999Z` | yes |
| instant | `2026-01-01T24:00:00.000Z` | no |
| instant | `2026-01-01T24:59:59.000Z` | no |
| instant | `2026-01-01T23:60:00.000Z` | no |
| instant | `2026-01-01T23:59:60.000Z` | no |
| instant | `2026-01-01T00:00:00Z` | no |
| instant | `2026-01-01T00:00:00.00Z` | no |
| instant | `2026-01-01T00:00:00.0000Z` | no |
| instant | `2026-01-01T00:00:00.000+00:00` | no |
| instant | `-0001-01-01T00:00:00.000Z` | no |
| instant | `-001-01-01T00:00:00.000Z` | no |
| instant | `10000-01-01T00:00:00.000Z` | no |
| instant | `２０２６-01-01T00:00:00.000Z` | no |

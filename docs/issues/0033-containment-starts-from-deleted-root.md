# 0033 — Recursive active reads traverse deleted roots or intermediates

- **Date:** 2026-10-07
- **Status:** resolved
- **Seen in:** a synthetic three-level containment query on `90c062e`.

## What happened

Tombstoning Japan still returned a day linked to Tokyo through Kanto when the containment recipe was asked about Japan. The existing leaf-only test expected no results for a tombstoned requested place but did not exercise descendants.

The category recipe likewise returned metrics below a tombstoned root or intermediate category. It filtered displayed rows but walked deleted nodes.

## Reproduce

Create three place identities and `Tokyo located-in Kanto located-in Japan`, with one day linked `at` Tokyo. Tombstone Japan and execute [inside a place](../cookbook/inside-a-place.md) with its id. The raw recursive seed still traversed live descendants.

## Rules involved

The active-read policy in `aminte_meta.deletes`, [D11](../decisions/D11-tombstones.md), the [containment recipe](../cookbook/inside-a-place.md) and [category recipe](../cookbook/metrics-by-category.md).

## Resolution

Both seeds select live identities; category recursion also checks each traversed child. Regressions cover deleted roots, deleted intermediates, direct live descendant reads, retained links, revival and a live alternative path through a cyclic category graph. Mutants restore each missing filter. Validation is recorded in [plan 075](../plans/075-schema-validation-depth.md).

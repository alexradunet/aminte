# 0005 — A place has a point and a radius; a photo's place and day are taken when it is kept

- **Date:** 2026-10-04
- **Status:** accepted
- **Answers:** 0010, a photo's place cannot become an `at` link (resolved; `git log -- docs/issues`)

## Problem

A photo's position must become an `at` link from the day it was taken to a place, with no position stored and no
track kept; a position near no known place is asked about once; Home and Work are recognised and never linked
(issue 0010).

## Options

- **A. The places' coordinates outside `life.db`** (a writer's file). No schema change, and plan 029's "no coordinates
  in `life.db`" stays whole; but what the matching knows is lost with that file, lives outside the one file to keep,
  and differs between writers.
- **B. A point and a radius on a place, inside `life.db`.** A new table `places`, keyed by `pages(id, entity_type)` as
  `people` is, optional (a place without a row has no point):
  - `lat`, `lon`: WGS84 degrees, a point and never 0°, 0° (how photo metadata says "no location"; D21's kept design);
  - `radius_m`: the circle a position must fall in — a café ~100 m, a city ~10 km;
  - `link_days`: 1, a photo here makes an `at` link from its day; 0, the place is recognised (so never asked about)
    and not linked — home, work.

  A position is matched to the live places whose circle holds it, **the smallest circle first, then the nearest**, so
  Café Lume wins over Lisbon and "where was I" climbs to Lisbon by `located-in`. The distance needs no math function:
  the writer binds the metres per degree of longitude at the position's latitude and the query uses `+ - *` on the
  squared equirectangular distance (D21's kept design, executed then against a haversine oracle: within 0.5 % of the
  great-circle distance in a city; the antimeridian is a known limit). The photo's coordinates are never stored: the
  `at` link is what is kept. Additive (one table); the 2075 test gains a question.
- **C. A city from an offline gazetteer, no point at all.** Automatic, but city-level only, and it creates many pages.
- **Rejected for this proposal: `positions`, a GPS track** ([D21](../decisions/D21-location-history.md)'s other half).
  The owner does not want movements kept; the `at` link answers "where was I that day".

**The photo's day and its page** (writer rules, no schema): a file's `pages.day` is the local day the file was made
when the file says so — a photo's EXIF `DateTimeOriginal`, the camera's local clock, as `aminte_meta.days` asks ("the
local calendar of the device that captured it") — else the day it is kept. A file with a picture and a day of its own
is appended once to that day's page as `![[title]]`, so the day shows it.

**Asking.** A position no circle holds writes no link: the writer reports it. One photo at a time, the owner names the
place (and its radius), which takes the photo's position as its point if it has none. A whole export is clustered
first, each cluster asked once in a file the owner approves, with its count, its days, a map link and a nearby city
from an offline list — never an online geocoder, which would send the positions away.

## Recommendation

**B**, as the owner chose: the matching knowledge belongs to the place and stays with the one file; a point per place
is not a track. It revises plan 029's "no coordinates in `life.db`" for places only. A per-place radius replaces
D21's kept bound radius: a city and a café cannot share one.

## Validation

A `places` suite (or the `named` suite grown): the composite key; `lat`/`lon` ranges, NaN and infinity refused, 0°, 0°
refused; `radius_m` and `link_days` ranges; edited in place, never deleted, `updated_at` bumped; a place without a row
is still whole (the orphan query unchanged). The recipe's match run literally against a haversine oracle on random
positions (the smallest circle, then the nearest; a tombstoned place never matched; the antimeridian limit stated),
the `at` link and the embed appended once, a place with `link_days = 0` matched and not linked. The 2075 question. A
mutant per rule.

## Outcome

Accepted: [D21](../decisions/D21-location-history.md) rewritten in place (a place has a point and a radius; a photo's place
becomes an `at` link when it is kept; no track), [D16](../decisions/D16-places.md) and [D9](../decisions/D09-binary-files.md) amended; plan 031 done
(`git log -- docs/plans`). Keeping the few photos chosen for a day, from any library, was plan 033 (done; plan 032, a Google export taken
wholesale, was rejected).

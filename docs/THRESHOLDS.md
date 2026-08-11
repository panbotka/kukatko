# Distance thresholds on the image-embedding path

Every cosine-distance constant in Kukátko belongs to the model that produced the vectors it
is compared against. A distance is not a property of two photos, it is a property of two
photos *as seen by one embedding model*, and two models trained separately share no scale —
CLIP's contrastive softmax and SigLIP 2's sigmoid loss spread their similarity distributions
differently, so the same number means a different thing on each side of a model change.

That makes the numbers here **derived, not chosen**. This document records what they are,
what they were, how each was measured, and how to redo the measurement the next time the
image tower changes. A constant with no recorded derivation is the thing this file exists to
prevent.

Faces are a separate space (InsightFace/ArcFace, 512-dim) and were untouched by the SigLIP 2
migration; the face thresholds — `faces.suggestion_max_distance`, `cluster.threshold`,
`cluster.suggestion_max_distance`, `candidates.max_distance`, `review.outlier_threshold`, the
sweep's default confidence — are **not** on this path and are not covered here.

## The inventory

| Constant | Configured in | CLIP ViT-L-14 | SigLIP 2 | Derivation |
| --- | --- | --- | --- | --- |
| `duplicate.embedding_max_dist` | `internal/config` (`setDefaults`), `config.example.yaml`; consumed by `internal/duplicates` and `internal/embedjob` | 0.05 | **0.028** | equal-pair-count match, [below](#duplicate-detection-005--0028) |
| `expand.max_distance` | `internal/config`, `config.example.yaml`, fallback `expand.DefaultMaxDistance` | 0.30 | **0.20** | equal-neighbour-density match, [below](#collection-expansion-030--020) |
| expand slider bounds + default | `web/src/lib/expandSearch.ts` | 20–80 %, default 70 % | **65–90 %, default 80 %** | the default mirrors `expand.max_distance`; the bounds are read off the library's own pair distribution |
| `review.band_min` / `band_max` / `sure_min` | `internal/config`, `internal/review` | 0.45 / 0.75 / 0.80 confidence (= distance 0.55 / 0.25 / 0.20) | unchanged | **shared with the face space** — measured but deliberately not moved, [below](#the-shared-review-bands) |

Semantic and hybrid search carry no distance constant: `photoapi` runs the vector search with
`maxDistance = 0` (rank only) and fuses with full text by Reciprocal Rank Fusion, which is
scale-free. `GET /photos/{uid}/similar` and the MCP `find_similar_photos` tool are likewise
rank-only. Nothing there needed recalibrating.

## The method

The rule is: **reproduce the behaviour, not the number.** A threshold is a promise about how
much gets through it, so the only thing worth carrying across a model change is the size of
what it admits.

The awkward part is that the two models are never in the database at the same time — the
migration that widens the column empties it. So the old side has to be captured *before* the
new model is deployed, from the live instance, through the API. That is what makes this
sequence work:

**1. Sample the real library, not a benchmark.** Two samples, because the two ends of the
scale need different things:

- *contiguous windows* (used here: 4 × 600 photos taken from evenly spread offsets of the
  default listing) for the **tight** end. Duplicates sit next to their twins in time, so a
  window keeps both ends of a duplicate pair inside the sample.
- *a uniform random draw* (used here: 1447 photos) for the **loose** end. Only a uniform
  subsample has the property that a within-sample rank scales to a library rank.

**2. Capture the old behaviour from the running instance.** `GET /photos/{uid}/similar?limit=N`
is a plain kNN over the whole library with the distances attached, it needs only a viewer
session, and it is the only way to see the outgoing model's distances once its vectors are
gone. Pull it for every sampled photo (`limit=50` is enough for duplicates, `limit=100` for
the loose end) and store it.

**3. Re-embed the same photos with the new model.** Fetch `GET /photos/{uid}/thumb/fit_720` —
the same preview `internal/embedjob` sends — and POST each to the sidecar's `/embed/image`.
Nothing is written to any database; this is a measurement, not a backfill. The box does about
30 images/s, so a few thousand photos is a couple of minutes.

**4. Match on behaviour.** Which quantity to hold fixed depends on the threshold:

- **A tight threshold** (duplicates) is matched on the **count of flagged pairs**, replaying
  the production rule exactly: for each photo its K nearest neighbours (K =
  `duplicates.defaultNeighbours` = 8, what `vectors.FindDuplicatePairs` asks the HNSW index
  for), keep the ones within the threshold, count pairs whose two ends are both inside the
  sample. Then bisect the new threshold until the count matches. This is exact as long as
  the K-neighbour cap does not bind — check that by counting the same pairs without the cap;
  if the two agree, sample size has dropped out of the comparison entirely.
- **A loose threshold** (expansion) admits a whole neighbourhood, so it is matched on
  **neighbour density**: how many library photos lie within it. The old side reads that off
  directly (a neighbour list is a rank-ordered walk over the library, so rank *k* at distance
  *d* means *d* admits *k* of *N*). The new side only covers a subsample of size *n*, in which
  each library photo appears with probability *n/N*, so a within-sample rank *k'* estimates a
  library rank of *k'·N/n*. Compare old rank *k* against new rank *k·n/N*, per photo, and read
  off the distance each model puts there.

**5. Hand-check what the new threshold flags** and write the number down. A threshold that
reproduces the old count can still be admitting a different, worse population.

## Measurement, 2026-08-12

Library: 20 636 photos on the production instance. Old vectors: CLIP ViT-L-14 (768-dim), read
from the instance running commit `48c7c05`, i.e. before migration 0057. New vectors: the
sidecar's `ViT-SO400M-14-SigLIP2-378` / `webli` at 1152-dim fp16, computed for this
measurement over the same photos' `fit_720` previews.

### Duplicate detection: 0.05 → 0.028

Sample: 4 × 600 contiguous photos = 2400. Rule replayed: K = 8 neighbours per photo, both ends
of a pair inside the sample.

CLIP at the deployed 0.05 flagged **440 directed / 222 undirected pairs**, touching 276 of the
2400 photos. The same count computed two ways — the global top-8 filtered to the sample, and
the top-8 within the sample — agreed exactly at every threshold up to 0.06, which is what
licenses comparing a whole-library scan against a sample-sized one at this end of the scale.

SigLIP 2 over the same 2400 photos:

| distance | directed | undirected | photos | undirected without the K cap |
| ---: | ---: | ---: | ---: | ---: |
| 0.010 | 90 | 45 | 88 | 45 |
| 0.015 | 152 | 76 | 129 | 76 |
| 0.020 | 244 | 122 | 196 | 122 |
| 0.025 | 364 | 182 | 268 | 182 |
| **0.028** | **446** | **223** | **317** | **223** |
| 0.030 | 514 | 257 | 357 | 257 |
| 0.050 | 1431 | 730 | 696 | 732 |

Bisecting for the CLIP count lands on **0.0280** — 446 directed / 223 undirected against a
target of 440 / 222, i.e. within one pair. The K cap does not bind there (223 = 223), so the
number does not depend on the sample being 2400 photos rather than 20 636.

Rounding up to 0.03 would flag 257 undirected pairs, ~16 % more than before, which is outside
the sampling noise on 222 pairs (≈ ±15). Hence the un-round 0.028: it is a measurement, and
the third decimal is doing work.

### Precision of what 0.028 flags

40 of the 223 undirected pairs were drawn at random and looked at side by side:

- **13 exact duplicates** — the same image twice (re-imports, two scans of one document).
- **23 same-moment near-duplicates** — consecutive frames of one shot: a ribbon being cut, a
  group photo taken twice, a bell tower framed slightly differently. This is what near-duplicate
  review exists to collapse, so they count as true positives.
- **4 false positives** — three of them different *pages* of the same handwritten letter
  (identical paper, identical hand, different words), one a wider and a closer photograph of
  the same chapel with a differently arranged group.

**Precision 36/40 = 90 %.** The failure mode is scanned documents that share a layout, and it
is inherited rather than introduced: 3 of the 4 false positives were already inside the old
0.05 CLIP threshold. Overall 27 of the 40 pairs were flagged by both models. Of the 13 the new
threshold finds that CLIP did not, 12 are genuine — including three exact duplicates that CLIP
had placed at 0.13–0.16, far outside its own threshold. Same-sized net, slightly better catch.

### Collection expansion: 0.30 → 0.20

Sample: 1447 uniformly drawn photos, so one within-sample rank = 14.3 library ranks. Matching
per photo at equal neighbour density:

| CLIP threshold | usable photos | SigLIP 2 p25 | **p50** | p75 | ratio |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 0.15 | 192 | 0.0805 | 0.1052 | 0.1187 | 0.70 |
| 0.20 | 432 | 0.1118 | 0.1370 | 0.1625 | 0.69 |
| 0.25 | 543 | 0.1414 | 0.1705 | 0.1959 | 0.68 |
| **0.30** | **424** | **0.1732** | **0.2011** | **0.2243** | **0.67** |
| 0.35 | 217 | 0.2087 | 0.2412 | 0.2655 | 0.69 |
| 0.40 | 106 | 0.2392 | 0.2809 | 0.3140 | 0.70 |

(A photo is unusable at a given threshold when its 100-neighbour list either never reaches the
threshold or reaches it before the equivalent within-sample rank 1; both are censored rather
than guessed.)

**0.30 → 0.201**, taken as **0.20**. The ratio is flat at 0.67–0.70 across the whole loose
region, which is the cross-check: a mapping derived at 0.15 and one derived at 0.40 agree.

Note that this ratio is *not* the duplicate one — 0.028/0.05 = 0.56. SigLIP 2 compresses the
near-duplicate end harder than the merely-similar end, which is exactly why each threshold has
to be measured where it lives instead of scaling the whole table by one factor.

### The SigLIP 2 distance landscape

Useful for sanity-checking any new threshold, from the 1447-photo random sample (1 046 181
pairs), the share of **random** library pairs a distance admits:

| distance | 0.10 | 0.15 | 0.20 | 0.28 | 0.30 | 0.35 | 0.40 | 0.55 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| share of all pairs | 0.04 % | 0.16 % | 0.62 % | 4.2 % | 6.5 % | 17.5 % | 37.9 % | 95.6 % |

Percentiles of the same distribution: p1 = 0.219, p5 = 0.288, p25 = 0.371, **median 0.424**,
p75 = 0.475. Two unrelated photos from this library sit at about 0.42 — the whole usable range
is below that, and anything past ~0.35 is no longer a search.

That is where the expand slider's new bounds come from: 65 % (distance 0.35) is the widest net
still worth offering and 90 % (0.10) is as tight as "similar" gets before it means "the same
shot". The old 20–80 % range was borrowed from the face slider and now spans mostly nonsense at
its loose end.

## The shared review bands

`review.band_min` (0.45), `review.band_max` (0.75) and `review.sure_min` (0.80) are confidences,
`1 − cosine distance`, and the review game applies them to **both** kinds of question: face
candidates from the ArcFace space and label candidates from the image space
(`internal/review/queue.go` — `sweep.Params{Threshold: 1 - s.bandMin}` for faces,
`expand.Request{Threshold: 1 - s.bandMin}` for labels). One pair of numbers, two scales.

Measured against the landscape above, the label side of that has drifted badly:

- the label search threshold `1 − band_min` = **0.55 now admits 95.6 % of the library**. The
  fan-out is still bounded by `expand.search_limit` and `MaxCandidates`, so nothing explodes,
  but the search no longer selects anything.
- the confident tier's floor `1 − sure_min` = **0.20 is exactly the new expand default** — the
  distance that means "plausibly belongs in this collection". A tier whose promise is "almost
  certainly yes" is now filled from the merely-plausible.

The numbers are here rather than in the code because they cannot be fixed without either
changing the face behaviour (out of scope for the recalibration — InsightFace did not change)
or splitting the bands into a face set and a label set. **The split is the right fix** and is
left as a separate change: three new config keys (`review.label_band_min` / `label_band_max` /
`label_sure_min`), the tier classification parameterised by space, and the label defaults taken
from this document's mapping — `sure_min` 0.80 → **0.86** (distance 0.20 → 0.137, measured),
`band_max` 0.75 → **0.83** (0.25 → 0.171, measured) and `band_min` 0.45 → **0.62**
(0.55 → ≈0.38, extrapolated at the flat 0.69 ratio since 0.55 is past the measured window).
Until then the review game's label questions are looser than they read.

## Redoing this after the next model change

1. Before deploying the new model, pull `GET /photos/{uid}/similar` for both samples off the
   live instance and keep the JSON. This is the only step that cannot be redone afterwards.
2. Deploy, then re-embed the sampled previews through `/embed/image` and recompute.
3. Match counts for the tight thresholds, densities for the loose ones.
4. Hand-check 40 flagged pairs, record the precision here, and update the table at the top
   together with `internal/config`, `config.example.yaml`, `expand.DefaultMaxDistance` and
   `web/src/lib/expandSearch.ts`.

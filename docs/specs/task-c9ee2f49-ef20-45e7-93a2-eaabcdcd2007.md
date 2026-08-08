# The face half of `repair --dimensions` transforms the bbox the wrong way

`maintenance repair --dimensions` has two halves. The photo half rewrites a quarter-turned
photo's transposed pixel dimensions and is correct. The face half — `RenormalizeTransposedBBox`
in `internal/vectors/geometry.go` and `repairFaceDimensionsSQL` next to it — applies a per-axis
rescale (`x*s, y/s, w*s, h/s` where `s = rawWidth/rawHeight`). Its doc comment justifies that
with "the detector's pixel box is in display space either way — the embeddings sidecar
auto-rotates before detecting — so only the divisors were wrong."

**That assumption does not hold for the rows in the live catalogue.** Those face boxes are in
the RAW (pre-rotation) frame and need a quarter turn, not a rescale.

## The evidence, measured against production

Photo `phqale6fftf3a3v5tn17vtfd3d`: stored 3000x4000 with `file_orientation: 6`; the original
downloaded from storage is really 4000x3000, so the stored pair is the already-oriented one.
`GET /photos/{uid}/faces` returns three PhotoPrism markers (verified by eye to sit exactly on
the faces in the displayed image) and two detections from our own `face_detect` job:

```
det0  bbox = [0.21085, 0.68267, 0.21971, 0.14213]
det1  bbox = [0.32460, 0.44934, 0.06467, 0.06607]
```

Applying the repair's rescale to det0 gives `[0.281, 0.512, 0.293, 0.107]`, which crops a torso
in a red coat. Applying a quarter turn (`x' = 1-(y+h)`, `y' = x`, `w' = h`, `h' = w`) gives a
box whose centre is 0.019 from the "Bohumil Nečas ml." marker's centre — visibly the face.
Repeated across the library through the API: over 4 detection/marker pairs on 3 quarter-turned
photos, the rotation reconciles the two coordinate spaces **4 times out of 4**, the rescale
**0 times**, identity **0 times**.

## Requirements

- Correct the transformation the face repair applies to a bbox recorded against the raw frame
  of a quarter-turned photo, and correct the doc comments that assert the old rationale.
- **Do not simply swap one blind transform for another.** Rows may not all be in the same
  space — the sidecar's behaviour may have differed over time. Decide per row on evidence
  (for example: whether the resulting box lands inside the frame, and where a photo carries
  both a detection and a marker, which candidate transform reconciles them), and leave a row
  alone when the evidence does not support changing it. A face left untouched is recoverable;
  a face moved the wrong way twice is not.
- Keep the repair idempotent and re-runnable, and keep the guard that stops it touching rows
  that are already right. Note the current guard is the fingerprint
  `photo_width = $3 AND photo_height = $2`: once a row is rewritten it no longer matches, so
  make sure a row this task decides to skip can still be picked up by a later run.
- `maintenance scan` (the dry run) must report what the repair would do under the new rule.
- Tests: unit tests for the transform decision, and an integration test using the real geometry
  above (a photo stored 3000x4000/orientation 6 whose raw file is 4000x3000, one bbox in raw
  space, one already correct) asserting the raw-space box ends up on the face and the correct
  one is untouched.

## Out of scope
Do not run any repair against the production instance — this task changes code and tests only.
No schema change is expected; if one is unavoidable, say so in the commit message.
## Context
Found while fixing the ReviewPage flake (commit 1260e16a). The container image is now published by the `publish` job of ci.yml with `needs: [check, integration]`, so ANY flaky test on main or a release tag means no image. On the box, 20 shuffled full-suite runs (two suites at once for load) of c54b395c failed once each on:
- `src/components/map/LocationPicker.test.tsx > LocationPicker ways in > takes the coordinate from a dragged pin`: `expected [ {…}, {…} ] to have a length of 1 but got 2` (a second call or event recorded).
- `src/components/organize/BulkEditControl.test.tsx > BulkEditControl > submits exactly the selected photos and clears the selection on success`: `expect(element).not.toBeInTheDocument()`. The modal (`role=dialog`, class `fade modal`) is still in the DOM, probably a sync assert on a modal that closes through a transition or effect.
Neither showed up in the 20 runs of the fixed tree, so the rate is ≤ 1/20 under heavy load.

## Requirements
- Reproduce each one: run the single file 5–8 copies at once (`npx vitest run <file> &` ×N, see memory kukatko-reviewpage-snapshot-red-on-main) and also with `--sequence.shuffle`. Find the root cause and fix it in the test or the component, never by retry or skip. Show before/after repetition counts.
- `make check` green.

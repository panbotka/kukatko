# Filter bar alignment

In the library filter bar the sort selector sits slightly lower than the name/search filter instead of sharing its line.

## Requirements

- The search input and the sort selector share a common baseline; the selector is no longer pushed down relative to the input.
- The cause is the helper text rendered underneath the search input: it makes that column taller, and the row's centre alignment then pushes the selector down. Fix the layout structure so the helper text no longer affects the selector's position — do not compensate with a hand-tuned margin or padding.
- The fix holds whether or not the helper text is present, at every viewport width, and when the bar wraps onto multiple lines.
- No change to what any control does, only to where it sits.

## Tests

- A component test rendering the filter bar both with and without the helper text, asserting the helper text is not a sibling that shares the alignment group with the sort selector — that is, the two controls remain in the same row container in both cases.

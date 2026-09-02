# Range Selection in the Photo Grid

Bulk curation currently means clicking photos one by one. Add contiguous range selection.

## Requirements

- Shift-click: clicking a photo with Shift held selects the whole contiguous range between the last-interacted (anchor) photo and the clicked one, in the current sort order, merging with the existing selection. Shift-clicking inside an already-selected range stays predictable (extend from the same anchor, no random toggling).
- Works with the virtualized grid: a range can span photos whose tiles are not currently mounted; selection is by position in the loaded list and the selection count stays correct.
- Existing single-click/checkbox toggling and select-mode behavior are unchanged; range selection is purely additive.
- The bulk actions bar reflects the new selection count immediately.
- Unit tests for the anchor/range logic as a pure function; a component test for the shift-click wiring.

## Implementation notes

- If photos load in pages, define the range over what is loaded — do not silently fetch thousands of items to complete a range.
- Update docs/FRONTEND.md.
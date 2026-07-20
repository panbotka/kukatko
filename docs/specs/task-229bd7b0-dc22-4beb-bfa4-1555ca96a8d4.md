# Fix bulk-edit autocomplete being clipped / unusable in the picker modal (desktop + mobile)

When several photos are selected and the user opens a bulk-edit picker that
contains a typeahead/autocomplete — "Přidat do alba" (add to album) and "Štítky"
(manage labels) — the autocomplete suggestion dropdown is clipped and the user
must scroll inside the modal body to see the options. On a phone it is even worse:
the picker is a small centred dialog and the on-screen keyboard covers the
suggestions. Fix the clipping on desktop AND make the picker usable on a phone.

## Current behavior (root cause)

- The bulk pickers live in `web/src/components/organize/BatchActionBar.tsx`
  (the quick "Přidat do alba" / "Štítky" pickers, the `<Modal centered scrollable>`
  around line 260) and the fuller editor
  `web/src/components/organize/BulkEditModal.tsx`.
- The quick pickers are `<Modal centered scrollable>` with NO `fullscreen="sm-down"`
  (unlike `BulkEditModal`, which correctly sets `fullscreen="sm-down"`). The
  `scrollable` prop puts `overflow-y: auto` on `.modal-body` and caps the dialog at
  viewport height.
- The autocomplete is `web/src/components/MultiSelect.tsx`. Its suggestion list is
  an absolutely-positioned `<ul class="dropdown-menu show ...">` (with its own
  `maxHeight: 50vh` and `overflow-auto`) inside a `.position-relative` wrapper that
  sits inside the scrollable `.modal-body`. Because the dropdown is a child of the
  `overflow` box it cannot escape it — it is clipped. On mobile it also drops below
  the field into/behind the on-screen keyboard.

## Requirements

- **Desktop:** in the bulk "Přidat do alba" and "Štítky" pickers, and in
  `BulkEditModal`, the autocomplete suggestion list must be fully visible and
  usable without scrolling inside the modal body — typing a query and seeing the
  matching albums/labels must work at a glance.
- **Mobile (phone, ≤576px):** give the quick pickers `fullscreen="sm-down"` so they
  use the whole small screen, and ensure the suggestion list sits in the modal's
  scroll flow ABOVE the on-screen keyboard (the field + its options must both be
  reachable while the keyboard is up). The already-correct `fullscreen="sm-down"`
  on `BulkEditModal` must be preserved.
- Do not regress the non-bulk usages of `MultiSelect` elsewhere in the app — the
  same component is reused, so verify its other call sites still render correctly.
- The dropdown should size to its content up to a sensible cap and scroll only the
  suggestion list itself when there are many matches — never require scrolling the
  whole modal to reach it.

## Implementation notes

You choose the mechanism. Reasonable options include: dropping `scrollable` on the
quick pickers where it causes the clip, letting the dropdown escape its container
(portal, or removing the `overflow` clipping on the wrapper), and/or widening the
modal so content plus dropdown fit. Whatever you pick must keep the modal from
overflowing the viewport on small screens. Check `web/src/styles/app.css` for the
`.dropdown-menu` token overrides so styling stays consistent.

## Testing

- Add/update component tests (Vitest + Testing Library) for the bulk pickers
  and/or `MultiSelect` asserting the suggestion options are rendered and reachable
  (not inside a clipped/`overflow:hidden` container) when the user types.
- `make check` must pass (Prettier, ESLint, tsc, Vitest, plus Go).
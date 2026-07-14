# Album & label detail: make the back link self-explanatory

On the album detail and label detail pages the way back is a bare arrow — it is not obvious what it does or where it leads.

## Requirements

- The back control carries a **`bootstrap-icons` arrow icon plus a text label** naming the destination, e.g. "Zpět na alba" / "Zpět na štítky" (i18n, `cs` + `en`), not a bare glyph.
- It reads as a link/button (hover and focus states visible, keyboard focusable) and has an accessible name; the icon itself stays decorative (`aria-hidden`), per the repo's `Icon` component convention.
- Same treatment on both the album detail and the label detail page — one shared component, not two copies.
- It navigates to the album list / label list respectively, preserving the list's view state (filters, sorting, page) that lives in the URL query params — "back always works".

## Implementation Notes

- Pages live under `web/src/pages/`; icons go through the existing `Icon` component (`bootstrap-icons` only).
- Check whether other detail pages (subject/person detail) have the same bare-arrow problem; if the shared component fits there too, use it.

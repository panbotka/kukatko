# Bulk selection bar wraps into two rows even on a 1440px desktop

UI/UX review finding #6 (2026-10-11). Selecting photos in the library shows a floating bottom bar: "× Vybráno: 2 | Vybrat vše | Přidat do alba | Štítky | Oblíbené | Nastavit polohu | Archivovat" and on a second row "Stáhnout ZIP | Seskupit vybrané | Zeptat se | Další úpravy" — ten actions that wrap even at 1440px and look crowded.

## Requirements
- The bar fits on one row at 1200px and wider: keep the count, "Vybrat vše", and the five most frequent actions visible (Přidat do alba, Štítky, Oblíbené, Nastavit polohu, Zeptat se — adjust if usage data in the code suggests otherwise) and move the rest (Stáhnout ZIP, Seskupit vybrané, Další úpravy) into a "Další…" dropdown.
- "Archivovat" stays visible as the last item, visually separated from the others (divider or spacing), in the danger outline style it has today.
- Every action keeps its icon, label, keyboard access and role gating exactly as today, just relocated.
- On phone the bar may use the dropdown for everything except count, "Vybrat vše" and the two most frequent actions; it must not overlap the bottom tab bar.
- Update the selection bar tests (menu items, role gating) for the new structure.

## Scope and verification
- UI only: no change to what the actions do.
- Reference screenshot: `<path>/uiux-review-2026-10-11/shots/d-selection.png`.
- Check at 1440px, 1024px and 390px with 2 photos selected.
- Finish per CLAUDE.md Definition of Done (docs/FRONTEND.md, `make check-box`, commit, push).
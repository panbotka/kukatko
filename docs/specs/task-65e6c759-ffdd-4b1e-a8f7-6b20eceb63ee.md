# Phone: the library search field is ~130px wide and the active query is unreadable

UI/UX review finding #7 (2026-10-11). At 390px the library toolbar row is: search field, "?" help button, big "Filtry" button. The field ends up about 130px wide, its placeholder is cut to "Filtrovat —" (a dash with nothing after it) and an active query like `year:2025 album:"Kroje a pouť"` shows as "year:2025 al". The user cannot see what is filtering the grid.

## Requirements
- On phone viewports (below the `md` breakpoint) the search field takes the full remaining width: the "Filtry" button becomes icon-only (funnel icon, aria-label "Filtry") with a small badge showing the number of active structured filters, and the "?" help button moves inside the field as an end adornment or into the Filtry offcanvas header.
- Use a short phone placeholder ("Hledat v knihovně…") instead of the long desktop one; never truncate mid-phrase.
- When a query is active on phone, render it as removable chips under the field (one chip per `key:value` term, free text as one chip) so the active filters are readable at a glance; removing a chip removes that term from the query. The desktop keeps the full text in the field (chips optional there).
- The same component serves favourites, album detail, label detail and trash, so they benefit automatically.
- Tests: the chip rendering from a query string and the icon-only Filtry button on narrow viewports.

## Scope and verification
- UI only: the query language and the URL state are unchanged; chips are just another view of `q`.
- Reference screenshots: `<review shots>/m-root.png`, `m-library-filtered.png`, `m-albums_<album uid>.png`.
- Check at 390px and 320px (no horizontal scroll) and at 1440px (unchanged).
- Finish per CLAUDE.md Definition of Done (docs/FRONTEND.md, `make check-box`, commit, push).
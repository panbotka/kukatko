# Add a persistent brand/home affordance to the navbar

On a phone the top bar is just `[hamburger] [search field]` — there is no logo/title
and no one-tap "home", so the user has no orientation ("what app / where am I") and no
fast path back to the start.

## Current behavior

- `web/src/components/Layout.tsx` (navbar around lines 308-341) has no `Navbar.Brand`;
  all destinations live inside the collapsed menu.

## Requirements

- Add a compact brand/home affordance that stays visible OUTSIDE the collapsed menu on
  all viewports (a `Navbar.Brand` with the app name/logo, or a small home icon on
  mobile if horizontal space with the search field is tight).
- Tapping it navigates to the app's home/default view (the Library).
- It must not crowd the search field on a 320-360px screen — keep it compact (icon or
  short wordmark) and ensure the top row still fits `[brand] [search] [hamburger]`
  (or a sensible arrangement) without horizontal overflow.
- Use the `Icon` component (`bootstrap-icons`) for any icon; give it a ≥44px touch
  target and an accessible label.
- Desktop appearance should gain the brand without disrupting the existing layout.

## Testing

- Add a Vitest test asserting the brand/home link renders and points at the home route.
- `make check` must pass. Update `docs/FRONTEND.md` if a component is added/changed.
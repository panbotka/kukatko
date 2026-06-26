# M1 — Library grid (frontend)

Build the main photo library view: a responsive, virtualized thumbnail grid with filters and
sorting whose state lives in the URL.

## Context
Read `docs/ARCHITECTURE.md` §13 (frontend; filters everywhere, back works). Backend:
`GET /api/v1/photos` (filters/sort/pagination) and `GET /api/v1/photos/{uid}/thumb/{size}`.
Use the urlState hook/convention established in the auth-frontend task. react-bootstrap (Superhero),
i18n (cs/en), mobile/tablet responsive.

## Requirements
- Responsive grid of tile thumbnails (e.g. tile_224/500), columns adapting to viewport;
  **virtualized + infinite scroll** (e.g. `react-virtuoso`) backed by paginated API.
- **Filter bar**: date range, has-GPS, camera, favorite, private, archived toggle; **sort**
  selector (newest/oldest/taken_at/title/size). All filter+sort+scroll state encoded in the URL
  query params; Back/Forward restores the exact view; sharing the URL reproduces it.
- Clicking a tile opens the photo detail route (detail page itself is a later task; navigate to
  `/photos/{uid}`).
- Empty state, loading skeletons, and error states (with retry), all i18n.
- Lazy-load thumbnails; avoid layout shift.

## Quality gate (mandatory)
- `make check` MUST pass (frontend ESLint + Vitest).
- Vitest tests: filter/sort changes update the URL and trigger refetch; Back restores state;
  empty/loading/error rendering; infinite-scroll requests next page. Mock the API.
- Typed components; all visible text via i18n.
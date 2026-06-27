# M4 — Map view (frontend)

Build a map showing geotagged photos using Leaflet with mapy.com tiles (via the backend proxy),
marker clustering, and the mandatory mapy.com attribution + logo.

## Context
Read `docs/ARCHITECTURE.md` §12. Backend proxies tiles at `/api/v1/map/tiles/{mapset}/{z}/{x}/{y}`
and serves photo GeoJSON at `/api/v1/map/photos`; reverse geocode at `/api/v1/map/rgeocode`.
**mapy.com requires** both an attribution link (© Seznam.cz a.s. a další → /copyright) AND a
clickable mapy.com logo control over the map — do not omit these. Use `Leaflet` +
`Leaflet.markercluster`.

## Requirements
- Map page using Leaflet with a tile layer pointing at the **backend proxy** URL template
  (key stays server-side). Mapset switch (basic/outdoor/aerial). Retina support where available.
- **Required controls**: attribution control with the © Seznam.cz link, and a bottom-left
  clickable logo control loading mapy.com's logo linking to mapy.com. These must always be present.
- Load photo GeoJSON (respecting active filters: date range, album/label scope) and render
  **clustered markers**; clicking a cluster zooms, clicking a marker opens a popup with the
  thumbnail linking to the photo detail.
- Map viewport/filters reflected in the URL where practical (so Back works); responsive/touch.
- Loading/empty/error states; i18n (cs/en).

## Quality gate (mandatory)
- `make check` MUST pass (frontend ESLint + Vitest).
- Vitest tests: tile layer uses the proxy URL (no API key in client); attribution + logo controls
  are rendered; GeoJSON markers/clusters render and marker click links to detail; filter changes
  refetch GeoJSON. Mock the API and Leaflet where needed.
- Typed components; all text via i18n.
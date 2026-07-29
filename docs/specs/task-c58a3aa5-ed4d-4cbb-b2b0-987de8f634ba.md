# Style Leaflet map popups and attribution for the dark theme

The map's popups and attribution render in Leaflet's default light styling, which
clashes with the app's dark Superhero theme.

## Current behavior

- `web/src/components/map/LeafletMap.tsx` + `web/src/pages/MapPage.tsx`. A repo-wide
  search found NO `.leaflet-*` CSS overrides, so `.leaflet-popup`, its tip, and the
  `.leaflet-control-attribution` all show Leaflet's default white/light chrome on the
  app's dark background.

## Requirements

- Add CSS overrides so the Leaflet popup surface (`.leaflet-popup-content-wrapper`,
  `.leaflet-popup-tip`), the attribution bar (`.leaflet-control-attribution`), and any
  other Leaflet chrome match the app's dark theme (use the app's existing color tokens
  from `web/src/styles/tokens.css` / `app.css` rather than hard-coded colors).
- Popup text, links, and the close "×" must have adequate contrast on the dark surface.
- Keep the map tiles themselves untouched (only the Leaflet UI chrome is restyled).
- Ensure the styles are scoped so they don't leak outside the map.

## Testing

- Visual/CSS change; add a test only if meaningful (e.g. the override stylesheet is
  imported). `make check` must pass.

Note: touch gesture-handling and control SIZING are handled by a SEPARATE task; this
task is ONLY the dark-theme styling of popups/attribution. Keep them apart.
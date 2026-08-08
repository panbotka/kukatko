# A bright map inside a dark app, and Místa is a single row

Three things across two pages:

1. **The map** uses light base tiles (`Základní`, `Turistická`) inside a dark interface. Going
   from a dark page to a glowing map over a large area is unpleasant, especially on a phone.
   The "Turistická" and "Letecká" style toggles also have poor contrast when inactive (dark
   blue on dark).
2. **Coverage:** "Fotek na mapě: 2 378" out of 20 906, i.e. **11 %**. The page neither explains
   this nor offers anything to do about it, even though the project has `internal/geoestimate`
   for filling in a missing location.
3. **`/places`** shows exactly **one row** for this library: "Česko — 2 351 fotek". Only after
   clicking does the list of municipalities appear (sorted by count, breadcrumb works). The
   rows have no previews at all — places in a photo gallery without a single photo.

Map and Places take two of the four entries in "Procházet". One of them is mostly emptiness,
the other is one row.

## Requirements
- Use a dark base layer if the tile provider offers one; otherwise lay a subtle dark filter
  over the light tiles. Raise the contrast of the inactive style toggles.
- On `/places`, show a preview image for each country/municipality row (the best photo of that
  place) — it is a photo gallery.
- When a level has only one entry, skip it and show the level below directly.
- On the map, state the coverage in human terms ("Na mapě je 2 378 z 20 906 fotek — u ostatních
  není uložená poloha.") and, for roles allowed to do it, link to filling in locations.
- Czech and English translations.

## Where it is
`web/src/pages/MapPage.tsx`, `web/src/components/map/*`, `web/src/pages/PlacesPage.tsx`,
`internal/mapsapi` (tile style choice — the mapy.com key stays server-side).

## Out of scope
No bulk location estimation run, no data changes.
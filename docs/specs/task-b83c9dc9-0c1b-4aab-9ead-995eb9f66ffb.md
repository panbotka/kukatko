# Rework the photo detail layout: full-width photo, controls below

Change the photo detail view so the photo spans the full width of the content area, and the control/info panels (metadata/information, location, edits, and any other side panels) move **below** the photo instead of beside it. The photo becomes larger, and the area under it becomes the home for controls — leave room to add more panels/functions there later.

## Current layout
- `web/src/pages/PhotoDetailPage.tsx` currently renders the photo and its panels in a side-by-side arrangement (photo on one side, a column of panels — information/metadata, location/map, edits — on the other). Find the exact layout container (react-bootstrap `Row`/`Col` or CSS grid) and the panel components it composes (e.g. the metadata panel `web/src/components/photo/MetadataPanel.tsx`, the map/location panel, the edit panel). Enumerate them from the code.

## Requirements
- The photo (image/video viewer) occupies the **full width** of the detail content area, larger than today. Keep the existing next/prev navigation arrows and any overlay controls working.
- Move all the control/info panels **below** the photo, stacked in a full-width region under it. This includes at least: information/metadata, location (map), and edits. Include every panel that is currently in the side column.
- Arrange the below-photo panels sensibly so the space is usable and extensible: a responsive multi-column grid (e.g. metadata / location / edits side by side on wide screens, stacking to one column on narrow screens) is preferred over one long single column. The design should make it easy to drop in additional panels later without another layout rewrite.
- Preserve all existing functionality: editing metadata (incl. any fields like notes), the map/location display, non-destructive edits, faces, rating/favorites, download — everything that works today must still work, just relocated.
- Keep it responsive: verify wide, medium, and mobile widths. On mobile the photo is full-width at the top and panels stack below.
- Respect the project's UI conventions: react-bootstrap + Bootswatch Superhero, `Icon` component with bootstrap-icons only, i18n for any new/changed strings (cs default + en). No layout regressions to the aspect ratio / letterboxing of the image or video poster.

## Constraints / notes
- This is a layout restructure of `PhotoDetailPage.tsx` and how it composes its panels — do not change the panels' internal behavior beyond what relocation requires.
- Keep the URL/back behavior and the detail query string intact.

## Tests
- Update any `PhotoDetailPage` layout test/snapshot affected by the restructure. Add a test asserting the panels (metadata, location, edits) are still rendered (now below the photo) and remain functional. Keep existing photo-detail tests green.

## Verification
- This is visual — run the dev server and open a photo detail: confirm the photo is full-width and larger, the info/location/edits panels sit below it, everything still works, and it degrades cleanly to mobile.

## Done when
- Docs updated: `docs/FRONTEND.md` (photo detail layout — full-width photo, controls below, extensible panel area).
- `make check` and `make dev` pass; commit and push.
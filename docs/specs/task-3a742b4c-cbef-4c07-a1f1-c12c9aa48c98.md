# Cleaner grid tiles

The photo tiles rendered in grids and lists are cluttered with curation controls that belong in the photo detail view.

## Requirements

- Remove the star rating control and the pick/reject (approve/decline) control from the photo tile used in grids and lists.
- Both controls remain fully functional in the photo detail view: rating a photo and flagging it as picked or rejected is still possible there.
- The favourite (heart) control stays on the tile.
- Any hook, style, prop or translation key that existed solely to serve the removed tile controls is removed as well. No dead code and no unused i18n keys may remain, and the strict linter must stay green.
- No capability is lost for keyboard or screen-reader users: everything removed from the tile is reachable in the detail view.

## Tests

- Update the tile's component tests to assert the rating and flag controls are absent and the favourite control is still present.
- Keep or add a test proving the photo detail view still exposes both rating and flagging.

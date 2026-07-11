# Add an "AI note" text field to photos

Photos need a new free-text field, `ai_note`, to hold text produced by an external AI classification pass. It is editable in the UI, settable via the photo edit API (so an automated agent can write it), shown on the photo detail, and included in full-text search.

## Data model
- Add a new SQL migration (use the **next sequential migration number after the highest existing file** in `internal/database/migrations/` — check the directory; do not hardcode a number). `ALTER TABLE photos ADD COLUMN ai_note TEXT NOT NULL DEFAULT ''`. Mirror the existing `notes` column added to the `photos` table (see `internal/database/migrations/0003_photos.sql` for `title`/`description`/`notes`, and `0021_user_note.sql` for the ADD COLUMN pattern).
- Go model + store (`internal/photos`): add `AiNote` (json tag `ai_note`) to the `Photo` struct in `models.go` next to `Notes`; add it to the `MetadataUpdate` struct. Wire it through `store.go`: add to `photoColumns`, `scanPhoto` (correct column order), and the `updateMetadataRow` UPDATE (SET + args). Follow exactly how `notes` is threaded.

## API
- `internal/photoapi/update.go`: add `AiNote *string` to `updateBody`, copy it in `applyScalars`, and seed it from current in `mergeUpdate` — mirroring `notes`. The `PATCH /photos/{uid}` route stays `RequireWrite`.
- The photo detail response already returns the `photos.Photo` JSON, so `ai_note` will be included automatically once it is on the struct — verify.

## Full-text search
- Include `ai_note` in full-text search so a photo can be found by a term that appears only in its AI note. Locate where the searchable text / tsvector is built for photos (the full-text path used by `internal/photoapi/search.go` → `internal/photos` `Search`), and add `ai_note` alongside `title`/`description`/`notes` (respecting the existing `unaccent` handling). If FTS uses a generated/stored tsvector column or index, update it in the migration accordingly.

## Frontend
- `web/src/services/photos.ts`: add `ai_note?: string` to `Photo`, and `ai_note?` to `PhotoMetadataUpdate`.
- `web/src/components/photo/MetadataPanel.tsx`: add an editable field mirroring `notes` — local state, reset-on-edit, a `Form.Control as="textarea"` in edit mode, include it in `buildPatch()`, and a read-only `MetaField` display. Place it near the notes field. Add an i18n label (e.g. `photo.metadata.aiNote`) in `web/src/i18n/locales` for cs (default) and en. Choose a clear Czech label (e.g. "Poznámka AI").

## Tests (mandatory)
- Store/integration test: set `ai_note` via metadata update, read it back.
- HTTP test: `PATCH /photos/{uid}` with `ai_note`, GET detail returns it.
- Search test: a photo whose term appears only in `ai_note` is returned by full-text search.
- Frontend: MetadataPanel test covering edit + save of the AI note if the component has existing tests to extend.

## Done when
- Docs updated: `docs/API.md` (the photo PATCH/detail field) and `docs/FRONTEND.md` (MetadataPanel field). Note the new column in the data-model doc if columns are enumerated there.
- `make check`, `make dev`, integration tests pass; commit and push.
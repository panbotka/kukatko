# Search must not wait 30 seconds for a service known to be offline

On the live instance the embeddings sidecar is offline most of the time, and
`GET /api/v1/capabilities` correctly reports `semantic_search: false`. The frontend asks
for capabilities on startup, receives `false`, and still sends `mode=hybrid` — so every
search blocks for the full 30-second sidecar timeout before falling back to text results
that were available the whole time. Measured on the live instance: capabilities 27 ms,
`GET /api/v1/search?...&mode=hybrid` 30 179 ms, reproducible on two different queries.

Since an offline box is the normal state, this means every search on the instance takes
half a minute of a bare spinner.

## Requirements
- When `semantic_search` is `false`, search requests go out as `mode=fulltext` immediately.
  No request that can hit the sidecar timeout is sent.
- The "Semantic" option in the search-mode selector is `disabled` when the capability is
  false, with a `title` explaining why it cannot be used right now.
- The "content search is temporarily unavailable, showing text results" notice is shown
  **before** the search runs (next to the mode selector / search input), not only after
  results arrive.
- The library page uses the same search backend and must behave identically.
- Shorten the embeddings HTTP client timeout to a few seconds (keep it configurable) so
  that even a wrong availability guess can never cost 30 seconds.
- Separate small fix on the same page: with no query entered, `/search` currently renders
  "Počet fotek: 0" above the "Zadejte hledaný výraz." prompt. Do not render the count until
  a query exists.

## Where it is
`web/src/pages/SearchPage.tsx` (mode selector around line 186),
`web/src/hooks/usePhotoSearch.ts`, `web/src/capabilities/CapabilitiesProvider.tsx`
(today neither the page nor the hook reads capabilities), `internal/embedding` and
`internal/config` for the timeout.

## Out of scope
No schema changes, no data changes.
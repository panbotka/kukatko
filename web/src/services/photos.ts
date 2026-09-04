import { ApiError } from './auth'

/**
 * A photo in the catalogue, mirroring the backend `photos.Photo` JSON shape
 * (`internal/photos/models.go`). Only the fields the library grid needs are
 * declared explicitly; the rest are intentionally omitted to keep the type
 * focused — extend it as later views require more metadata.
 */
export interface Photo {
  uid: string
  file_hash: string
  file_name: string
  file_size: number
  file_mime: string
  file_width: number
  file_height: number
  /** The raw EXIF orientation tag (1–8); 0/absent when the file carried none. */
  file_orientation?: number
  /**
   * A BlurHash of the photo's rendering — a few dozen characters a decoder turns
   * into the blurred stand-in a tile can paint while the real thumbnail loads.
   * Absent means the placeholder has not been computed yet (a photo catalogued
   * before placeholders existed, or one whose original could not be decoded), so
   * anything using it must cope with its absence rather than assume a value.
   */
  blurhash?: string
  /**
   * The file name the photo carried before it was ingested. Only interesting when
   * it differs from `file_name`, the name it has in the storage layout.
   */
  original_name?: string
  taken_at?: string
  taken_at_source: string
  /**
   * Whether `taken_at` is an estimate rather than a known capture time — the
   * scanned photo that is "somewhere in the forties". Presentation only: sorting,
   * the timeline, grouping and the date filters go on using `taken_at` exactly as
   * before.
   */
  taken_at_estimated?: boolean
  /**
   * The dating note in the user's own words ("kolem roku 1950", "za války"). Only
   * kept while `taken_at_estimated` is set — the backend clears it with the flag.
   */
  taken_at_note?: string
  /**
   * How fine `taken_at` actually is: `'day'` (an ordinary date, and what an
   * absent value means), `'month'`, `'year'` or `'decade'`. Anything coarser than
   * a day means `taken_at` is the first instant of that period in UTC, so
   * anything rendering the date must consult this — see `lib/takenDate`. Sorting,
   * the timeline, the period filter and the year facets go on using `taken_at`
   * exactly as before.
   */
  taken_at_precision?: string
  /**
   * The capture date the photo carried at the moment somebody declared its date
   * unknown — the day a scanner stamped on it, the file's mtime an import fell
   * back to. Present only while the photo has no `taken_at`: stating a real date
   * discards the value that was put away.
   *
   * It is provenance, never a second date axis. Nothing sorts, groups or filters
   * by it (see migration 0066); the detail drawer shows it as what was disowned
   * and offers to put it back, which is an ordinary date edit.
   */
  taken_at_before_unknown?: string
  title: string
  description: string
  lat?: number
  lng?: number
  /**
   * Where `lat`/`lng` came from: `'exif'` (the file's GPS), `'manual'` (the user
   * decided), `'estimate'` (inferred from photos taken nearby in time) or `''`
   * (unknown — an older row nobody has decided about).
   *
   * Only `'estimate'` is marked in the UI. An estimated location looking identical
   * to a measured one is a lie the app tells the user, so anything rendering a
   * position must consult this. `'manual'` with no coordinates is not a
   * contradiction: it records that the user cleared the location on purpose, which
   * is what stops the backfill re-adding a guess they threw away.
   */
  location_source?: string
  camera_make: string
  camera_model: string
  lens_model: string
  /** Capture notes (private annotations); present on detail responses. */
  notes?: string
  /**
   * Free-text note produced by an external AI classification pass. Editable in
   * the UI and settable via the photo edit API; included in full-text search.
   * Present on detail responses.
   */
  ai_note?: string
  /** EXIF capture settings, present when the original carried them. */
  iso?: number
  aperture?: number
  exposure?: string
  focal_length?: number
  /** The camera body's serial number, when the file recorded one. */
  camera_serial?: string
  /** GPS altitude in metres, when geotagged with elevation. */
  altitude?: number
  /**
   * The IPTC/XMP credit block, as the source file wrote it. All user-editable
   * (in `MetadataPanel`) and shown read-only on the detail card.
   */
  subject?: string
  /** IPTC keywords, comma-separated and verbatim — not Kukátko's own labels. */
  keywords?: string
  artist?: string
  copyright?: string
  license?: string
  /**
   * File technicals written by ingest/import: what produced the image, the embedded
   * ICC profile, the still image's compression and a panorama's projection. All
   * machine-derived and read-only — the one exception is {@link Photo.scan}, which
   * no file reliably states and the user therefore corrects in `MetadataPanel`.
   */
  software?: string
  /** Whether the photo is a scan of a physical print rather than a camera capture. */
  scan?: boolean
  color_profile?: string
  image_codec?: string
  projection?: string
  /** Whether the photo is private (hidden from the shared views). */
  private?: boolean
  /** Media kind: `image`, `video` or `live`. Absent is treated as `image`. */
  media_type?: string
  /** Clip length in milliseconds for videos/live photos; absent for images. */
  duration_ms?: number
  /** Primary video codec (e.g. `h264`, `hevc`); empty/absent for images. */
  video_codec?: string
  /** Primary audio codec (e.g. `aac`); empty/absent when there is no audio. */
  audio_codec?: string
  /** Whether the video carries an audio stream. */
  has_audio?: boolean
  /** Average frame rate of the video; absent for images. */
  fps?: number
  /**
   * The source the photo was imported from, when it was: its UID in PhotoPrism or
   * in photo-sorter. Provenance only — the detail card shows it so it is obvious
   * where an imported photo came from.
   */
  photoprism_uid?: string
  photosorter_uid?: string
  archived_at?: string
  /**
   * Whether the photo is hidden from the library: it is left out of the grid and
   * its counts, the timeline, the map and places, the slideshow, the review game
   * and the default search, while staying fully visible in its albums and
   * labels, in favourites and at its own URL. Curation the user set by hand, for
   * the things that belong in the catalogue but not among the photographs
   * (scanned documents, screenshots). It is neither `archived_at` (on its way
   * out, purged after retention) nor "private". Search `hidden:yes` to list
   * them.
   */
  hidden_from_library?: boolean
  created_at: string
  updated_at: string
  /**
   * The stack this photo belongs to, when it is stacked (grouped with the other
   * files of the same shot). Absent for a standalone photo. Only a stack's
   * visible primary ever appears in listings, so a photo carrying this is that
   * primary. See {@link Photo.stack_count} and {@link PhotoDetail.stack_members}.
   */
  stack_uid?: string
  /**
   * How many files the stack holds when this photo is a stacked primary (always
   * ≥ 2); absent/0 for a standalone photo. Drives the grid tile's member-count
   * badge.
   */
  stack_count?: number
  /**
   * Whether the current user has favorited this photo. Present on list, search
   * and detail responses (`internal/photoapi` annotates each photo for the
   * acting user); absent (treated as false) when the favorites backend is
   * unwired.
   */
  is_favorite?: boolean
  /**
   * The current user's star rating for this photo, 0–5 (0 = unrated). Present on
   * list, search and detail responses (`internal/photoapi` annotates each photo
   * for the acting user). Optional — mirroring {@link Photo.is_favorite} — so it
   * defaults to 0 when the ratings backend is unwired or the field is absent.
   */
  rating?: number
  /**
   * The current user's pick/reject flag for this photo. Present on list, search
   * and detail responses; optional (treated as `'none'` when absent), like
   * {@link Photo.rating}.
   */
  flag?: RatingFlag
  /**
   * Where to fetch this photo's grid thumbnail (`tile_500`). The backend decides:
   * with originals on a local disk it is this application's own thumb route, and
   * with them in the object store it is a short-lived signed URL at the media
   * Worker's domain. Put it straight into `<img src>` — never rebuild it from the
   * UID, which cannot produce the signature. Because a signed URL expires, an
   * `<img>` using it must tolerate a stale one; see {@link useThumbSrc}.
   */
  thumb_url: string
  /**
   * Where to fetch this photo's aspect-preserving rendition (`fit_720`) — what
   * the justified photo wall draws, where a tile is the shape of its photograph
   * rather than a square. Same rules as {@link Photo.thumb_url}: the backend
   * mints it, it may be a signed URL, and it may go stale. Absent on a payload
   * from an older backend, in which case the square crop is the fallback.
   */
  preview_url?: string
  /**
   * Where to fetch this photo's untouched original bytes — a signed URL, or the
   * download route with `?original=true`. It never renders a non-destructive
   * edit; for that, link to the plain download route (see {@link downloadUrl}).
   */
  download_url: string
}

/**
 * The per-user personal marking on a photo (`internal/organize` `RatingFlag`) — a
 * neutral, mutually-exclusive icon state. The stored strings are internal (users
 * see icons): `none` (no mark), `pick` (👍 thumbs-up), `reject` (👎 thumbs-down)
 * or `eye` (👁 eye). `pick`/`reject` are kept from the earlier pick/reject flag.
 */
export type RatingFlag = 'none' | 'pick' | 'reject' | 'eye'

/**
 * Response body of `GET /api/v1/photos`. `next_offset` is the offset to request
 * for the following page, or `null` when the current page is the last one —
 * letting an infinite-scroll client page until it is absent.
 */
export interface PhotoListResponse {
  photos: Photo[]
  total: number
  limit: number
  offset: number
  next_offset: number | null
  /** Effective search mode, only present on `GET /search` responses. */
  mode?: EffectiveSearchMode
  /**
   * True when `total` is the size of the ranked set a semantic or hybrid search
   * built out of its bounded candidate pool — the best matches it returned —
   * rather than an exact count of every matching photo. Such a total stops
   * growing once the pool is full, so it must never be presented as a library
   * total. Absent (treated as false) whenever the backend ran a real COUNT:
   * full-text, a filter-only query, a plain list, and a ranked search degraded
   * to full-text.
   */
  ranked_total?: boolean
  /**
   * True when a semantic or hybrid search fell back to full-text because the
   * embeddings sidecar was unavailable, so the UI can tell the user semantic
   * ranking was skipped. Absent (treated as false) on list responses.
   */
  degraded?: boolean
  /**
   * Filter-shaped `q` tokens the search query language did not understand
   * (unknown key or malformed value). They degraded to free text server-side,
   * so results are still meaningful; the UI shows a gentle hint. Absent when
   * every token parsed.
   */
  unknown_tokens?: string[]
  /**
   * Machine-readable reasons the page is what it is, for the cases where an
   * empty result is a decision rather than an absence. The only code today is
   * `person_me_unlinked`: the caller filtered on `person:me` without their
   * account having said which person they are, so the server answered with
   * nothing rather than silently widening the query to the whole library.
   * Absent when there is nothing to say.
   */
  notices?: string[]
}

/**
 * Search ranking mode for `GET /search` (`internal/photoapi`): `fulltext` ranks
 * by Czech-aware full-text relevance, `semantic` by image-vector similarity to the
 * embedded query, and `hybrid` (the default) fuses the two with Reciprocal Rank
 * Fusion.
 */
export type SearchMode = 'fulltext' | 'semantic' | 'hybrid'

/**
 * The mode a search response reports back: one of the requestable modes, or
 * `filter` when the query held only filters (no free text) and the backend ran
 * the plain list path without ranking or embedding.
 */
export type EffectiveSearchMode = SearchMode | 'filter'

/**
 * Public sort aliases accepted by the list endpoint (`internal/photoapi`).
 * `rating` sorts by the acting user's star rating (unrated last); the backend
 * only honours it when the request is scoped to a rating user (which the list
 * handler always is for the signed-in caller).
 */
export type PhotoSort = 'newest' | 'oldest' | 'added' | 'title' | 'size' | 'rating'

/**
 * Every order the list endpoint accepts: the sorts a grid offers, plus the
 * `random` one the slideshow's shuffle plays. `random` is deliberately not a
 * {@link PhotoSort} — it belongs to no picker and is never part of a view's URL
 * state; it is a pseudo-random permutation of the result set, keyed by
 * {@link PhotoListParams.seed}, which is what makes a shuffled show pageable.
 */
export type PhotoOrder = PhotoSort | 'random'

/** Archive-state selector: hide archived (default), include them, or only them. */
export type ArchivedFilter = 'false' | 'true' | 'only'

/**
 * Filters, sort and pagination for a photo list request. Empty strings and
 * `undefined` values are treated as "no filter" and omitted from the query, so
 * the same shape works for both the URL-encoded view state and direct calls.
 */
export interface PhotoListParams {
  limit?: number
  offset?: number
  sort?: PhotoOrder
  /**
   * Seed for `sort: 'random'`, ignored by every other order. The permutation is a
   * function of the photo uid and this string, so a caller paging through a
   * random order must repeat the same seed on every request — a seed that
   * changed between pages would deal a fresh shuffle each time, overlapping the
   * pages and dropping whatever fell between them.
   */
  seed?: string
  archived?: ArchivedFilter
  /** Tri-state filter: 'true', 'false', or '' / undefined for no filter. */
  has_gps?: string
  camera?: string
  q?: string
  /**
   * Capture-time period, lower bound: an RFC3339 timestamp or a YYYY-MM-DD date
   * (read as UTC midnight). Together with {@link PhotoListParams.taken_before} it
   * is the only way the client scopes the time axis — a year, a decade or an
   * arbitrary span are all this one pair, so the backend's single-year `year`
   * param has no caller here. Photos with an unknown capture time are excluded.
   */
  taken_after?: string
  /**
   * Capture-time period, upper bound (inclusive, `taken_at <= …`). The library
   * sends the end of the last day rather than its midnight, so a period covers
   * the whole day the reader picked; see `lib/period` `takenBeforeParam`.
   */
  taken_before?: string
  /**
   * Scope the listing to photos in one or more albums (`album` query param).
   * Several UIDs are comma-joined here and sent as repeated params
   * (`?album=a&album=b`); the backend combines them with AND, so a photo must be
   * a member of every album. A single UID (no comma) is the historical
   * single-album scope. Empty / undefined means no scope.
   */
  album?: string
  /**
   * Scope the listing to photos carrying one or more labels (`label` query
   * param). Like {@link PhotoListParams.album}, several UIDs are comma-joined and
   * sent as repeated params combined with AND. Empty / undefined means no scope.
   */
  label?: string
  /**
   * Scope the listing to photos that contain one or more subjects/people
   * (`person` query param). Like {@link PhotoListParams.album}, several subject
   * UIDs are comma-joined here and sent as repeated params (`?person=a&person=b`)
   * combined with AND, so a photo must contain every chosen person. A subject is on
   * a photo when a named face/region marker links them. Empty / undefined means no
   * scope.
   */
  person?: string
  /**
   * Scope the listing to what one account uploaded (`uploader` query param): a
   * user UID, or the reserved value `'none'` for the photos with no uploader at
   * all — the ones an import brought in, which the uploader facet offers as its
   * "imported" group. Empty / undefined means no scope.
   */
  uploader?: string
  /**
   * Scope the listing to photos in this country (`country` query param, exact
   * match against the reverse-geocoded place). Empty / undefined means no scope.
   */
  country?: string
  /**
   * Scope the listing to photos in this city (`city` query param, exact match).
   * Usually paired with `country`. Empty / undefined means no scope.
   */
  city?: string
  /**
   * Scope the listing to the current user's favorites when set to `'true'`
   * (`favorite` query param). Any other value / undefined means no scope.
   */
  favorite?: string
  /**
   * Minimum star rating filter as a string, `'1'`–`'5'` (`min_rating` query
   * param): keep only photos the acting user rated at least this high. Empty /
   * undefined means no filter.
   */
  min_rating?: string
  /**
   * Pick/reject flag filter (`flag` query param): `'pick'` or `'reject'` keeps
   * only photos the acting user flagged accordingly. Empty / undefined means no
   * filter.
   */
  flag?: string
}

const API_BASE = '/api/v1'

/** Standard backend error envelope (`internal/photoapi/http.go`). */
interface ErrorBody {
  error?: string
}

/** Extracts the backend error message from a non-OK response, if present. */
async function readErrorMessage(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as ErrorBody
    if (typeof body.error === 'string' && body.error !== '') {
      return body.error
    }
  } catch {
    // Body was empty or not JSON; fall back to the status text below.
  }
  return res.statusText || `request failed: ${res.status}`
}

/**
 * Serialises list params into a query string, omitting empty / undefined values
 * so absent filters never reach the backend (which would otherwise treat an
 * empty value as no filter anyway, but a minimal query keeps requests tidy and
 * cache-friendly).
 */
export function buildPhotoQuery(params: PhotoListParams): URLSearchParams {
  const query = new URLSearchParams()
  const set = (key: string, value: string | number | undefined): void => {
    if (value === undefined) {
      return
    }
    const str = String(value)
    if (str !== '') {
      query.set(key, str)
    }
  }
  // A multi-value filter is stored as one comma-joined string but sent as
  // repeated params (?album=a&album=b), the form the backend parses into an AND
  // of memberships. A single UID (no comma) collapses to one param, matching the
  // historical single-album/label scope. Empty segments are dropped.
  const setList = (key: string, value: string | undefined): void => {
    if (value === undefined) {
      return
    }
    for (const item of value.split(',')) {
      if (item !== '') {
        query.append(key, item)
      }
    }
  }
  set('limit', params.limit)
  set('offset', params.offset)
  set('sort', params.sort)
  set('seed', params.seed)
  set('archived', params.archived)
  set('has_gps', params.has_gps)
  set('camera', params.camera)
  set('q', params.q)
  set('taken_after', params.taken_after)
  set('taken_before', params.taken_before)
  setList('album', params.album)
  setList('label', params.label)
  setList('person', params.person)
  set('uploader', params.uploader)
  set('country', params.country)
  set('city', params.city)
  set('favorite', params.favorite)
  set('min_rating', params.min_rating)
  set('flag', params.flag)
  return query
}

/**
 * Fetches a page of photos from `GET /api/v1/photos`. The session cookie is sent
 * automatically (same-origin); the backend filters, sorts and paginates.
 *
 * @throws ApiError with `status` 400 (invalid filter/sort/page) or 5xx so the
 *   caller can render the matching message.
 */
export async function fetchPhotos(
  params: PhotoListParams,
  signal?: AbortSignal,
): Promise<PhotoListResponse> {
  const query = buildPhotoQuery(params)
  const res = await fetch(`${API_BASE}/photos?${query.toString()}`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as PhotoListResponse
}

/**
 * Searches the catalogue via `GET /api/v1/search`. `params.q` is the search text
 * (required by the backend; an empty value yields a 400). `mode` selects the
 * ranking strategy (default `hybrid`); list filters and pagination in `params`
 * apply in every mode. The response mirrors {@link fetchPhotos} and additionally
 * carries the effective `mode` and a `degraded` flag set when a semantic/hybrid
 * search fell back to full-text because the sidecar was offline.
 *
 * @throws ApiError with `status` 400 (missing query / invalid filter) or 5xx so
 *   the caller can render the matching message.
 */
export async function searchPhotos(
  params: PhotoListParams,
  mode?: SearchMode,
  signal?: AbortSignal,
): Promise<PhotoListResponse> {
  const query = buildPhotoQuery(params)
  if (mode !== undefined) {
    query.set('mode', mode)
  }
  const res = await fetch(`${API_BASE}/search?${query.toString()}`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as PhotoListResponse
}

/**
 * A primary or sidecar file backing a photo (`photos.PhotoFile`). Only the
 * fields the detail view needs are declared.
 */
export interface PhotoFile {
  id: number
  role: string
  is_primary: boolean
  file_mime: string
  file_size: number
}

/** A compact album reference on a photo detail response (an inline chip). */
export interface PhotoAlbumRef {
  uid: string
  title: string
}

/** A compact label reference on a photo detail response (an inline chip). */
export interface PhotoLabelRef {
  uid: string
  name: string
}

/**
 * The resolved uploader on a photo detail response: the uploading user's UID plus
 * a human-readable `name` (display name, or username when the display name is
 * empty). Absent (undefined) for photos with no uploader — e.g. items imported
 * from PhotoPrism / photo-sorter — so the detail view shows a neutral fallback.
 */
export interface PhotoUploaderRef {
  uid: string
  name: string
}

/**
 * The photo's cached reverse-geocoded place on a detail response — the hierarchy
 * the background `places` job resolved its coordinate into. It is a *cache* read:
 * the detail endpoint never geocodes on demand (mapy.com credits are metered), so
 * the block is absent for a photo the job has not reached, for one without usable
 * coordinates, and for one with no GPS at all. Individual levels can be empty
 * strings when the geocoder knew no better.
 */
export interface PhotoPlace {
  country: string
  region: string
  city: string
  place_name: string
}

/**
 * Full photo detail (`internal/photoapi` detail handler): a photo plus its
 * files, its album/label memberships (empty arrays when none), the resolved
 * uploader (omitted when the photo has no uploader) and its cached place (omitted
 * when it has none).
 */
/**
 * One member of a stack, as listed in the detail page's variants strip. Each
 * member is its own photo row (a RAW, its JPEG, an exported edit); this is the
 * photo's own `uid`, format and dimensions, with its grid thumbnail and original.
 * Distinct from {@link PhotoFile}, which is a file *within* one photo row.
 */
export interface StackMember {
  uid: string
  file_name: string
  media_type: string
  file_mime: string
  file_width: number
  file_height: number
  file_size: number
  is_primary: boolean
  thumb_url?: string
  download_url?: string
}

/**
 * One per-photo computation the library runs in the background. The names are
 * the backend's job types (`internal/jobs`), in the order the detail page shows
 * them. `storyboard` is deliberately not among them: a video's scrub preview is
 * rendered lazily on first playback and leaves no persisted evidence, so it has
 * no state to report.
 */
export type ProcessingStep =
  | 'metadata'
  | 'thumbnail'
  | 'image_embed'
  | 'face_detect'
  | 'ocr'
  | 'places'
  | 'sidecar'

/**
 * What a step's row says about the work: it landed (`done`), a worker has it
 * (`running`), it waits in the queue (`queued`), the last attempt errored
 * (`failed`), it can never apply to this photo or its feature is off
 * (`skipped`), or it has simply never run (`pending`).
 */
export type ProcessingState = 'done' | 'running' | 'queued' | 'failed' | 'skipped' | 'pending'

/**
 * One entry of a photo detail's `processing` array. `at` is when the evidence
 * was recorded and is present only for `done`; `error` is the last failure's
 * message and only for `failed`. The two step-specific fields say what a
 * completed step actually produced, so a legitimately empty result does not read
 * as a gap: `face_count` on `face_detect` (0 = looked at, no face) and
 * `text_found` on `ocr` (false = read, no text).
 */
export interface PhotoProcessing {
  step: ProcessingStep
  state: ProcessingState
  at?: string
  error?: string
  face_count?: number
  text_found?: boolean
}

export interface PhotoDetail extends Photo {
  files: PhotoFile[]
  albums: PhotoAlbumRef[]
  labels: PhotoLabelRef[]
  uploader?: PhotoUploaderRef
  place?: PhotoPlace
  /**
   * What the library has already computed about the photo — one entry per step,
   * in a fixed order. Absent when the instance wires no processing service (and
   * on older fixtures), which the UI reads as "nothing to show" rather than as an
   * empty report.
   */
  processing?: PhotoProcessing[]
  /**
   * The stack's variants strip: every file of this photo's stack (this photo
   * among them), the primary first. Absent/empty for an unstacked photo.
   */
  stack_members?: StackMember[]
  /**
   * How many live comments the photo carries. Always present on a detail response
   * (0 when there are none) so the viewer can badge the conversation without
   * fetching the thread; optional here only so older fixtures stay valid.
   */
  comment_count?: number
}

/**
 * Partial metadata update for `PATCH /api/v1/photos/{uid}`. An omitted key leaves
 * the field unchanged; `null` clears a nullable field (`taken_at`, `lat`, `lng`).
 * Mirrors the backend `updateBody`.
 */
export interface PhotoMetadataUpdate {
  title?: string
  description?: string
  notes?: string
  ai_note?: string
  taken_at?: string | null
  /** Marks `taken_at` a guess; clearing it also clears `taken_at_note` server-side. */
  taken_at_estimated?: boolean
  /** Free-text dating note, at most 500 characters (a longer one is answered 400). */
  taken_at_note?: string
  /**
   * The IPTC/XMP credit block. The backend trims each value and caps its length
   * (`creditLimits`): 1000 characters for `subject`/`copyright`/`license`, 255 for
   * `artist` and 2000 for the whole `keywords` string — a longer one is answered
   * 400. `keywords` is one comma-separated string, not a list.
   */
  subject?: string
  keywords?: string
  artist?: string
  copyright?: string
  license?: string
  /** Whether the photo is a scan of a physical print rather than a camera capture. */
  scan?: boolean
  lat?: number | null
  lng?: number | null
  /**
   * Accepts an estimated location, promoting it to the user's own decision. The
   * only accepted value is `'manual'`, and only on a photo that has a location —
   * `'exif'` and `'estimate'` are the server's to write, not a client's to claim.
   *
   * Sending the coordinates back would work too, but would round them to whatever
   * precision the form rendered, so accepting has its own key. To clear an
   * estimate, send `lat: null, lng: null` instead; that stamps `'manual'` on its
   * own and is remembered as a decision.
   */
  location_source?: 'manual'
}

/**
 * Applies a partial metadata update to a photo via `PATCH /api/v1/photos/{uid}`
 * and returns the refreshed detail. Editor/admin only.
 *
 * @throws ApiError with `status` 400 (invalid field/coordinate), 403 (viewer),
 *   404 (no such photo) or 5xx.
 */
export async function updatePhoto(
  uid: string,
  patch: PhotoMetadataUpdate,
  signal?: AbortSignal,
): Promise<PhotoDetail> {
  const res = await fetch(`${API_BASE}/photos/${encodeURIComponent(uid)}`, {
    method: 'PATCH',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(patch),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as PhotoDetail
}

/**
 * The non-destructive edit stored for a photo (`photo_edits`): an optional
 * normalised crop rectangle (all four set together or all absent), a rotation of
 * 0/90/180/270 degrees, and brightness/contrast each neutral at 0 and meaningful
 * in [-1, 1]. Mirrors the backend `photos.Edit`.
 */
export interface PhotoEdit {
  photo_uid?: string
  crop_x?: number
  crop_y?: number
  crop_w?: number
  crop_h?: number
  rotation: number
  brightness: number
  contrast: number
  updated_at?: string
}

/**
 * Fetches the stored non-destructive edit for a photo via
 * `GET /api/v1/photos/{uid}/edit`. An unedited photo returns a neutral edit
 * (rotation 0, brightness/contrast 0, no crop), so the caller always has a value.
 *
 * @throws ApiError with `status` 404 (no such photo) or 5xx.
 */
export async function fetchEdit(uid: string, signal?: AbortSignal): Promise<PhotoEdit> {
  const res = await fetch(`${API_BASE}/photos/${encodeURIComponent(uid)}/edit`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as PhotoEdit
}

/**
 * Saves the non-destructive edit for a photo via `PUT /api/v1/photos/{uid}/edit`
 * and returns the persisted edit. The original file is never modified; downloads
 * honour the edit. Editor/admin only.
 *
 * Only the edit itself goes on the wire. `PhotoEdit` doubles as the GET response,
 * which also carries `photo_uid`/`updated_at` — and the PUT body is decoded
 * strictly, so echoing an edit straight back (as the edit panel does) would be
 * rejected as malformed. An absent crop field is simply omitted, which is how the
 * API is told there is no crop.
 *
 * @throws ApiError with `status` 400 (invalid edit), 403 (viewer), 404 (no such
 *   photo) or 5xx.
 */
export async function saveEdit(
  uid: string,
  edit: PhotoEdit,
  signal?: AbortSignal,
): Promise<PhotoEdit> {
  const body = {
    crop_x: edit.crop_x,
    crop_y: edit.crop_y,
    crop_w: edit.crop_w,
    crop_h: edit.crop_h,
    rotation: edit.rotation,
    brightness: edit.brightness,
    contrast: edit.contrast,
  }
  const res = await fetch(`${API_BASE}/photos/${encodeURIComponent(uid)}/edit`, {
    method: 'PUT',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as PhotoEdit
}

/**
 * Builds the URL of a photo's download (`GET /api/v1/photos/{uid}/download`). By
 * default the download honours any saved edit; pass `original: true` for the
 * untouched original bytes. A download token can be appended for cookie-less
 * contexts.
 *
 * For the untouched original prefer {@link Photo.download_url} off the payload,
 * which already points at the object store when that is where it lives. This
 * route stays the only way to fetch a *rendered edit*, which only the application
 * can produce.
 *
 * Pass `proxy: true` when the page itself has to *read* the bytes rather than hand
 * the address to the browser: the route then streams them instead of redirecting
 * to the object store, and the response stays same-origin and readable. That is
 * what sharing photos into the phone's library needs (see `services/share.ts`); an
 * `<a download>` or an `<img>` must not ask for it, because it moves every byte
 * through the application for nothing.
 */
export function downloadUrl(
  uid: string,
  options: { original?: boolean; token?: string | null; proxy?: boolean } = {},
): string {
  const query = new URLSearchParams()
  if (options.original === true) {
    query.set('original', 'true')
  }
  if (options.proxy === true) {
    query.set('proxy', 'true')
  }
  if (options.token !== undefined && options.token !== null && options.token !== '') {
    query.set('t', options.token)
  }
  const suffix = query.toString() === '' ? '' : `?${query.toString()}`
  return `${API_BASE}/photos/${encodeURIComponent(uid)}/download${suffix}`
}

/**
 * Names the photos to pack into a bulk ZIP download. Either an explicit set of
 * UIDs (a library selection), an album to download whole, or both — the backend
 * merges and de-duplicates them.
 */
export interface ZipDownloadRequest {
  /** Explicit photo UIDs to include (a library selection). */
  photoUids?: string[]
  /** An album UID to expand to its live photos server-side (whole-album download). */
  albumUid?: string
  /**
   * Base archive name without extension (e.g. an album title). When omitted a
   * dated default (`kukatko-photos-<date>.zip`) is used.
   */
  name?: string
}

/**
 * Downloads a set of photos' originals as a single ZIP via
 * `POST /api/v1/photos/download-zip` and hands the archive to the browser as a
 * file download. The server streams the archive; the response is read as a Blob
 * and saved through a temporary object URL, so the caller only has to reflect the
 * pending/error state.
 *
 * The archive name is computed client-side (the object-URL download uses the
 * anchor's `download` attribute, not the server's Content-Disposition): the given
 * `name` or a dated `kukatko-photos-<date>.zip`. The same date is sent to the
 * server (which avoids wall-clock) for its own Content-Disposition.
 *
 * @throws ApiError on a non-OK response, notably 413 when the request is over the
 *   per-download file cap, so the caller can show a specific message.
 */
export async function downloadPhotosZip(req: ZipDownloadRequest): Promise<void> {
  const date = todayStamp()
  const res = await fetch(`${API_BASE}/photos/download-zip`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify({
      photo_uids: req.photoUids,
      album_uid: req.albumUid,
      name: req.name,
      date,
    }),
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  const blob = await res.blob()
  const base =
    req.name !== undefined && req.name.trim() !== '' ? req.name.trim() : `kukatko-photos-${date}`
  saveBlob(blob, `${base}.zip`)
}

/** Returns today's local date as YYYY-MM-DD, for the ZIP archive's name. */
function todayStamp(): string {
  const now = new Date()
  const year = now.getFullYear()
  const month = String(now.getMonth() + 1).padStart(2, '0')
  const day = String(now.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

/**
 * Saves a Blob to the user's downloads as `filename` by clicking a temporary
 * anchor pointed at an object URL, revoking the URL afterwards so the blob can be
 * garbage-collected.
 */
function saveBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  URL.revokeObjectURL(url)
}

/**
 * Fetches one photo's full detail via `GET /api/v1/photos/{uid}`.
 *
 * @throws ApiError with `status` 404 (no such photo) or 5xx so the caller can
 *   render the matching message.
 */
export async function fetchPhoto(uid: string, signal?: AbortSignal): Promise<PhotoDetail> {
  const res = await fetch(`${API_BASE}/photos/${encodeURIComponent(uid)}`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as PhotoDetail
}

/**
 * One entry in the similar-photos response (`internal/photoapi/similar.go`): a
 * full photo record plus its cosine `distance` to the source photo (smaller is
 * closer / more similar).
 */
export interface SimilarPhoto extends Photo {
  distance: number
}

/** Response body of `GET /api/v1/photos/{uid}/similar`. */
export interface SimilarResponse {
  similar: SimilarPhoto[]
}

/**
 * Fetches the photos most visually similar to `uid` via
 * `GET /api/v1/photos/{uid}/similar`, ordered nearest-first and excluding the
 * source photo. The endpoint is empty-friendly: a photo that has not been
 * embedded yet (or a server with no search backend) yields an empty list with
 * 200, so an empty array is a normal result, not an error.
 *
 * @param limit optional cap on the number of neighbours (backend default 24,
 *   max 100); omitted values use the backend default.
 * @throws ApiError with `status` 404 (no such photo) or 5xx so the caller can
 *   render the matching message.
 */
export async function fetchSimilar(
  uid: string,
  limit?: number,
  signal?: AbortSignal,
): Promise<SimilarPhoto[]> {
  const query = new URLSearchParams()
  if (limit !== undefined) {
    query.set('limit', String(limit))
  }
  const suffix = query.toString() === '' ? '' : `?${query.toString()}`
  const res = await fetch(`${API_BASE}/photos/${encodeURIComponent(uid)}/similar${suffix}`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  const body = (await res.json()) as SimilarResponse
  return body.similar
}

/**
 * Builds the URL of a photo's cached thumbnail at the given size (for example
 * `fit_1920`). The media endpoint accepts the session cookie sent by the browser
 * for same-origin `<img>` requests; an optional download token is appended for
 * cookie-less contexts.
 *
 * Use this only for a size no photo payload carries — a lightbox preview, an
 * editor canvas, a cover addressed by UID alone. For a grid tile, read
 * {@link Photo.thumb_url} off the payload instead: it already points wherever the
 * media actually lives, whereas this route may cost the browser a redirect there.
 *
 * `proxy: true` forces the route to stream the JPEG instead of redirecting to the
 * object store, for the one caller that must read the bytes in the page rather than
 * paint them: sharing a RAW photo into the phone's library as its preview (see
 * `services/share.ts`). An `<img>` never wants it — a redirect is cheaper for
 * everyone.
 */
export function thumbUrl(
  uid: string,
  size: string,
  downloadToken?: string | null,
  options: { proxy?: boolean } = {},
): string {
  const url = `${API_BASE}/photos/${encodeURIComponent(uid)}/thumb/${encodeURIComponent(size)}`
  const params: string[] = []
  if (downloadToken !== undefined && downloadToken !== null && downloadToken !== '') {
    params.push(`t=${encodeURIComponent(downloadToken)}`)
  }
  if (options.proxy === true) {
    params.push('proxy=true')
  }
  return params.length === 0 ? url : `${url}?${params.join('&')}`
}

/**
 * Builds the URL of a photo's inline video stream
 * (`GET /api/v1/photos/{uid}/video`). The endpoint supports HTTP range requests
 * (seeking) and serves a live photo's motion clip. The browser sends the session
 * cookie for same-origin `<video>` requests; an optional download token is
 * appended for cookie-less contexts.
 *
 * When the clip lives in the object store the endpoint redirects to the media
 * Worker, which serves the range requests itself. The `<video>` element follows
 * the redirect on every request, so it always seeks against a fresh signature and
 * playback of a long clip never outlives one.
 */
export function videoUrl(uid: string, downloadToken?: string | null): string {
  const url = `${API_BASE}/photos/${encodeURIComponent(uid)}/video`
  if (downloadToken !== undefined && downloadToken !== null && downloadToken !== '') {
    return `${url}?t=${encodeURIComponent(downloadToken)}`
  }
  return url
}

/**
 * A video's scrub-preview storyboard as `GET /api/v1/photos/{uid}/storyboard`
 * reports it. Mirrors the backend `storyboardResponse`.
 *
 * `status` is the only field always present:
 * - `ready` — the sprite exists and the grid fields describe it;
 * - `pending` — generation was just scheduled; ask again shortly;
 * - `unavailable` — this photo will never have one (a still, a live photo, a
 *   clip of unknown length, or a host with no ffmpeg). Stop asking.
 *
 * The grid fields are omitted while pending or unavailable, so a client that
 * reads `status` first can never place a preview against a zero grid.
 */
export interface Storyboard {
  status: 'ready' | 'pending' | 'unavailable'
  columns?: number
  rows?: number
  count?: number
  tile_width?: number
  tile_height?: number
  interval_ms?: number
}

/**
 * Asks whether a video's scrub-preview sprite is ready, scheduling its
 * generation in the background when it is not
 * (`GET /api/v1/photos/{uid}/storyboard`).
 *
 * The GET is what schedules the work, deliberately: previews are for whoever is
 * watching, viewers included, and the queue dedups per photo so asking on every
 * playback costs nothing. Call it when playback starts — never for a list of
 * photos, which would enqueue a full decode per video in the library.
 *
 * @throws ApiError with `status` 404 (no such photo) or 5xx.
 */
export async function fetchStoryboard(uid: string, signal?: AbortSignal): Promise<Storyboard> {
  const res = await fetch(`${API_BASE}/photos/${encodeURIComponent(uid)}/storyboard`, {
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as Storyboard
}

/**
 * Builds the URL of a video's storyboard sprite — the JPEG grid of frames the
 * player samples for its scrub preview. The browser sends the session cookie for
 * same-origin requests; an optional download token is appended for cookie-less
 * contexts, like the CSS background the preview is painted with.
 *
 * The sprite is served by the application (it is cache-only derived media, never
 * published to the object store), so unlike a thumbnail this address is always
 * this route.
 */
export function storyboardSpriteUrl(uid: string, downloadToken?: string | null): string {
  const url = `${API_BASE}/photos/${encodeURIComponent(uid)}/storyboard/sprite`
  if (downloadToken !== undefined && downloadToken !== null && downloadToken !== '') {
    return `${url}?t=${encodeURIComponent(downloadToken)}`
  }
  return url
}

/**
 * Toggles whether the current user has favorited a photo via
 * `PUT /api/v1/photos/{uid}/favorite` (favorite) or `DELETE …` (unfavorite).
 * Both are idempotent and resolve with no body (204). Favoriting is a personal
 * action available to every signed-in user, including viewers.
 *
 * @throws ApiError with `status` 404 (no such photo), 503 (favorites backend
 *   unwired) or 5xx, so the caller can roll back an optimistic update.
 */
export async function favoritePhoto(
  uid: string,
  favorite: boolean,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch(`${API_BASE}/photos/${encodeURIComponent(uid)}/favorite`, {
    method: favorite ? 'PUT' : 'DELETE',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
}

/** The result of a successful thumbnail regeneration. Mirrors the backend
 * `regenerateThumbnailResponse`. */
export interface RegenerateThumbnailResult {
  /** Fixed status marker (`"regenerated"`). */
  status: string
  /** The thumbnail size names that were rebuilt. */
  sizes: string[]
}

/**
 * Rebuilds a photo's cached thumbnails and perceptual hashes from its original
 * via `POST /api/v1/photos/{uid}/regenerate-thumbnail`. It is a maintenance
 * action for a missing or stale thumbnail: editors/admins only, idempotent, and
 * it never touches the original file. It runs synchronously and resolves once the
 * derived data has been rebuilt, so the caller can then cache-bust the displayed
 * image.
 *
 * @throws ApiError with `status` 403 (viewer), 404 (no such photo), 422 (the
 *   original is missing or cannot be decoded), 503 (regeneration unwired) or 5xx.
 */
export async function regenerateThumbnail(
  uid: string,
  signal?: AbortSignal,
): Promise<RegenerateThumbnailResult> {
  const res = await fetch(`${API_BASE}/photos/${encodeURIComponent(uid)}/regenerate-thumbnail`, {
    method: 'POST',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as RegenerateThumbnailResult
}

/**
 * Schedules one per-photo computation via
 * `POST /api/v1/photos/{uid}/process/{step}` and resolves with that step's new
 * state, so the caller can update the row without re-fetching the photo.
 *
 * It is the repair for a photo the pipeline missed: maintainers only, and
 * idempotent — the queue's dedup index absorbs a request for work already
 * waiting or running, so a double click schedules it once.
 *
 * @throws ApiError with `status` 400 (not a known step), 403 (not a
 *   maintainer), 404 (no such photo), 409 (the step cannot apply to this photo),
 *   503 (processing unwired) or 5xx.
 */
export async function runProcessingStep(
  uid: string,
  step: ProcessingStep,
  signal?: AbortSignal,
): Promise<PhotoProcessing> {
  const res = await fetch(
    `${API_BASE}/photos/${encodeURIComponent(uid)}/process/${encodeURIComponent(step)}`,
    { method: 'POST', credentials: 'same-origin', signal },
  )
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as PhotoProcessing
}

/**
 * A partial rating update for `PUT /api/v1/photos/{uid}/rating`: a star rating
 * (0–5) and/or a pick/reject flag. At least one must be present; an omitted key
 * leaves that field unchanged. Mirrors the backend `ratingBody`.
 */
export interface RatingUpdate {
  rating?: number
  flag?: RatingFlag
}

/**
 * Sets the current user's star rating and/or pick/reject flag on a photo via
 * `PUT /api/v1/photos/{uid}/rating`. Idempotent, resolves with no body (204).
 * Rating is a personal action available to every signed-in user. Pass just the
 * field you are changing (e.g. `{ rating: 4 }` or `{ flag: 'reject' }`).
 *
 * @throws ApiError with `status` 400 (invalid rating/flag, or empty update),
 *   404 (no such photo), 503 (ratings backend unwired) or 5xx, so the caller can
 *   roll back an optimistic update.
 */
export async function ratePhoto(
  uid: string,
  update: RatingUpdate,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch(`${API_BASE}/photos/${encodeURIComponent(uid)}/rating`, {
    method: 'PUT',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(update),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
}

/**
 * Clears the current user's rating and flag on a photo via
 * `DELETE /api/v1/photos/{uid}/rating` (resets it to rating 0 / flag `none`).
 * Idempotent, resolves with no body (204).
 *
 * @throws ApiError with `status` 404 (no such photo), 503 (ratings backend
 *   unwired) or 5xx, so the caller can roll back an optimistic update.
 */
export async function clearRating(uid: string, signal?: AbortSignal): Promise<void> {
  const res = await fetch(`${API_BASE}/photos/${encodeURIComponent(uid)}/rating`, {
    method: 'DELETE',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
}

/**
 * Archives a photo via `POST /api/v1/photos/{uid}/archive`, setting its
 * `archived_at` so it is soft-deleted: it leaves the library and moves to the
 * trash, from where it can be restored (see {@link unarchivePhoto}) or later
 * purged. Editor/admin only.
 *
 * @throws ApiError with `status` 404 (no such photo), 403 (not an editor) or 5xx.
 */
export async function archivePhoto(uid: string, signal?: AbortSignal): Promise<void> {
  const res = await fetch(`${API_BASE}/photos/${encodeURIComponent(uid)}/archive`, {
    method: 'POST',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
}

/**
 * Restores an archived photo via `POST /api/v1/photos/{uid}/unarchive`, clearing
 * its `archived_at` so it leaves the trash and reappears in the library. Editor/
 * admin only.
 *
 * @throws ApiError with `status` 404 (no such photo), 403 (not an editor) or 5xx.
 */
export async function unarchivePhoto(uid: string, signal?: AbortSignal): Promise<void> {
  const res = await fetch(`${API_BASE}/photos/${encodeURIComponent(uid)}/unarchive`, {
    method: 'POST',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
}

/**
 * Hides a photo from the library via `POST /api/v1/photos/{uid}/hide`, setting
 * its `hidden_from_library`. It is not archiving: the photo stays in the
 * catalogue, in its albums and labels, in favourites and at its own URL — it
 * only leaves the library grid, the timeline, the map, the slideshow, the review
 * game and the default search. List the hidden ones again with `hidden:yes`, and
 * bring one back with {@link unhidePhoto}. Editor/admin only.
 *
 * @throws ApiError with `status` 404 (no such photo), 403 (not an editor) or 5xx.
 */
export async function hidePhoto(uid: string, signal?: AbortSignal): Promise<void> {
  const res = await fetch(`${API_BASE}/photos/${encodeURIComponent(uid)}/hide`, {
    method: 'POST',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
}

/**
 * Brings a hidden photo back into the library via
 * `POST /api/v1/photos/{uid}/unhide`, clearing its `hidden_from_library`.
 * Editor/admin only.
 *
 * @throws ApiError with `status` 404 (no such photo), 403 (not an editor) or 5xx.
 */
export async function unhidePhoto(uid: string, signal?: AbortSignal): Promise<void> {
  const res = await fetch(`${API_BASE}/photos/${encodeURIComponent(uid)}/unhide`, {
    method: 'POST',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
}

/**
 * Groups the given photos into one new stack via `POST /api/v1/photos/stack`
 * (manual stacking, for the cases automatic detection misses) and returns the
 * new stack's primary detail. Editor/admin only.
 *
 * @throws ApiError with `status` 400 (fewer than two photos), 404 (one is
 *   missing), 403 (not an editor), 503 (stacking disabled) or 5xx.
 */
export async function stackPhotos(photoUids: string[], signal?: AbortSignal): Promise<PhotoDetail> {
  const res = await fetch(`${API_BASE}/photos/stack`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ photo_uids: photoUids }),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as PhotoDetail
}

/**
 * Makes the photo the primary of its stack via
 * `POST /api/v1/photos/{uid}/stack/primary` and returns the refreshed detail.
 * Editor/admin only.
 *
 * @throws ApiError with `status` 404 (no such photo), 409 (not stacked), 403
 *   (not an editor), 503 (stacking disabled) or 5xx.
 */
export async function setStackPrimary(uid: string, signal?: AbortSignal): Promise<PhotoDetail> {
  return postStackMutation(`${API_BASE}/photos/${encodeURIComponent(uid)}/stack/primary`, signal)
}

/**
 * Removes the photo from its stack via `POST /api/v1/photos/{uid}/unstack`,
 * turning it back into a standalone photo, and returns its refreshed detail.
 * Editor/admin only.
 */
export async function unstackMember(uid: string, signal?: AbortSignal): Promise<PhotoDetail> {
  return postStackMutation(`${API_BASE}/photos/${encodeURIComponent(uid)}/unstack`, signal)
}

/**
 * Dissolves the whole stack the photo belongs to via
 * `POST /api/v1/photos/{uid}/unstack-all` and returns its refreshed detail.
 * Editor/admin only.
 */
export async function unstackAll(uid: string, signal?: AbortSignal): Promise<PhotoDetail> {
  return postStackMutation(`${API_BASE}/photos/${encodeURIComponent(uid)}/unstack-all`, signal)
}

/**
 * postStackMutation POSTs to a body-less stack endpoint and returns the refreshed
 * photo detail, shared by the set-primary and unstack actions.
 */
async function postStackMutation(url: string, signal?: AbortSignal): Promise<PhotoDetail> {
  const res = await fetch(url, { method: 'POST', credentials: 'same-origin', signal })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as PhotoDetail
}

/**
 * Permanently deletes a single archived photo via
 * `POST /api/v1/photos/{uid}/purge?confirm=true`. This is irreversible: the row,
 * its originals and thumbnails are removed. The explicit `confirm=true` guard
 * mirrors the server's requirement. Editor/admin only.
 *
 * @throws ApiError with `status` 404 (no such photo), 409 (not archived), 503
 *   (purge backend unwired) or 5xx.
 */
export async function purgePhoto(uid: string, signal?: AbortSignal): Promise<void> {
  const res = await fetch(`${API_BASE}/photos/${encodeURIComponent(uid)}/purge?confirm=true`, {
    method: 'POST',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
}

/** Counts returned by the empty-trash action. */
export interface PurgeResult {
  purged: number
  failed: number
}

/**
 * Permanently deletes every archived photo via
 * `POST /api/v1/trash/empty?confirm=true` and returns how many were purged and
 * failed. Irreversible; editor/admin only.
 *
 * @throws ApiError with `status` 503 (purge backend unwired) or 5xx.
 */
export async function emptyTrash(signal?: AbortSignal): Promise<PurgeResult> {
  const res = await fetch(`${API_BASE}/trash/empty?confirm=true`, {
    method: 'POST',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as PurgeResult
}

/**
 * Permanently deletes every archived photo older than `days` days via
 * `POST /api/v1/trash/purge-older?days=<days>&confirm=true` and returns how many
 * were purged and failed. `days` must be an integer ≥ 0; `days = 0` purges the
 * whole trash. Irreversible; admin only (backend guard `RequireAdmin`), and the
 * explicit `confirm=true` mirrors the server's requirement.
 *
 * @throws ApiError with `status` 400 (missing/invalid `days`), 503 (purge backend
 *   unwired) or 5xx.
 */
export async function purgeTrashOlderThan(
  days: number,
  signal?: AbortSignal,
): Promise<PurgeResult> {
  const query = new URLSearchParams({ days: String(days), confirm: 'true' })
  const res = await fetch(`${API_BASE}/trash/purge-older?${query.toString()}`, {
    method: 'POST',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as PurgeResult
}

/** The trash retention window, used to show each item's auto-purge countdown. */
export interface TrashInfo {
  retention_days: number
}

/**
 * Fetches the trash retention window via `GET /api/v1/trash/info` so the trash
 * UI can compute how long each archived photo has before it is auto-purged.
 *
 * @throws ApiError on a non-2xx response.
 */
export async function fetchTrashInfo(signal?: AbortSignal): Promise<TrashInfo> {
  const res = await fetch(`${API_BASE}/trash/info`, { credentials: 'same-origin', signal })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as TrashInfo
}

/**
 * One month-granularity bucket of the library timeline (`photos.TimelineBucket`,
 * `GET /api/v1/photos/timeline`): the number of photos captured in that calendar
 * month (`count`) and the number of photos that sort before this bucket in the
 * default newest-first grid order (`cumulative`). Because buckets are ordered
 * newest-first and never overlap, `cumulative` is the scroll index of the
 * bucket's first photo — which is exactly what the scrubber jumps to.
 */
export interface TimelineBucket {
  year: number
  month: number
  count: number
  cumulative: number
}

/**
 * The month date-histogram of the library (`photos.Timeline`,
 * `GET /api/v1/photos/timeline`): the buckets in newest-first order plus the
 * overall `total`. `total` counts every matching photo — including those with an
 * unknown capture time that belong to no bucket — so it may exceed the sum of the
 * bucket counts.
 */
export interface Timeline {
  buckets: TimelineBucket[]
  total: number
}

/**
 * Fetches the month date-histogram of the library via
 * `GET /api/v1/photos/timeline`. It accepts the same filter params as
 * {@link fetchPhotos} (sort/order and pagination are ignored server-side — the
 * histogram is always grouped by capture date, newest-first) so the buckets line
 * up with the default-sorted grid and a scrubber can map a month to a scroll
 * index via each bucket's `cumulative`.
 *
 * @throws ApiError with `status` 400 (invalid filter) or 5xx so the caller can
 *   render the matching message.
 */
export async function fetchTimeline(
  params: PhotoListParams,
  signal?: AbortSignal,
): Promise<Timeline> {
  const query = buildPhotoQuery(params)
  const res = await fetch(`${API_BASE}/photos/timeline?${query.toString()}`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as Timeline
}

/**
 * One calendar year that holds photos, with the number of photos captured in it
 * (`photos.YearBucket`). Backs the library's year facet, where each year is
 * offered with its count.
 */
export interface YearBucket {
  year: number
  count: number
}

/**
 * The year histogram of the catalog (`photos.Years`,
 * `GET /api/v1/photos/years`): the years that actually hold photos, newest first,
 * plus the overall `total`. `total` counts every matching photo — including those
 * with an unknown capture time that belong to no year — so it may exceed the sum
 * of the bucket counts.
 */
export interface YearsResponse {
  years: YearBucket[]
  total: number
}

/**
 * Fetches the years that hold photos, newest first, each with its count, via
 * `GET /api/v1/photos/years` — the option list behind the library's period filter,
 * which groups them into decades.
 *
 * It accepts the same filter params as {@link fetchPhotos}, and the counts respect
 * them (including the caller's archived visibility), so a year's count is
 * what the grid will show once that year is picked. The caller drops the period
 * bounds first — a facet must not narrow its own options; see `useLibraryFacets`
 * — and sort/order and pagination are ignored here as well.
 *
 * @throws ApiError with `status` 400 (invalid filter) or 5xx so the caller can
 *   render the matching message.
 */
export async function fetchPhotoYears(
  params: PhotoListParams,
  signal?: AbortSignal,
): Promise<YearsResponse> {
  const query = buildPhotoQuery(params)
  const res = await fetch(`${API_BASE}/photos/years?${query.toString()}`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as YearsResponse
}

/**
 * One uploader of the photos in the current view, with how many of them are
 * theirs (`photos.UploaderBucket`). Backs the library's uploader facet.
 *
 * The photos nobody uploaded — the ones an import brought in — are a bucket of
 * their own, with an empty `uid` **and** an empty `name`: naming that group is
 * the client's job, because only the client knows the reader's language.
 */
export interface UploaderBucket {
  /** The uploader's user UID, `''` for the imported photos. */
  uid: string
  /** The uploader's display name (or username), `''` for the imported photos. */
  name: string
  /** How many photos of the current view are theirs. */
  count: number
}

/** The uploader facet of the current view (`GET /api/v1/photos/uploaders`). */
export interface UploadersResponse {
  uploaders: UploaderBucket[]
}

/**
 * Fetches who uploaded the photos of the current view, largest contribution
 * first, each with its count, via `GET /api/v1/photos/uploaders` — the option
 * list behind the library's uploader filter.
 *
 * It accepts the same filter params as {@link fetchPhotos}, and the counts
 * respect them, so in an event album it lists that event's contributors rather
 * than every account on the instance. The caller drops the `uploader` scope
 * first — a facet must not narrow its own options; see `useUploaders` — and
 * sort/order and pagination are ignored here as well.
 *
 * @throws ApiError with `status` 400 (invalid filter) or 5xx so the caller can
 *   render the matching message.
 */
export async function fetchPhotoUploaders(
  params: PhotoListParams,
  signal?: AbortSignal,
): Promise<UploadersResponse> {
  const query = buildPhotoQuery(params)
  const res = await fetch(`${API_BASE}/photos/uploaders?${query.toString()}`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as UploadersResponse
}

/** Thumbnail size used for square grid tiles (medallions, cards) — a crop. */
export const GRID_THUMB_SIZE = 'tile_500'

/**
 * The rendition the justified photo wall draws: aspect-preserving, not a square
 * crop, and the size the backend stamps into every photo's `preview_url`. A tile
 * wide enough to outrun it asks for a larger rung by route — see
 * `lib/tileRendition`.
 */
export const GRID_PREVIEW_SIZE = 'fit_720'

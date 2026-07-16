import { ApiError } from './auth'
import { type ArchivedFilter } from './photos'

/**
 * GeoJSON `Point` geometry as returned by the map feed
 * (`internal/mapsapi/geojson.go`). Per RFC 7946 the coordinate order is
 * `[longitude, latitude]`.
 */
export interface MapPointGeometry {
  type: 'Point'
  coordinates: [number, number]
}

/**
 * The marker properties carried by each map feature: enough to render a popup
 * (a thumbnail linking to the photo detail) without a second request. `thumb` is
 * a ready-to-use relative thumbnail path under the media API.
 */
export interface MapFeatureProperties {
  uid: string
  title: string
  taken_at?: string
  media_type: string
  thumb: string
  /**
   * Whether the pin's position was inferred from photos taken nearby in time
   * rather than measured. Estimated photos are on the map by default — putting
   * them there is the point of estimating them — but a pin that looks identical to
   * a measured one is the map quietly lying, so it is drawn differently.
   *
   * Absent means "not an estimate": the backend only emits the key when true.
   */
  location_estimated?: boolean
}

/** A single GeoJSON `Feature`: a point plus its marker properties. */
export interface MapFeature {
  type: 'Feature'
  geometry: MapPointGeometry
  properties: MapFeatureProperties
}

/**
 * The GeoJSON `FeatureCollection` returned by `GET /api/v1/map/photos`: every
 * geotagged photo matching the active filters, capped server-side.
 */
export interface MapFeatureCollection {
  type: 'FeatureCollection'
  features: MapFeature[]
}

/**
 * Filters accepted by the map photo feed. Mirrors the subset of the photo list
 * filters the GeoJSON endpoint honours (`parseGeoParams`); the backend forces
 * has-GPS on, so only geotagged photos ever come back. Empty / `undefined`
 * values are omitted from the query.
 */
export interface MapPhotoParams {
  /** RFC3339 timestamp or YYYY-MM-DD date. */
  taken_after?: string
  /** RFC3339 timestamp or YYYY-MM-DD date. */
  taken_before?: string
  archived?: ArchivedFilter
  /** Scope the feed to photos in this album (`album` query param). */
  album?: string
  /** Scope the feed to photos carrying this label (`label` query param). */
  label?: string
}

const API_BASE = '/api/v1'

/** Standard backend error envelope (`internal/mapsapi`). */
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
 * Serialises the map filters into a query string, omitting empty / undefined
 * values so absent filters never reach the backend.
 */
export function buildMapQuery(params: MapPhotoParams): URLSearchParams {
  const query = new URLSearchParams()
  const set = (key: string, value: string | undefined): void => {
    if (value !== undefined && value !== '') {
      query.set(key, value)
    }
  }
  set('taken_after', params.taken_after)
  set('taken_before', params.taken_before)
  set('archived', params.archived)
  set('album', params.album)
  set('label', params.label)
  return query
}

/**
 * Fetches geotagged photos as a GeoJSON FeatureCollection from
 * `GET /api/v1/map/photos`, honouring the active filters. The session cookie is
 * sent automatically (same-origin).
 *
 * @throws ApiError with `status` 400 (invalid filter) or 5xx so the caller can
 *   render the matching message.
 */
export async function fetchMapPhotos(
  params: MapPhotoParams,
  signal?: AbortSignal,
): Promise<MapFeatureCollection> {
  const query = buildMapQuery(params)
  const suffix = query.toString() === '' ? '' : `?${query.toString()}`
  const res = await fetch(`${API_BASE}/map/photos${suffix}`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as MapFeatureCollection
}

/** Tile mapsets the UI offers; a subset of the backend allow-list. */
export type Mapset = 'basic' | 'outdoor' | 'aerial'

/** The mapsets the switcher exposes, in display order. */
export const MAPSETS: readonly Mapset[] = ['basic', 'outdoor', 'aerial']

/** Narrows a raw string to a known mapset, defaulting to "basic". */
export function toMapset(raw: string): Mapset {
  return (MAPSETS as readonly string[]).includes(raw) ? (raw as Mapset) : 'basic'
}

/**
 * Builds the Leaflet tile-layer URL template for a mapset, pointing at the
 * **backend proxy** so the mapy.com API key never reaches the client. The
 * `{z}/{x}/{y}` placeholders are filled by Leaflet; the trailing `{r}`
 * placeholder becomes `@2x` on retina displays (where the backend serves the
 * hi-DPI variant for the mapsets that support it).
 */
export function tileLayerUrl(mapset: Mapset): string {
  return `${API_BASE}/map/tiles/${mapset}/{z}/{x}/{y}{r}`
}

/**
 * Why the tile layer is failing, as classified from the tile proxy's status.
 * `key_rejected` is the one that needs a human: mapy.com is refusing the server's
 * API key (expired, revoked or over quota), so no amount of retrying will help.
 */
export type TileFailure = 'key_rejected' | 'rate_limited' | 'unavailable' | 'error'

/**
 * The status the tile proxy answers with when mapy.com rejects *the server's* API
 * key (`mapsapi.StatusMapKeyRejected`, 424 Failed Dependency). The upstream 401/403
 * is deliberately not passed through: the caller's own request is fine, it is our
 * dependency that failed.
 */
const STATUS_MAP_KEY_REJECTED = 424

/**
 * Asks the tile proxy why a tile failed, by re-requesting the tile Leaflet could
 * not load (an `<img>` never exposes its response status to JavaScript, so the
 * status has to be fetched). Returns the classified failure, or `null` when the
 * tile is in fact fine — a transient hiccup, or a tile mapy.com simply does not
 * have (404), which is a normal answer outside the covered area and no reason to
 * warn anyone.
 *
 * A network failure is reported as `'error'` rather than thrown: the caller wants
 * to explain the empty map, not handle an exception. An aborted probe still
 * throws, so a caller that has gone away does not act on it.
 */
export async function probeTileFailure(
  tileUrl: string,
  signal?: AbortSignal,
): Promise<TileFailure | null> {
  let res: Response
  try {
    res = await fetch(tileUrl, { method: 'GET', credentials: 'same-origin', signal })
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') {
      throw err
    }
    return 'error'
  }
  if (res.ok || res.status === 404) {
    return null
  }
  switch (res.status) {
    case STATUS_MAP_KEY_REJECTED:
      return 'key_rejected'
    case 429:
      return 'rate_limited'
    case 503:
      return 'unavailable'
    default:
      return 'error'
  }
}

/** One administrative level of a reverse-geocoded place (region, town, …). */
export interface RegionalItem {
  name: string
  type: string
}

/**
 * A reverse-geocoded location (`internal/mapy` GeocodeResult): a primary name,
 * a human-readable location string and the administrative structure around it.
 */
export interface GeocodeResult {
  name: string
  location: string
  regional_structure: RegionalItem[]
}

/**
 * Reverse-geocodes a coordinate via the backend proxy
 * `GET /api/v1/map/rgeocode?lat=&lng=` (the mapy.com key stays server-side). The
 * backend caches and rate-limits the upstream call to conserve credits.
 *
 * @throws ApiError with `status` 404 (no place found), 429 (rate limited), 503
 *   (geocoding not configured) or 5xx, so the caller can show the right message.
 */
export async function reverseGeocode(
  lat: number,
  lng: number,
  signal?: AbortSignal,
): Promise<GeocodeResult> {
  const query = new URLSearchParams({ lat: String(lat), lng: String(lng) })
  const res = await fetch(`${API_BASE}/map/rgeocode?${query.toString()}`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as GeocodeResult
}

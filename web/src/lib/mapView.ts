import { type MapPhotoParams, type Mapset, toMapset } from '../services/map'
import { type ArchivedFilter } from '../services/photos'

/**
 * URL-encoded view state for the map page: the active mapset, the map viewport
 * (centre + zoom) and the photo filters the GeoJSON feed honours. All values are
 * strings (the urlState convention) so the whole view round-trips through the
 * query string and Back/Forward restores it exactly. An empty string means
 * "default" — for the viewport that means "fit to the markers".
 */
// A type alias (not an interface) so it satisfies the urlState `Record<string,
// string>` constraint — interfaces lack the implicit index signature TS requires.
// eslint-disable-next-line @typescript-eslint/consistent-type-definitions -- see above
export type MapView = {
  mapset: string
  /** Viewport latitude; empty until the user pans or a link sets it. */
  lat: string
  /** Viewport longitude; empty until the user pans or a link sets it. */
  lng: string
  /** Viewport zoom; empty until the user zooms or a link sets it. */
  z: string
  taken_after: string
  taken_before: string
  archived: string
  private: string
  album: string
  label: string
}

/**
 * Default view: the basic mapset, no explicit viewport (so the map fits its
 * markers) and no filters. Declared at module scope so the urlState setter keeps
 * a stable identity and values equal to a default are omitted from the URL.
 */
export const MAP_DEFAULTS: MapView = {
  mapset: 'basic',
  lat: '',
  lng: '',
  z: '',
  taken_after: '',
  taken_before: '',
  archived: 'false',
  private: '',
  album: '',
  label: '',
}

/** Accepted archive selectors; an unknown value falls back to hiding archived. */
const ARCHIVED: readonly ArchivedFilter[] = ['false', 'true', 'only']

/** Narrows a raw string to a known archive selector, defaulting to "false". */
function toArchived(raw: string): ArchivedFilter {
  return (ARCHIVED as readonly string[]).includes(raw) ? (raw as ArchivedFilter) : 'false'
}

/**
 * Maps the URL view state to the map photo feed params, sanitising the
 * enum-like archived field so a tampered URL cannot send an out-of-range value.
 * The viewport (lat/lng/zoom) and mapset are map-only and not part of the feed
 * query.
 */
export function mapViewToParams(view: MapView): MapPhotoParams {
  return {
    taken_after: view.taken_after,
    taken_before: view.taken_before,
    archived: toArchived(view.archived),
    private: view.private,
    album: view.album,
    label: view.label,
  }
}

/** The map viewport: centre coordinates and zoom level. */
export interface MapViewport {
  lat: number
  lng: number
  zoom: number
}

/**
 * Parses the viewport from the view state, returning `null` when any component
 * is absent or unparseable so the map can fall back to fitting its markers.
 */
export function viewportFromView(view: MapView): MapViewport | null {
  if (view.lat === '' || view.lng === '' || view.z === '') {
    return null
  }
  const lat = Number(view.lat)
  const lng = Number(view.lng)
  const zoom = Number(view.z)
  if (!Number.isFinite(lat) || !Number.isFinite(lng) || !Number.isFinite(zoom)) {
    return null
  }
  return { lat, lng, zoom }
}

/** Narrows the view's mapset to a known value, defaulting to "basic". */
export function mapsetFromView(view: MapView): Mapset {
  return toMapset(view.mapset)
}

/** Reports whether any photo filter differs from its default. */
export function hasActiveMapFilters(view: MapView): boolean {
  return (
    view.taken_after !== '' ||
    view.taken_before !== '' ||
    view.archived !== MAP_DEFAULTS.archived ||
    view.private !== '' ||
    view.album !== '' ||
    view.label !== ''
  )
}

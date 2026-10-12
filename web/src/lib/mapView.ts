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
  album: string
  label: string
  /**
   * The area panel's position: `''` (its resting state — open beside the map on
   * a desktop, peeking on a phone), {@link MAP_PANEL_CLOSED} (collapsed off a
   * desktop map; a phone sheet still peeks) or one of the phone sheet's taller
   * stops, {@link MAP_PANEL_HALF} / {@link MAP_PANEL_FULL}. Kept in the URL so
   * Back from a photo lands on the panel as it was left.
   */
  panel: string
  /**
   * A cluster opened as a grid (`south,west,north,east`, see `lib/mapArea`):
   * set when a tapped cluster cannot split any further — its photos share one
   * spot — so the panel lists just those photos instead of the whole viewport.
   * Empty for the plain "photos in this area" list.
   */
  sel: string
  /** A photo to highlight on the map — where "show on map" from a photo lands. */
  photo: string
  /**
   * A box to fit on load (`south,west,north,east`), used only while there is no
   * explicit centre/zoom: the viewer's Back link carries the area a photo was
   * opened from this way, since it knows the box but not the zoom.
   */
  area: string
}

/** {@link MapView.panel}: the desktop panel collapsed (a phone sheet peeks). */
export const MAP_PANEL_CLOSED = 'closed'
/** {@link MapView.panel}: the phone sheet at half the map's height. */
export const MAP_PANEL_HALF = 'half'
/** {@link MapView.panel}: the phone sheet over (nearly) the whole map. */
export const MAP_PANEL_FULL = 'full'

/** The phone bottom sheet's three stops. */
export type SheetState = 'peek' | 'half' | 'full'

/** The sheet stop a view asks for; anything unknown peeks. */
export function sheetStateFromView(view: Pick<MapView, 'panel'>): SheetState {
  if (view.panel === MAP_PANEL_HALF) {
    return 'half'
  }
  if (view.panel === MAP_PANEL_FULL) {
    return 'full'
  }
  return 'peek'
}

/** The {@link MapView.panel} value that records a sheet stop. */
export function panelValueForSheet(state: SheetState): string {
  switch (state) {
    case 'half':
      return MAP_PANEL_HALF
    case 'full':
      return MAP_PANEL_FULL
    default:
      return ''
  }
}

/** Whether the desktop side panel is open: everything but an explicit close. */
export function panelOpenFromView(view: Pick<MapView, 'panel'>): boolean {
  return view.panel !== MAP_PANEL_CLOSED
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
  album: '',
  label: '',
  panel: '',
  sel: '',
  photo: '',
  area: '',
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

/**
 * How many photo filters differ from their default. This is the number the
 * phone's Filters button wears as a badge: with every control folded into a
 * drawer, it is the only thing that says the map is showing a subset — so it
 * counts the filters the URL can carry, `album` and `label` included, not just
 * the three the bar has controls for.
 *
 * The map viewport (centre/zoom) and the mapset are deliberately not counted:
 * neither narrows the set of photos.
 */
export function activeMapFilterCount(view: MapView): number {
  const active = [
    view.taken_after !== '',
    view.taken_before !== '',
    view.archived !== MAP_DEFAULTS.archived,
    view.album !== '',
    view.label !== '',
  ]
  return active.filter((set) => set).length
}

/** Reports whether any photo filter differs from its default. */
export function hasActiveMapFilters(view: MapView): boolean {
  return activeMapFilterCount(view) > 0
}

/** How close "show on map" zooms in: a street, with the photo's neighbours around it. */
export const PHOTO_MAP_ZOOM = 17

/**
 * The map link of one photo — where "show on map" on its detail page leads: the
 * map centred on the photo at street level with its tile highlighted. An
 * archived photo brings the archive into the map with it; the map hides the
 * archive by default, and a link that lands on a map without the photo it was
 * about would be a broken promise.
 */
export function photoMapHref(photo: {
  uid: string
  lat: number
  lng: number
  archived?: boolean
}): string {
  const view: MapView = {
    ...MAP_DEFAULTS,
    lat: photo.lat.toFixed(5),
    lng: photo.lng.toFixed(5),
    z: String(PHOTO_MAP_ZOOM),
    photo: photo.uid,
    archived: photo.archived === true ? 'true' : MAP_DEFAULTS.archived,
  }
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(view)) {
    if (value !== MAP_DEFAULTS[key as keyof MapView]) {
      params.set(key, value)
    }
  }
  return `/map?${params.toString()}`
}

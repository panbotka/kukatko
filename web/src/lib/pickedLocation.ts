import { type Coordinates } from './coordinates'

/**
 * sessionStorage key holding the coordinate most recently picked in the location
 * picker. Session storage rather than local: it exists so the *next* photo in a
 * sitting of geotagging opens the map where the last one was set, and a week-old
 * pin is not where anybody wants to start.
 */
const STORAGE_KEY = 'kukatko.lastPickedLocation'

/**
 * Zoom the map opens at when it is centred on a coordinate the user picked
 * before. Deliberately looser than the zoom for a photo that *has* a location:
 * this is a neighbourhood to start looking in, not a claim about this photo, and
 * opening tight on the wrong village is worse than opening on the right district.
 */
export const NEARBY_ZOOM = 11

/**
 * Remembers a coordinate the user just picked, so the next photo they geotag in
 * the same sitting opens near it. Best-effort: a disabled or full storage costs
 * the next photo its head start and nothing else.
 */
export function rememberPickedLocation(position: Coordinates): void {
  try {
    window.sessionStorage.setItem(STORAGE_KEY, JSON.stringify(position))
  } catch {
    // Storage unavailable — the picker simply falls back to the library's region.
  }
}

/**
 * The coordinate picked most recently in this session, or `null` when there is
 * none (or what is stored is not a coordinate this build can read).
 *
 * A family archive is geographically lumpy: a box of scans is almost always one
 * village, so the last pin is by far the best guess for where the next one goes.
 */
export function lastPickedLocation(): Coordinates | null {
  try {
    const raw = window.sessionStorage.getItem(STORAGE_KEY)
    if (raw === null) {
      return null
    }
    const parsed: unknown = JSON.parse(raw)
    return asCoordinates(parsed)
  } catch {
    // Storage unavailable or holding something unparseable.
    return null
  }
}

/**
 * Narrows a parsed JSON value to a coordinate, rejecting anything that is not a
 * pair of numbers inside the geographic range — the same bounds the backend
 * validates, so nothing that could not be saved is ever used to centre a map.
 */
function asCoordinates(value: unknown): Coordinates | null {
  if (typeof value !== 'object' || value === null) {
    return null
  }
  const { lat, lng } = value as { lat?: unknown; lng?: unknown }
  if (typeof lat !== 'number' || typeof lng !== 'number') {
    return null
  }
  if (!Number.isFinite(lat) || !Number.isFinite(lng)) {
    return null
  }
  if (lat < -90 || lat > 90 || lng < -180 || lng > 180) {
    return null
  }
  return { lat, lng }
}

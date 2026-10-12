import { type MapFeature } from '../services/map'

/**
 * A geographic bounding box in degrees. `west` may lie east of `east`: such a
 * box crosses the antimeridian and covers the longitudes on both sides of it —
 * the same convention the backend's `bbox` list filter reads
 * (`photos.ParseBBox`), so the map panel and the viewer agree on what "in this
 * area" means.
 */
export interface GeoBounds {
  south: number
  west: number
  north: number
  east: number
}

/** Decimal places a bounding box keeps in a URL: ~1 m, far below a tile's pixel. */
const BBOX_DECIMALS = 5

/** Folds any longitude into [-180, 180], the range the backend accepts. */
function wrapLng(lng: number): number {
  if (lng >= -180 && lng <= 180) {
    return lng
  }
  return ((((lng + 180) % 360) + 360) % 360) - 180
}

/** Clamps a latitude into [-90, 90]. */
function clampLat(lat: number): number {
  return Math.min(90, Math.max(-90, lat))
}

/**
 * Normalises a map viewport into a box the backend accepts. Leaflet reports the
 * bounds of the world copy the user panned to, so a longitude can lie past ±180
 * (a zoomed-out map shows several worlds side by side); a box at least a whole
 * world wide is the whole world, anything narrower keeps its two edges wrapped
 * into range — which may leave `west > east`, the antimeridian case.
 */
export function normalizeBounds(bounds: GeoBounds): GeoBounds {
  const south = clampLat(Math.min(bounds.south, bounds.north))
  const north = clampLat(Math.max(bounds.south, bounds.north))
  if (bounds.east - bounds.west >= 360) {
    return { south, west: -180, north, east: 180 }
  }
  return { south, west: wrapLng(bounds.west), north, east: wrapLng(bounds.east) }
}

/** Whether a point lies inside a (normalised) box, edges included. */
export function boundsContain(bounds: GeoBounds, lat: number, lng: number): boolean {
  if (lat < bounds.south || lat > bounds.north) {
    return false
  }
  if (bounds.west <= bounds.east) {
    return lng >= bounds.west && lng <= bounds.east
  }
  return lng >= bounds.west || lng <= bounds.east
}

/**
 * The photos of the feed inside a box, **in feed order**. The feed already comes
 * newest first — the same `taken_at` descending order the photo list uses — so
 * keeping it (rather than re-sorting here) is what makes the panel's order and
 * the viewer's prev/next through `GET /photos?bbox=…` one and the same.
 */
export function photosInBounds(
  features: readonly MapFeature[],
  bounds: GeoBounds | null,
): MapFeature[] {
  if (bounds === null) {
    return []
  }
  const box = normalizeBounds(bounds)
  return features.filter((feature) => {
    const [lng, lat] = feature.geometry.coordinates
    return boundsContain(box, lat, lng)
  })
}

/**
 * Encodes a box as the `south,west,north,east` string the backend's `bbox`
 * filter and the map's URL keys read. Rounded outward, so a photo sitting
 * exactly on the edge of the visible map is never rounded out of the area.
 */
export function formatBBox(bounds: GeoBounds): string {
  const box = normalizeBounds(bounds)
  const factor = 10 ** BBOX_DECIMALS
  const down = (v: number) => (Math.floor(v * factor) / factor).toFixed(BBOX_DECIMALS)
  const up = (v: number) => (Math.ceil(v * factor) / factor).toFixed(BBOX_DECIMALS)
  return [down(box.south), down(box.west), up(box.north), up(box.east)].join(',')
}

/**
 * Parses a `south,west,north,east` string back into a box, or `null` when it is
 * absent or malformed (a tampered URL then simply has no box).
 */
export function parseBBox(raw: string): GeoBounds | null {
  if (raw === '') {
    return null
  }
  const parts = raw.split(',').map((part) => Number(part.trim()))
  if (parts.length !== 4 || parts.some((part) => !Number.isFinite(part))) {
    return null
  }
  const [south, west, north, east] = parts as [number, number, number, number]
  if (south > north || Math.abs(south) > 90 || Math.abs(north) > 90) {
    return null
  }
  if (Math.abs(west) > 180 || Math.abs(east) > 180) {
    return null
  }
  return { south, west, north, east }
}

/** Two boxes are the same area once written into a URL. */
export function sameBBox(a: GeoBounds | null, b: GeoBounds | null): boolean {
  if (a === null || b === null) {
    return a === b
  }
  return formatBBox(a) === formatBBox(b)
}

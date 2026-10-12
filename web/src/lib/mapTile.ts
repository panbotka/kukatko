import type { TFunction } from 'i18next'

import { type MapFeature } from '../services/map'
import { thumbUrl } from '../services/photos'

import { blurPlaceholderUrl } from './blurPlaceholder'
import { formatCaptureRange, formatDate } from './format'
import { squareRenditionName } from './rendition'

/**
 * The side of a photo tile on the map, in CSS pixels, border included. Big
 * enough to recognise a picture at a glance, small enough that a screen holds a
 * few dozen; the cluster radius ({@link MAP_CLUSTER_RADIUS}) is derived from it,
 * so tiles do not pile into each other.
 */
export const MAP_TILE_PX = 56

/**
 * How far apart (in screen pixels) two photos may be and still share a tile.
 * A little over one tile: two neighbouring tiles then overlap by at most a
 * sliver, and a cluster centre — markercluster's weighted centroid — never sits
 * under its neighbour.
 */
export const MAP_CLUSTER_RADIUS = MAP_TILE_PX + 12

/** The largest count a badge spells out; past it the badge says "999+". */
const MAX_BADGE_COUNT = 999

/**
 * The thumbnail size a map tile loads: the square rendition covering the tile's
 * CSS size on this screen's (capped) device-pixel ratio — `tile_100` on a
 * standard or a 2× screen. The thumbnail route mints the signed URL lazily, one
 * per tile actually shown, so the feed itself never carries a URL at all.
 */
export function mapTileThumbSize(dpr?: number): string {
  return squareRenditionName(MAP_TILE_PX, dpr)
}

/**
 * What one tile stands for: the representative photo it shows, how many photos
 * it covers and the capture-time span they cover — everything the tile and its
 * accessible label say.
 */
export interface TileSummary {
  /** The photo whose thumbnail the tile shows. */
  representative: MapFeature
  /** How many photos the tile covers (1 for a single photo). */
  count: number
  /** Earliest known capture time among them (ISO string), if any is known. */
  oldest?: string
  /** Latest known capture time among them (ISO string), if any is known. */
  newest?: string
}

/**
 * Sums up the photos under one tile. The representative is the **newest** photo
 * — the first in the feed's newest-first order, so `rank` (the feed index) is
 * all it compares. Newest rather than "best" because "best" has no shared
 * meaning here: ratings and favourites are per user and not in the feed, and a
 * map that reshuffled its pictures by somebody's stars would be a different map
 * for everyone. Newest is also what the area panel lists first, so the picture
 * on a cluster is the one the grid opens with.
 */
export function summarizeTile(
  features: readonly MapFeature[],
  rank: (feature: MapFeature) => number,
): TileSummary | null {
  let representative: MapFeature | null = null
  let bestRank = Number.POSITIVE_INFINITY
  let oldest: string | undefined
  let newest: string | undefined
  for (const feature of features) {
    const r = rank(feature)
    if (r < bestRank) {
      bestRank = r
      representative = feature
    }
    const taken = feature.properties.taken_at
    if (taken !== undefined && taken !== '') {
      if (oldest === undefined || taken < oldest) oldest = taken
      if (newest === undefined || taken > newest) newest = taken
    }
  }
  if (representative === null) {
    return null
  }
  return { representative, count: features.length, oldest, newest }
}

/**
 * The tile's accessible name: "12 fotek, 2019–2023" for a cluster, "Fotka,
 * 5. 3. 2021" for a single photo, with the estimate called out in words where
 * the dashed frame says it at a glance. The capture span stands in for a place
 * name the feed does not carry — and on a map the place is what the reader is
 * already looking at.
 */
export function tileLabel(
  t: TFunction,
  language: string,
  summary: TileSummary,
  estimated = false,
): string {
  if (summary.count > 1) {
    const range = formatCaptureRange(summary.oldest, summary.newest)
    return range === ''
      ? t('map.tile.clusterUndated', { count: summary.count })
      : t('map.tile.cluster', { count: summary.count, range })
  }
  const taken = summary.representative.properties.taken_at
  const base =
    taken === undefined || taken === ''
      ? t('map.tile.photoUndated')
      : t('map.tile.photo', { date: formatDate(taken, language) })
  return estimated ? `${base}, ${t('map.tile.estimated')}` : base
}

/** Options for {@link buildMapTile}. */
export interface MapTileOptions {
  /** The thumbnail size to load (see {@link mapTileThumbSize}). */
  thumbSize: string
  /** The accessible name (see {@link tileLabel}). */
  label: string
  /** Whether the photo's position is an estimate (single photos only). */
  estimated?: boolean
}

/**
 * Builds the DOM of one photo tile — the content Leaflet puts inside a marker's
 * (or a cluster's) icon element, which is itself the focusable `role="button"`
 * Leaflet makes of every keyboard-enabled marker. Plain DOM rather than React:
 * markercluster asks for an icon per visible cluster on every zoom step, and a
 * React root per icon would cost far more than the picture it draws.
 *
 * The tile is a rounded square of the representative photo inside a light
 * frame, its blurred placeholder painted first so a freshly revealed cluster is
 * already the colours of its picture, with the count in a badge on the corner
 * for a cluster. The image loads lazily and fades in over the blur on `load`; a
 * failed load just leaves the blur. Its own text is a visually hidden label, so
 * the button's name is the sentence {@link tileLabel} wrote, never a file name.
 */
export function buildMapTile(summary: TileSummary, options: MapTileOptions): HTMLElement {
  const { uid, blurhash, media_type: mediaType } = summary.representative.properties
  const tile = document.createElement('div')
  tile.className = 'kk-map-tile'
  if (summary.count > 1) tile.classList.add('kk-map-tile--cluster')
  if (options.estimated === true) tile.classList.add('kk-map-tile--estimated')
  tile.dataset.uid = uid

  const frame = document.createElement('span')
  frame.className = 'kk-map-tile-frame'
  frame.setAttribute('aria-hidden', 'true')
  const blur = blurPlaceholderUrl(blurhash)
  if (blur !== undefined) {
    const placeholder = document.createElement('span')
    placeholder.className = 'kk-media-blur'
    placeholder.style.backgroundImage = `url("${blur}")`
    frame.appendChild(placeholder)
  }
  const img = document.createElement('img')
  img.className = 'kk-map-tile-img'
  img.alt = ''
  img.setAttribute('loading', 'lazy')
  img.setAttribute('decoding', 'async')
  img.draggable = false
  img.addEventListener('load', () => {
    img.classList.add('is-loaded')
  })
  img.addEventListener('error', () => {
    img.classList.add('is-failed')
  })
  img.src = thumbUrl(uid, options.thumbSize)
  frame.appendChild(img)
  tile.appendChild(frame)

  if (summary.count > 1) {
    const badge = document.createElement('span')
    badge.className = 'kk-map-tile-count'
    badge.setAttribute('aria-hidden', 'true')
    badge.textContent =
      summary.count > MAX_BADGE_COUNT ? `${String(MAX_BADGE_COUNT)}+` : String(summary.count)
    tile.appendChild(badge)
  } else if (mediaType === 'video') {
    const play = document.createElement('i')
    play.className = 'kk-map-tile-play bi bi-play-fill'
    play.setAttribute('aria-hidden', 'true')
    tile.appendChild(play)
  }

  const label = document.createElement('span')
  label.className = 'visually-hidden'
  label.textContent = options.label
  tile.appendChild(label)
  return tile
}

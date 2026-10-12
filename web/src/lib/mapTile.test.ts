import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { type MapFeature } from '../services/map'

import {
  buildMapTile,
  MAP_CLUSTER_RADIUS,
  MAP_TILE_PX,
  mapTileThumbSize,
  summarizeTile,
  tileLabel,
  type TileSummary,
} from './mapTile'

// jsdom has no canvas to decode a BlurHash into; stand in a fixed data URL so
// the tile's placeholder can be asserted.
vi.mock('./blurPlaceholder', () => ({
  blurPlaceholderUrl: (hash: string | undefined) =>
    hash === undefined || hash === '' ? undefined : `data:image/png;base64,${hash}`,
}))

function photo(uid: string, takenAt?: string, extra: Partial<MapFeature['properties']> = {}) {
  const feature: MapFeature = {
    type: 'Feature',
    geometry: { type: 'Point', coordinates: [16.6, 49.2] },
    properties: { uid, title: '', media_type: 'image', thumb: '', taken_at: takenAt, ...extra },
  }
  return feature
}

beforeEach(async () => {
  await i18n.changeLanguage('cs')
})

describe('map tile geometry', () => {
  it('clusters within a little over one tile, so tiles do not pile up', () => {
    expect(MAP_CLUSTER_RADIUS).toBeGreaterThan(MAP_TILE_PX)
  })

  it('loads the square rendition covering the tile on the screen', () => {
    expect(mapTileThumbSize(1)).toBe('tile_100')
    expect(mapTileThumbSize(2)).toBe('tile_100')
    expect(mapTileThumbSize(3)).toBe('tile_100')
  })
})

describe('summarizeTile', () => {
  it('shows the newest photo — the first in the feed order — and spans the dates', () => {
    const features = [
      photo('b', '2019-05-01T10:00:00Z'),
      photo('a', '2023-08-01T10:00:00Z'),
      photo('c'),
    ]
    const rank = (f: MapFeature) => ['a', 'b', 'c'].indexOf(f.properties.uid)
    const summary = summarizeTile(features, rank)
    expect(summary?.representative.properties.uid).toBe('a')
    expect(summary?.count).toBe(3)
    expect(summary?.oldest).toBe('2019-05-01T10:00:00Z')
    expect(summary?.newest).toBe('2023-08-01T10:00:00Z')
  })

  it('has nothing to show for no photos', () => {
    expect(summarizeTile([], () => 0)).toBeNull()
  })
})

describe('tileLabel', () => {
  const t = i18n.t.bind(i18n)

  it('names a cluster by its count and capture span, in Czech plurals', () => {
    const cluster = (count: number): TileSummary => ({
      representative: photo('a'),
      count,
      oldest: '2019-05-01T10:00:00Z',
      newest: '2023-08-01T10:00:00Z',
    })
    expect(tileLabel(t, 'cs', cluster(3))).toBe('3 fotky, 2019–2023')
    expect(tileLabel(t, 'cs', cluster(12))).toBe('12 fotek, 2019–2023')
  })

  it('leaves the span out when no photo in the cluster is dated', () => {
    expect(tileLabel(t, 'cs', { representative: photo('a'), count: 2 })).toBe('2 fotky')
  })

  it('names a single photo by its date and calls out an estimate in words', () => {
    const single: TileSummary = { representative: photo('a', '2021-03-05T12:00:00Z'), count: 1 }
    expect(tileLabel(t, 'cs', single)).toBe('Fotka, 5. 3. 2021')
    expect(tileLabel(t, 'cs', single, true)).toBe('Fotka, 5. 3. 2021, odhadovaná poloha')
    expect(tileLabel(t, 'cs', { representative: photo('a'), count: 1 })).toBe('Fotka bez data')
  })

  it('speaks English too', async () => {
    await i18n.changeLanguage('en')
    const cluster: TileSummary = { representative: photo('a'), count: 1200 }
    expect(tileLabel(i18n.t.bind(i18n), 'en', cluster)).toBe('1200 photos')
  })
})

describe('buildMapTile', () => {
  it('draws a cluster as its photo over the blur, with the count in a badge', () => {
    const tile = buildMapTile(
      { representative: photo('ph1', undefined, { blurhash: 'LEHV6n' }), count: 12 },
      { thumbSize: 'tile_100', label: '12 fotek, 2019–2023' },
    )
    expect(tile).toHaveClass('kk-map-tile', 'kk-map-tile--cluster')
    expect(tile.dataset.uid).toBe('ph1')
    expect(tile.querySelector('.kk-map-tile-count')?.textContent).toBe('12')
    const blur = tile.querySelector<HTMLElement>('.kk-media-blur')
    expect(blur?.style.backgroundImage).toContain('data:image/png;base64,LEHV6n')
    const img = tile.querySelector('img')
    expect(img?.getAttribute('src')).toBe('/api/v1/photos/ph1/thumb/tile_100')
    expect(img?.getAttribute('loading')).toBe('lazy')
    expect(img?.getAttribute('alt')).toBe('')
    // The button's name is the sentence, not the badge's digits.
    expect(tile.querySelector('.visually-hidden')?.textContent).toBe('12 fotek, 2019–2023')
    expect(tile.querySelector('.kk-map-tile-count')).toHaveAttribute('aria-hidden', 'true')
  })

  it('caps the badge and fades the image in only once it has loaded', () => {
    const tile = buildMapTile(
      { representative: photo('ph1'), count: 4321 },
      { thumbSize: 'tile_100', label: 'x' },
    )
    expect(tile.querySelector('.kk-map-tile-count')?.textContent).toBe('999+')
    const img = tile.querySelector('img')
    expect(img).not.toHaveClass('is-loaded')
    img?.dispatchEvent(new Event('load'))
    expect(img).toHaveClass('is-loaded')
  })

  it('draws a single photo without a badge, marks a video and an estimate', () => {
    const tile = buildMapTile(
      { representative: photo('v1', undefined, { media_type: 'video' }), count: 1 },
      { thumbSize: 'tile_100', label: 'Fotka', estimated: true },
    )
    expect(tile.querySelector('.kk-map-tile-count')).toBeNull()
    expect(tile.querySelector('.kk-map-tile-play')).not.toBeNull()
    expect(tile).toHaveClass('kk-map-tile--estimated')
    // No hash, no placeholder: the frame's own surface shows.
    expect(tile.querySelector('.kk-media-blur')).toBeNull()
  })
})

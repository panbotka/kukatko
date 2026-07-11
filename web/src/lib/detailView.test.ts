import { describe, expect, it } from 'vitest'

import { backHref, DETAIL_DEFAULTS, detailQueryString, detailToParams } from './detailView'

describe('detailView helpers', () => {
  it('folds the album/label/favorite scope into the list params', () => {
    const params = detailToParams({ ...DETAIL_DEFAULTS, sort: 'oldest', album: 'al_1' })
    expect(params.album).toBe('al_1')
    expect(params.label).toBe('')
    expect(params.favorite).toBe('')
    expect(params.sort).toBe('oldest')
  })

  it('omits default values from the detail query string', () => {
    expect(detailQueryString(DETAIL_DEFAULTS)).toBe('')
    const query = detailQueryString({ ...DETAIL_DEFAULTS, sort: 'oldest', album: 'al_1' })
    const parsed = new URLSearchParams(query)
    expect(parsed.get('sort')).toBe('oldest')
    expect(parsed.get('album')).toBe('al_1')
  })

  it('builds a Back link to the originating scope, carrying the library filters', () => {
    expect(backHref(DETAIL_DEFAULTS)).toBe('/')
    expect(backHref({ ...DETAIL_DEFAULTS, album: 'al_1' })).toBe('/albums/al_1')
    expect(backHref({ ...DETAIL_DEFAULTS, label: 'lb_2' })).toBe('/labels/lb_2')
    expect(backHref({ ...DETAIL_DEFAULTS, favorite: 'true' })).toBe('/favorites')
    // Library filters (but not the scope) are appended as a query suffix.
    expect(backHref({ ...DETAIL_DEFAULTS, album: 'al_1', sort: 'oldest' })).toBe(
      '/albums/al_1?sort=oldest',
    )
  })

  it('builds a Back link to the search when a search mode scope is present', () => {
    // A default-mode (hybrid) search with no other state still returns to /search,
    // because the search grid always writes the mode as the scope marker.
    expect(backHref({ ...DETAIL_DEFAULTS, mode: 'hybrid' })).toBe('/search')
    // The search query and filters travel back; hybrid (the search default) is
    // omitted, a non-default mode is carried.
    expect(backHref({ ...DETAIL_DEFAULTS, mode: 'hybrid', q: 'cat' })).toBe('/search?q=cat')
    expect(backHref({ ...DETAIL_DEFAULTS, mode: 'fulltext', q: 'cat' })).toBe(
      '/search?q=cat&mode=fulltext',
    )
  })

  it('carries the search mode in the detail query string as the scope marker', () => {
    // The library/album/label/favorites views leave mode empty, so it never
    // appears; a search always writes it, even the default hybrid.
    expect(new URLSearchParams(detailQueryString(DETAIL_DEFAULTS)).get('mode')).toBeNull()
    const query = detailQueryString({ ...DETAIL_DEFAULTS, mode: 'hybrid', q: 'cat' })
    const parsed = new URLSearchParams(query)
    expect(parsed.get('mode')).toBe('hybrid')
    expect(parsed.get('q')).toBe('cat')
  })
})

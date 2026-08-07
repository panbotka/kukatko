import { describe, expect, it } from 'vitest'

import { type AlbumSummary, type AlbumType } from '../services/organize'

import {
  type AlbumsView,
  albumBrowseOptions,
  ALBUMS_DEFAULTS,
  browseAlbums,
  tabForType,
  toAlbumSort,
  toAlbumTab,
} from './albumBrowse'

/** Builds an album summary with just the fields the browse rules read. */
function album(
  title: string,
  type: AlbumType,
  photoCount: number,
  uid = title.toLowerCase(),
): AlbumSummary {
  return {
    uid,
    slug: uid,
    title,
    description: '',
    type,
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: photoCount,
  }
}

/** The albums a library ends up with: a few hand-made, mostly machine-made. */
const LIBRARY: AlbumSummary[] = [
  album('Dovolená 2019', 'album', 42),
  album('Zebra', 'album', 7),
  album('Pets', 'album', 0),
  album('January 2026', 'folder', 15),
  album('May 2026', 'month', 3),
  album('Trip to the lake', 'moment', 9),
  album('Czechia', 'state', 120),
]

/** The default view with the given overrides, as the URL would encode it. */
function view(overrides: Partial<AlbumsView> = {}): AlbumsView {
  return { ...ALBUMS_DEFAULTS, ...overrides }
}

/** Runs the browse rules over the fixture library under a Czech UI. */
function browse(overrides: Partial<AlbumsView> = {}) {
  return browseAlbums(LIBRARY, albumBrowseOptions(view(overrides), 'cs'))
}

/** The visible albums' stored titles, in the order they are rendered. */
function titles(result: { visible: AlbumSummary[] }): string[] {
  return result.visible.map((a) => a.title)
}

describe('tabForType', () => {
  it('gives every album type a section, folding month in with folder', () => {
    expect(tabForType('album')).toBe('album')
    expect(tabForType('folder')).toBe('folder')
    expect(tabForType('month')).toBe('folder')
    expect(tabForType('moment')).toBe('moment')
    expect(tabForType('state')).toBe('state')
  })

  it('keeps an unknown type reachable in the default section', () => {
    // A type this frontend does not know yet must not vanish from every tab.
    expect(tabForType('brand-new' as AlbumType)).toBe('album')
  })
})

describe('toAlbumTab / toAlbumSort', () => {
  it('accepts the known values and falls back on anything else', () => {
    expect(toAlbumTab('moment')).toBe('moment')
    expect(toAlbumTab('nonsense')).toBe('album')
    expect(toAlbumSort('name')).toBe('name')
    expect(toAlbumSort('nonsense')).toBe('date')
  })
})

describe('browseAlbums', () => {
  it('opens on the hand-made albums, without the machine-made ones', () => {
    expect(titles(browse())).toEqual(['Dovolená 2019', 'Zebra'])
  })

  it('hides albums holding no photos until the switch asks for them', () => {
    expect(titles(browse())).not.toContain('Pets')
    expect(titles(browse({ empty: '1' }))).toContain('Pets')
  })

  it('splits the machine-made albums by type', () => {
    expect(titles(browse({ type: 'folder' }))).toEqual(['January 2026', 'May 2026'])
    expect(titles(browse({ type: 'moment' }))).toEqual(['Trip to the lake'])
    expect(titles(browse({ type: 'state' }))).toEqual(['Czechia'])
  })

  it('counts what each section holds under the current filters', () => {
    expect(browse().counts).toEqual({ album: 2, folder: 2, moment: 1, state: 1 })
    expect(browse({ empty: '1' }).counts.album).toBe(3)
  })

  it('reports how many albums the filters dropped', () => {
    expect(browse().filteredOut).toBe(1)
    expect(browse({ empty: '1' }).filteredOut).toBe(0)
  })

  it('keeps the order the server returned when sorting by date', () => {
    // The server ranks by the newest photo, which the client cannot recompute —
    // so "newest first" means "do not reorder".
    expect(titles(browse())).toEqual(['Dovolená 2019', 'Zebra'])
  })

  it('sorts by name, case- and accent-insensitively', () => {
    expect(titles(browse({ sort: 'name', empty: '1' }))).toEqual(['Dovolená 2019', 'Pets', 'Zebra'])
  })

  it('sorts a month section by the Czech name the reader actually sees', () => {
    // květen before leden would be the English order; the reader sees Czech.
    expect(titles(browse({ type: 'folder', sort: 'name' }))).toEqual(['May 2026', 'January 2026'])
  })

  it('sorts by photo count, most first', () => {
    expect(titles(browse({ sort: 'count' }))).toEqual(['Dovolená 2019', 'Zebra'])
    expect(titles(browse({ sort: 'count', empty: '1' }))).toEqual([
      'Dovolená 2019',
      'Zebra',
      'Pets',
    ])
  })

  it('searches the stored name, ignoring case and diacritics', () => {
    expect(titles(browse({ q: 'dovolena' }))).toEqual(['Dovolená 2019'])
  })

  it('searches the Czech display name too, so `leden` finds `January 2026`', () => {
    expect(titles(browse({ type: 'folder', q: 'leden' }))).toEqual(['January 2026'])
    expect(titles(browse({ type: 'folder', q: 'January' }))).toEqual(['January 2026'])
  })

  it('counts the matches of every section, so a search points at where they are', () => {
    const result = browse({ q: 'january' })
    expect(result.visible).toEqual([])
    expect(result.counts).toEqual({ album: 0, folder: 1, moment: 0, state: 0 })
  })

  it('leaves the input list untouched', () => {
    const before = LIBRARY.map((a) => a.title)
    browse({ sort: 'name', empty: '1' })
    expect(LIBRARY.map((a) => a.title)).toEqual(before)
  })
})

describe('albumBrowseOptions', () => {
  it('decodes the URL strings into the browse options', () => {
    expect(
      albumBrowseOptions(view({ type: 'state', sort: 'count', empty: '1', q: 'x' }), 'cs'),
    ).toEqual({ tab: 'state', sort: 'count', showEmpty: true, query: 'x', language: 'cs' })
  })

  it('treats anything but the show-empty marker as hiding empty albums', () => {
    expect(albumBrowseOptions(view({ empty: '' }), 'cs').showEmpty).toBe(false)
    expect(albumBrowseOptions(view({ empty: '0' }), 'cs').showEmpty).toBe(false)
  })
})

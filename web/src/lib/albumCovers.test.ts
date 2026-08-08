import { describe, expect, it } from 'vitest'

import { ALBUM_COLLAGE_TILES, albumCover, coverPhotoUids, planAlbumCovers } from './albumCovers'

import { type AlbumSummary } from '../services/organize'

/** Builds an album summary fixture, overriding the fields a case cares about. */
function album(overrides: Partial<AlbumSummary> = {}): AlbumSummary {
  return {
    uid: 'al1',
    slug: 'album',
    title: 'Album',
    description: '',
    type: 'album',
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: 0,
    ...overrides,
  }
}

/** `n` candidate uids with the given prefix, newest first as the server sends them. */
function photos(prefix: string, n: number): string[] {
  return Array.from({ length: n }, (_, i) => `${prefix}${i + 1}`)
}

describe('albumCover', () => {
  it('draws a collage from the album’s newest photos', () => {
    const cover = albumCover(album({ cover_uids: photos('p', 8), cover_uid: 'p1' }))
    expect(cover).toEqual({ kind: 'collage', photoUids: ['p1', 'p2', 'p3', 'p4'] })
  })

  it('degrades to a single image below the collage size', () => {
    const uids = photos('p', ALBUM_COLLAGE_TILES - 1)
    expect(albumCover(album({ cover_uids: uids, cover_uid: 'p1' }))).toEqual({
      kind: 'single',
      photoUid: 'p1',
    })
  })

  it('shows a hand-picked cover alone, even when a collage would fit', () => {
    // Somebody answered "this is what the album looks like"; a guess must not
    // dilute that answer into one cell of four.
    const cover = albumCover(
      album({ cover_photo_uid: 'chosen', cover_uid: 'chosen', cover_uids: photos('p', 8) }),
    )
    expect(cover).toEqual({ kind: 'single', photoUid: 'chosen' })
  })

  it('keeps a hand-picked cover even when an earlier tile took it', () => {
    const cover = albumCover(album({ cover_photo_uid: 'chosen' }), new Set(['chosen']))
    expect(cover).toEqual({ kind: 'single', photoUid: 'chosen' })
  })

  it('falls back to the single cover when the server sends no candidate list', () => {
    expect(albumCover(album({ cover_uid: 'only' }))).toEqual({ kind: 'single', photoUid: 'only' })
  })

  it('draws nothing for an album with no photo to show', () => {
    expect(albumCover(album())).toEqual({ kind: 'none' })
    expect(albumCover(album({ cover_uid: '', cover_uids: [] }))).toEqual({ kind: 'none' })
  })

  it('steps past the photos an earlier tile already used', () => {
    const cover = albumCover(album({ cover_uids: photos('p', 8) }), new Set(['p1', 'p2']))
    expect(cover).toEqual({ kind: 'collage', photoUids: ['p3', 'p4', 'p5', 'p6'] })
  })

  it('tops a collage up rather than leaving a hole', () => {
    // Only two photos are left unused, and three pictures with a gap would
    // advertise the repeat louder than a repeated picture does.
    const cover = albumCover(album({ cover_uids: photos('p', 5) }), new Set(['p1', 'p2', 'p3']))
    expect(cover).toEqual({ kind: 'collage', photoUids: ['p4', 'p5', 'p1', 'p2'] })
  })

  it('steps a single image past a used photo too', () => {
    const cover = albumCover(album({ cover_uids: ['p1', 'p2'] }), new Set(['p1']))
    expect(cover).toEqual({ kind: 'single', photoUid: 'p2' })
  })

  it('repeats only once every candidate is spoken for', () => {
    const cover = albumCover(album({ cover_uids: ['p1', 'p2'] }), new Set(['p1', 'p2']))
    expect(cover).toEqual({ kind: 'single', photoUid: 'p1' })
  })
})

describe('coverPhotoUids', () => {
  it('lists what each kind of cover draws', () => {
    expect(coverPhotoUids({ kind: 'none' })).toEqual([])
    expect(coverPhotoUids({ kind: 'single', photoUid: 'p1' })).toEqual(['p1'])
    expect(coverPhotoUids({ kind: 'collage', photoUids: ['p1', 'p2'] })).toEqual(['p1', 'p2'])
  })
})

describe('planAlbumCovers', () => {
  it('gives overlapping albums different covers', () => {
    // The bug, in miniature: four albums holding the same scanned title page all
    // showed that page, so the grid said nothing the titles did not.
    const shared = photos('p', 8)
    const covers = planAlbumCovers([
      album({ uid: 'a', cover_uids: shared, cover_uid: 'p1' }),
      album({ uid: 'b', cover_uids: shared, cover_uid: 'p1' }),
    ])

    expect(covers.get('a')).toEqual({ kind: 'collage', photoUids: ['p1', 'p2', 'p3', 'p4'] })
    expect(covers.get('b')).toEqual({ kind: 'collage', photoUids: ['p5', 'p6', 'p7', 'p8'] })
  })

  it('shares no photo between any two tiles while candidates last', () => {
    const covers = planAlbumCovers(
      ['a', 'b', 'c', 'd'].map((uid) => album({ uid, cover_uids: photos('p', 16) })),
    )
    const drawn = [...covers.values()].flatMap(coverPhotoUids)
    expect(drawn).toHaveLength(4 * ALBUM_COLLAGE_TILES)
    expect(new Set(drawn).size).toBe(drawn.length)
  })

  it('plans a small album around what the big ones took', () => {
    const covers = planAlbumCovers([
      album({ uid: 'big', cover_uids: ['p1', 'p2', 'p3', 'p4', 'p5'] }),
      album({ uid: 'small', cover_uids: ['p1', 'p5'] }),
    ])
    expect(covers.get('small')).toEqual({ kind: 'single', photoUid: 'p5' })
  })

  it('is a pure function of the list, so a tile keeps its cover across renders', () => {
    // The grid is virtualized: a tile scrolled out and back is mounted afresh,
    // and a plan that depended on anything but the list would deal it a new cover.
    const albums = [
      album({ uid: 'a', cover_uids: photos('p', 8) }),
      album({ uid: 'b', cover_uids: photos('p', 8) }),
    ]
    expect([...planAlbumCovers(albums)]).toEqual([...planAlbumCovers(albums)])
  })

  it('keys every album, including the ones with nothing to draw', () => {
    const covers = planAlbumCovers([album({ uid: 'a' }), album({ uid: 'b', cover_uid: 'p1' })])
    expect([...covers.keys()]).toEqual(['a', 'b'])
    expect(covers.get('a')).toEqual({ kind: 'none' })
  })
})

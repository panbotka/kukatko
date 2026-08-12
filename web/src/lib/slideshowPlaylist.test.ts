import { describe, expect, it } from 'vitest'

import { extendSeen, newShuffleSeed, playlistOf } from './slideshowPlaylist'

/** A playlist entry stripped to what these rules read: its identity. */
function item(uid: string): { uid: string } {
  return { uid }
}

const A = item('a')
const B = item('b')
const C = item('c')
const D = item('d')

describe('playlistOf', () => {
  it('is the loaded list itself when nothing has been carried', () => {
    const loaded = [A, B, C]

    // Identity, not just equality: the ordinary show must not pay for a merge it
    // does not need on every slide.
    expect(playlistOf([], loaded)).toBe(loaded)
  })

  it('puts the seen photos first and drops them from what is still to come', () => {
    // The reader has seen a and b, then turned shuffle on: the reloaded list
    // comes back in a new order and happens to lead with a photo already played.
    expect(playlistOf([A, B], [C, A, D, B])).toEqual([A, B, C, D])
  })

  it('never replays a carried photo, whatever order the reload brings', () => {
    const playlist = playlistOf([C, A], [A, B, C, D])
    const uids = playlist.map((p) => p.uid)

    expect(uids).toEqual(['c', 'a', 'b', 'd'])
    expect(new Set(uids).size).toBe(uids.length)
  })

  it('keeps the whole set reachable: every loaded photo is somewhere in it', () => {
    const loaded = [A, B, C, D]
    const uids = new Set(playlistOf([B], loaded).map((p) => p.uid))

    for (const photo of loaded) {
      expect(uids.has(photo.uid)).toBe(true)
    }
  })
})

describe('extendSeen', () => {
  it('grows to cover the cursor', () => {
    expect(extendSeen([], [A, B, C], 0)).toEqual([A])
    expect(extendSeen([A], [A, B, C], 2)).toEqual([A, B, C])
  })

  it('does not shrink when the reader steps back', () => {
    const seen = [A, B, C]

    // Revisiting a photo does not un-see it: the pass is only over on a wrap.
    expect(extendSeen(seen, [A, B, C], 0)).toBe(seen)
  })

  it('stops at the end of the playlist', () => {
    expect(extendSeen([], [A, B], 9)).toEqual([A, B])
  })
})

describe('newShuffleSeed', () => {
  it('produces a non-empty seed that differs between shows', () => {
    const seeds = new Set(Array.from({ length: 20 }, () => newShuffleSeed()))

    for (const seed of seeds) {
      expect(seed).not.toBe('')
    }
    // Two shows starting with the same order would be a coincidence, not a rule.
    expect(seeds.size).toBeGreaterThan(1)
  })
})

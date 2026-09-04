import { type StateSnapshot } from 'react-virtuoso'
import { beforeEach, describe, expect, it } from 'vitest'

import {
  GRID_SCROLL_MAX_ENTRIES,
  gridScrollKey,
  readGridScroll,
  writeGridScroll,
} from './gridScroll'

const STORAGE_KEY = 'kukatko.gridScroll'

/** A plausible virtuoso snapshot at the given offset. */
function snapshot(scrollTop: number): StateSnapshot {
  return {
    ranges: [
      { startIndex: 0, endIndex: 11, size: 220 },
      { startIndex: 12, endIndex: 30, size: 180 },
    ],
    scrollTop,
  }
}

beforeEach(() => {
  window.sessionStorage.clear()
})

describe('gridScrollKey', () => {
  it('keys on the path and the filters that define the result set', () => {
    expect(gridScrollKey('/', '')).toBe('/')
    expect(gridScrollKey('/albums/al_1', '?sort=oldest')).toBe('/albums/al_1?sort=oldest')
  })

  it('ignores the order the query was written in', () => {
    expect(gridScrollKey('/', '?sort=oldest&q=cat')).toBe(gridScrollKey('/', '?q=cat&sort=oldest'))
  })

  it('drops the params that name a position rather than a result set', () => {
    // `at` is the timeline's month and `info` the viewer's drawer: neither
    // changes which photos are listed, so both must key to the same view.
    expect(gridScrollKey('/', '?at=2013-05&sort=oldest')).toBe(gridScrollKey('/', '?sort=oldest'))
    expect(gridScrollKey('/', '?info=1')).toBe('/')
  })

  it('keys a different filter to a different view', () => {
    // The point of the key: a position taken under one filter must never be
    // restored into the unrelated result set another one produces.
    expect(gridScrollKey('/', '?sort=oldest')).not.toBe(gridScrollKey('/', '?sort=newest'))
  })
})

describe('readGridScroll / writeGridScroll', () => {
  it('round-trips a remembered position', () => {
    writeGridScroll('/', { count: 400, scrollY: 4000, snapshot: snapshot(3800) })

    expect(readGridScroll('/')).toEqual({
      count: 400,
      scrollY: 4000,
      snapshot: snapshot(3800),
    })
  })

  it('keeps views apart', () => {
    writeGridScroll('/', { count: 0, scrollY: 100, snapshot: snapshot(100) })
    writeGridScroll('/albums/al_1', { count: 200, scrollY: 900, snapshot: snapshot(900) })

    expect(readGridScroll('/')?.scrollY).toBe(100)
    expect(readGridScroll('/albums/al_1')?.scrollY).toBe(900)
    expect(readGridScroll('/labels/lb_1')).toBeNull()
  })

  it('remembers a position with no snapshot (a grid that is not virtualized)', () => {
    writeGridScroll('/people/su_1', { count: 300, scrollY: 2400 })

    expect(readGridScroll('/people/su_1')).toEqual({ count: 300, scrollY: 2400 })
  })

  it('reads and writes nothing under an empty key', () => {
    writeGridScroll('', { count: 1, scrollY: 1 })

    expect(readGridScroll('')).toBeNull()
    expect(window.sessionStorage.getItem(STORAGE_KEY)).toBeNull()
  })

  it('drops the oldest views once there are too many', () => {
    for (let i = 0; i < GRID_SCROLL_MAX_ENTRIES + 2; i++) {
      writeGridScroll(`/view-${String(i)}`, { count: 0, scrollY: i + 1 })
    }

    expect(readGridScroll('/view-0')).toBeNull()
    expect(readGridScroll('/view-1')).toBeNull()
    expect(readGridScroll('/view-2')?.scrollY).toBe(3)
    expect(readGridScroll(`/view-${String(GRID_SCROLL_MAX_ENTRIES + 1)}`)).not.toBeNull()
  })

  it('keeps a view the reader keeps coming back to', () => {
    writeGridScroll('/', { count: 0, scrollY: 10 })
    for (let i = 0; i < GRID_SCROLL_MAX_ENTRIES - 1; i++) {
      writeGridScroll(`/view-${String(i)}`, { count: 0, scrollY: 1 })
    }
    // Re-visiting the library moves it back to the newest end, so the next few
    // views push out the ones that were not touched instead.
    writeGridScroll('/', { count: 0, scrollY: 20 })
    writeGridScroll('/fresh', { count: 0, scrollY: 1 })

    expect(readGridScroll('/')?.scrollY).toBe(20)
    expect(readGridScroll('/view-0')).toBeNull()
  })

  it('ignores a stored value it cannot read', () => {
    window.sessionStorage.setItem(STORAGE_KEY, 'not json at all')
    expect(readGridScroll('/')).toBeNull()

    // A snapshot missing its measurements would restore a nonsense layout, so it
    // is dropped while the plain offset beside it survives.
    window.sessionStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({ '/': { count: 5, scrollY: 300, snapshot: { scrollTop: 300 } } }),
    )
    expect(readGridScroll('/')).toEqual({ count: 5, scrollY: 300 })

    // Same for a range that is not a measurement, and for the shape the build
    // with a uniform square grid used to write.
    window.sessionStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({
        '/': { count: 5, scrollY: 300, snapshot: { scrollTop: 300, ranges: [{ size: 220 }] } },
      }),
    )
    expect(readGridScroll('/')).toEqual({ count: 5, scrollY: 300 })

    window.sessionStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({
        '/': {
          count: 5,
          scrollY: 300,
          snapshot: {
            gap: { column: 8, row: 8 },
            item: { height: 220, width: 220 },
            scrollTop: 300,
            viewport: { height: 900, width: 1400 },
          },
        },
      }),
    )
    expect(readGridScroll('/')).toEqual({ count: 5, scrollY: 300 })
  })

  // The measured shape of a real virtuoso snapshot: react-virtuoso closes the
  // size tree with an open-ended range, whose `endIndex` is `Infinity` — which
  // `JSON.stringify` writes as `null`. Rejecting that dropped *every* snapshot a
  // window-scrolled grid ever wrote, which is what left the reader at the top of
  // the library on the way back from a photograph.
  it('round-trips the open-ended range a virtuoso snapshot ends with', () => {
    const open: StateSnapshot = {
      ranges: [
        { startIndex: 0, endIndex: 4, size: 105 },
        { startIndex: 5, endIndex: Number.POSITIVE_INFINITY, size: 106 },
      ],
      scrollTop: 3872,
    }
    writeGridScroll('/', { count: 0, scrollY: 4000, snapshot: open })

    expect(readGridScroll('/')).toEqual({ count: 0, scrollY: 4000, snapshot: open })
  })

  it('drops an entry with a nonsensical offset', () => {
    window.sessionStorage.setItem(STORAGE_KEY, JSON.stringify({ '/': { count: 1, scrollY: -5 } }))
    expect(readGridScroll('/')).toBeNull()
  })
})

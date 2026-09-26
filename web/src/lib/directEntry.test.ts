import { afterEach, describe, expect, it } from 'vitest'

import { directEntryState, isDirectEntry, isFirstEntry } from './directEntry'

describe('isDirectEntry', () => {
  it('recognises the state a forwarding page attaches', () => {
    expect(isDirectEntry(directEntryState())).toBe(true)
  })

  it('reads anything else as "not forwarded from a first entry"', () => {
    for (const state of [
      undefined,
      null,
      'directEntry',
      {},
      { directEntry: 'true' },
      { directEntry: 1 },
    ]) {
      expect(isDirectEntry(state)).toBe(false)
    }
  })
})

describe('isFirstEntry', () => {
  afterEach(() => {
    window.history.replaceState(null, '')
  })

  it('treats the router’s default key as a first entry', () => {
    window.history.replaceState({ idx: 3 }, '')
    expect(isFirstEntry('default')).toBe(true)
  })

  it('treats the browser router’s index 0 as a first entry, whatever the key', () => {
    // The sign-in round trip: guard and login both replace, so the entry has a
    // real key but still nothing of the app behind it.
    window.history.replaceState({ idx: 0, key: 'abc' }, '')
    expect(isFirstEntry('abc')).toBe(true)
  })

  it('knows a later entry has something behind it', () => {
    window.history.replaceState({ idx: 2, key: 'abc' }, '')
    expect(isFirstEntry('abc')).toBe(false)
  })

  it('lets the key decide when the history keeps no index (a memory router)', () => {
    window.history.replaceState(null, '')
    expect(isFirstEntry('abc')).toBe(false)
  })
})

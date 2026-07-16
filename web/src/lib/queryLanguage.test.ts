import { describe, expect, it } from 'vitest'

import { applyFilterKey, FILTER_KEYS, suggestFilterKeys } from './queryLanguage'

describe('suggestFilterKeys', () => {
  it('suggests keys sharing the trailing token prefix', () => {
    const s = suggestFilterKeys('beach ca')
    expect(s).not.toBeNull()
    expect(s?.keys).toEqual(['camera'])
    expect(s?.start).toBe(6)
  })

  it('suggests several keys for a shared prefix', () => {
    const s = suggestFilterKeys('c')
    expect(s?.keys).toEqual(['camera', 'city', 'codec', 'country'])
  })

  it('matches case-insensitively', () => {
    expect(suggestFilterKeys('Ca')?.keys).toEqual(['camera'])
  })

  it('returns null for an empty input', () => {
    expect(suggestFilterKeys('')).toBeNull()
    expect(suggestFilterKeys('beach ')).toBeNull()
  })

  it('returns null once the key is completed with a colon', () => {
    expect(suggestFilterKeys('camera:')).toBeNull()
    expect(suggestFilterKeys('camera:can')).toBeNull()
  })

  it('returns null for a non-letter token', () => {
    expect(suggestFilterKeys('iso:100 20')).toBeNull()
    expect(suggestFilterKeys('-blur')).toBeNull()
  })

  it('returns null inside an open quote', () => {
    expect(suggestFilterKeys('camera:"Canon ca')).toBeNull()
  })

  it('returns null when no key matches', () => {
    expect(suggestFilterKeys('zzz')).toBeNull()
  })

  it('returns null when the token already equals a key exactly and alone', () => {
    expect(suggestFilterKeys('iso')).toBeNull()
  })

  it('knows every documented key', () => {
    for (const key of FILTER_KEYS) {
      const prefix = key.slice(0, key.length - 1)
      if (prefix === '') {
        continue
      }
      const s = suggestFilterKeys(prefix)
      // Either the prefix is itself another key (e.g. `face` before `faces`)
      // or the key must be offered.
      const offered = s?.keys.includes(key) ?? false
      const prefixIsKey = (FILTER_KEYS as readonly string[]).includes(prefix)
      expect(offered || prefixIsKey, `key ${key} unreachable`).toBe(true)
    }
  })
})

describe('applyFilterKey', () => {
  it('replaces the trailing token with the key and a colon', () => {
    const s = suggestFilterKeys('beach ca')
    expect(s).not.toBeNull()
    if (s) {
      expect(applyFilterKey('beach ca', s, 'camera')).toBe('beach camera:')
    }
  })

  it('works at the start of the input', () => {
    const s = suggestFilterKeys('la')
    expect(s).not.toBeNull()
    if (s) {
      expect(applyFilterKey('la', s, 'label')).toBe('label:')
    }
  })
})

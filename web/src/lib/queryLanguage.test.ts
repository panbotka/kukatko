import { describe, expect, it } from 'vitest'

import {
  applyFilterKey,
  FACET_QUERY_KEYS,
  facetQueryTokens,
  FILTER_KEYS,
  queryFilterTokens,
  suggestFilterKeys,
} from './queryLanguage'

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

  it('offers hidden:, the documented way back to a hidden photo', () => {
    // A flag you cannot list is a flag you cannot undo, so the key has to be
    // reachable from the search box, not only from the docs.
    expect(suggestFilterKeys('hid')?.keys).toContain('hidden')
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

describe('queryFilterTokens', () => {
  it('groups recognised filter tokens by their key', () => {
    const tokens = queryFilterTokens('svatba year:1960-1969 person:Jarmila')
    expect(tokens.get('year')).toEqual(['year:1960-1969'])
    expect(tokens.get('person')).toEqual(['person:Jarmila'])
    expect(tokens.has('svatba')).toBe(false)
  })

  it('keeps every token of a repeated key, as typed', () => {
    const tokens = queryFilterTokens('album:Léto album:"Vánoce 2024"')
    expect(tokens.get('album')).toEqual(['album:Léto', 'album:"Vánoce 2024"'])
  })

  it('lowercases the key but never the value', () => {
    expect(queryFilterTokens('Year:1965').get('year')).toEqual(['Year:1965'])
  })

  it('ignores keys the language does not know', () => {
    // `osoba:` is the Czech spelling of `person:`; the backend degrades it to
    // free text, so it filters nothing and must not be reported as a filter.
    expect(queryFilterTokens('osoba:Jarmila').size).toBe(0)
  })

  it('ignores tokens that are not filter-shaped', () => {
    // A quoted colon, a leading '-' (a negated free-text term, since a key is
    // ASCII letters only), a bare colon and a time all stay free text.
    for (const input of ['"year:1965"', '-year:1965', ':1965', '12:30', 'year']) {
      expect(queryFilterTokens(input).size, input).toBe(0)
    }
  })

  it('honours an escaped colon', () => {
    expect(queryFilterTokens('year\\:1965').size).toBe(0)
  })

  it('finds a filter after a quoted value holding spaces', () => {
    const tokens = queryFilterTokens('camera:"Canon EOS R6" year:1965')
    expect(tokens.get('camera')).toEqual(['camera:"Canon EOS R6"'])
    expect(tokens.get('year')).toEqual(['year:1965'])
  })

  it('returns nothing for an empty query', () => {
    expect(queryFilterTokens('').size).toBe(0)
    expect(queryFilterTokens('   ').size).toBe(0)
  })
})

describe('facetQueryTokens', () => {
  it('joins every alias of a facet in one string', () => {
    const tokens = queryFilterTokens('person:Anna subject:Jarmila')
    expect(facetQueryTokens(tokens, FACET_QUERY_KEYS.person)).toBe('person:Anna subject:Jarmila')
  })

  it('is empty when the query leaves the facet alone', () => {
    const tokens = queryFilterTokens('year:1965')
    expect(facetQueryTokens(tokens, FACET_QUERY_KEYS.album)).toBe('')
  })
})

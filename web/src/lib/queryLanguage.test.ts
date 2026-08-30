import { describe, expect, it } from 'vitest'

import {
  applyFilterKey,
  applyFilterValue,
  FACET_QUERY_KEYS,
  facetQueryTokens,
  FILTER_KEYS,
  type FilterValue,
  matchFilterValues,
  queryFilterTokens,
  quoteFilterValue,
  suggestFilterKeys,
  suggestFilterValues,
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

  it('offers dated:, the worklist of photos with no date', () => {
    // `dated:no` is how the undated pile is found at all; typing "dat" must
    // reach it rather than only the date-ish keys around it.
    expect(suggestFilterKeys('dat')?.keys).toContain('dated')
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

describe('suggestFilterValues', () => {
  it('offers values for a completable key being typed', () => {
    const s = suggestFilterValues('person:an')
    expect(s?.facet).toBe('person')
    expect(s?.prefix).toBe('an')
    // The value starts right after the colon, so the replacement swallows it all.
    expect(s?.start).toBe(7)
  })

  it('offers the whole list right after the colon', () => {
    const s = suggestFilterValues('svatba album:')
    expect(s?.facet).toBe('album')
    expect(s?.prefix).toBe('')
    expect(s?.start).toBe(13)
  })

  it('treats subject: as person:, its alias in the language', () => {
    expect(suggestFilterValues('subject:jar')?.facet).toBe('person')
  })

  it('completes inside an unterminated quote, where spaces need it most', () => {
    const s = suggestFilterValues('album:"Léto 2')
    expect(s?.facet).toBe('album')
    expect(s?.prefix).toBe('Léto 2')
    // Anchored on the colon, so the opening quote is replaced rather than doubled.
    expect(s?.start).toBe(6)
  })

  it('starts a fresh value after an OR separator', () => {
    const s = suggestFilterValues('label:cat|do')
    expect(s?.prefix).toBe('do')
    expect(s?.start).toBe(10)
  })

  it('keeps a leading negation out of the value', () => {
    const s = suggestFilterValues('label:!blu')
    expect(s?.prefix).toBe('blu')
    expect(s?.start).toBe(7)
  })

  it('returns null for keys whose values nothing can propose', () => {
    for (const input of ['iso:10', 'year:196', 'favorite:y', 'camera:Can']) {
      expect(suggestFilterValues(input), input).toBeNull()
    }
  })

  it('returns null once the caret has left the token', () => {
    expect(suggestFilterValues('person:Anna ')).toBeNull()
    expect(suggestFilterValues('album:"Léto 2024" ')).toBeNull()
  })

  it('returns null for free text and an empty input', () => {
    for (const input of ['', '   ', 'svatba', 'per', '-person:Anna']) {
      expect(suggestFilterValues(input), input).toBeNull()
    }
  })

  it('never fires at the same time as a key suggestion', () => {
    for (const input of ['per', 'person:an', 'lab', 'label:']) {
      const both = suggestFilterKeys(input) !== null && suggestFilterValues(input) !== null
      expect(both, input).toBe(false)
    }
  })
})

describe('quoteFilterValue', () => {
  it('leaves a plain value bare', () => {
    expect(quoteFilterValue('Anna')).toBe('Anna')
    expect(quoteFilterValue('Nováková-Anna')).toBe('Nováková-Anna')
  })

  it('quotes a value holding spaces', () => {
    expect(quoteFilterValue('Léto 2024')).toBe('"Léto 2024"')
  })

  it('quotes a value holding an operator character', () => {
    expect(quoteFilterValue('cat|dog')).toBe('"cat|dog"')
    expect(quoteFilterValue('star*')).toBe('"star*"')
    expect(quoteFilterValue('!bang')).toBe('"!bang"')
    expect(quoteFilterValue('-dash')).toBe('"-dash"')
  })

  it('escapes quotes and backslashes inside the quoted form', () => {
    expect(quoteFilterValue('say "hi"')).toBe('"say \\"hi\\""')
    expect(quoteFilterValue('back\\slash')).toBe('"back\\\\slash"')
  })

  it('renders an empty value as an empty quoted string', () => {
    expect(quoteFilterValue('')).toBe('""')
  })
})

describe('applyFilterValue', () => {
  it('completes the value and leaves the caret on a fresh token', () => {
    const input = 'person:an'
    const s = suggestFilterValues(input)
    expect(s).not.toBeNull()
    if (s) {
      expect(applyFilterValue(input, s, 'Anna')).toBe('person:Anna ')
    }
  })

  it('quotes a value with spaces and swallows the opening quote already typed', () => {
    const input = 'svatba album:"Léto 2'
    const s = suggestFilterValues(input)
    expect(s).not.toBeNull()
    if (s) {
      expect(applyFilterValue(input, s, 'Léto 2024')).toBe('svatba album:"Léto 2024" ')
    }
  })

  it('keeps the earlier alternatives of an OR list', () => {
    const input = 'label:cat|do'
    const s = suggestFilterValues(input)
    expect(s).not.toBeNull()
    if (s) {
      expect(applyFilterValue(input, s, 'dog')).toBe('label:cat|dog ')
    }
  })

  it('keeps a negation in front of the completed value', () => {
    const input = 'label:!blu'
    const s = suggestFilterValues(input)
    expect(s).not.toBeNull()
    if (s) {
      expect(applyFilterValue(input, s, 'blurry')).toBe('label:!blurry ')
    }
  })

  it('round-trips through the token scanner as one filter', () => {
    const input = 'album:"Léto 2'
    const s = suggestFilterValues(input)
    expect(s).not.toBeNull()
    if (s) {
      const completed = applyFilterValue(input, s, 'Léto | 2024')
      // Whatever the title holds, the query language must read it back as ONE
      // album filter — the pipe sits inside the quotes, so it is a character of
      // the title rather than an OR between two of them.
      expect(queryFilterTokens(completed).get('album')).toEqual(['album:"Léto | 2024"'])
    }
  })
})

describe('matchFilterValues', () => {
  const values: FilterValue[] = [
    { name: 'Anna', count: 12 },
    { name: 'Anna Marie', count: 40 },
    { name: 'Aneta', count: 3 },
    { name: 'Božena', count: 90 },
    { name: 'Náměstí', count: 5 },
  ]

  it('prefix-matches and ranks by photo count', () => {
    expect(matchFilterValues(values, 'an').map((v) => v.name)).toEqual([
      'Anna Marie',
      'Anna',
      'Aneta',
    ])
  })

  it('ignores case and diacritics on both sides', () => {
    expect(matchFilterValues(values, 'namesti').map((v) => v.name)).toEqual(['Náměstí'])
    expect(matchFilterValues(values, 'BOŽ').map((v) => v.name)).toEqual(['Božena'])
  })

  it('matches everything for an empty prefix, most-used first', () => {
    expect(matchFilterValues(values, '')[0].name).toBe('Božena')
    expect(matchFilterValues(values, '')).toHaveLength(values.length)
  })

  it('matches on the prefix, not anywhere in the name', () => {
    expect(matchFilterValues(values, 'marie')).toEqual([])
  })

  it('keeps only the busiest of two values that fold to the same name', () => {
    const dupes: FilterValue[] = [
      { name: 'Léto', count: 2 },
      { name: 'leto', count: 9 },
    ]
    expect(matchFilterValues(dupes, 'l')).toEqual([{ name: 'leto', count: 9 }])
  })

  it('caps the list at the requested limit', () => {
    expect(matchFilterValues(values, '', 2)).toHaveLength(2)
  })

  it('never mutates the list it was given', () => {
    const original = [...values]
    matchFilterValues(values, '')
    expect(values).toEqual(original)
  })
})

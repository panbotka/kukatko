import { describe, expect, it } from 'vitest'

import {
  MAX_QUERY_SUGGESTIONS,
  applySuggestedKey,
  applySuggestedValue,
  suggestQuery,
} from './querySuggest'
import { type QueryFilterKey } from '../services/search'

/** A small stand-in for the schema the server publishes, in its own order. */
const SCHEMA: QueryFilterKey[] = [
  { key: 'album', kind: 'text' },
  { key: 'alt', kind: 'number' },
  { key: 'archived', kind: 'bool', values: ['yes', 'no'] },
  { key: 'face', kind: 'enum', values: ['new'] },
  { key: 'faces', kind: 'count', values: ['yes', 'no'] },
  { key: 'flag', kind: 'enum', values: ['pick', 'reject', 'eye'] },
  { key: 'label', kind: 'text' },
  { key: 'person', kind: 'text' },
  { key: 'title', kind: 'text' },
  { key: 'type', kind: 'enum', values: ['image', 'video', 'live'] },
]

describe('suggestQuery', () => {
  it('offers the keys sharing the typed prefix', () => {
    const result = suggestQuery('al', SCHEMA)
    expect(result?.mode).toBe('keys')
    expect(result?.mode === 'keys' && result.keys.map((k) => k.key)).toEqual(['album', 'alt'])
  })

  it('still offers a key that is fully typed, so it can be completed to key:', () => {
    const result = suggestQuery('face', SCHEMA)
    expect(result?.mode === 'keys' && result.keys.map((k) => k.key)).toEqual(['face', 'faces'])
  })

  it('leaves the filters already typed alone and only looks at the last token', () => {
    const result = suggestQuery('year:2024 ty', SCHEMA)
    expect(result?.mode === 'keys' && result.keys.map((k) => k.key)).toEqual(['type'])
  })

  it('offers nothing for a word no key starts with', () => {
    expect(suggestQuery('dovolená', SCHEMA)).toBeNull()
  })

  it('offers nothing once the token is finished with a space', () => {
    expect(suggestQuery('type:video ', SCHEMA)).toBeNull()
  })

  it('caps the key list', () => {
    const wide: QueryFilterKey[] = Array.from({ length: 20 }, (_, i) => ({
      key: `a${String(i)}`,
      kind: 'text',
    }))
    const result = suggestQuery('a', wide)
    expect(result?.mode === 'keys' && result.keys).toHaveLength(MAX_QUERY_SUGGESTIONS)
  })

  it('offers an enum key its own words after the colon', () => {
    const result = suggestQuery('type:', SCHEMA)
    expect(result?.mode).toBe('values')
    expect(result?.mode === 'values' && result.values).toEqual(['image', 'video', 'live'])
  })

  it('narrows the words by what has been typed after the colon', () => {
    const result = suggestQuery('flag:re', SCHEMA)
    expect(result?.mode === 'values' && result.values).toEqual(['reject'])
  })

  it('matches the words case-insensitively', () => {
    const result = suggestQuery('type:VID', SCHEMA)
    expect(result?.mode === 'values' && result.values).toEqual(['video'])
  })

  it('offers yes/no for a yes-or-no key', () => {
    const result = suggestQuery('archived:', SCHEMA)
    expect(result?.mode === 'values' && result.values).toEqual(['yes', 'no'])
  })

  it('offers yes/no for a count key, which also takes numbers', () => {
    const result = suggestQuery('faces:', SCHEMA)
    expect(result?.mode === 'values' && result.values).toEqual(['yes', 'no'])
  })

  it('offers nothing for a word the enum does not have', () => {
    expect(suggestQuery('type:zzz', SCHEMA)).toBeNull()
  })

  it('asks for library names once a name-valued key has a prefix', () => {
    const result = suggestQuery('album:vese', SCHEMA)
    expect(result).toEqual({ mode: 'names', key: 'album', facet: 'album', prefix: 'vese' })
  })

  it('treats the subject: alias as person:', () => {
    const result = suggestQuery('subject:an', SCHEMA)
    expect(result?.mode === 'names' && result.facet).toBe('person')
  })

  it('does not ask for names before anything is typed after the colon', () => {
    expect(suggestQuery('album:', SCHEMA)).toBeNull()
  })

  it('completes a name still inside its opening quote', () => {
    const result = suggestQuery('album:"Léto 2', SCHEMA)
    expect(result?.mode === 'names' && result.prefix).toBe('Léto 2')
  })

  it('offers nothing for a key whose values only the reader knows', () => {
    expect(suggestQuery('title:sva', SCHEMA)).toBeNull()
  })

  it('offers nothing for a key the schema does not carry', () => {
    expect(suggestQuery('nonsense:x', SCHEMA)).toBeNull()
  })
})

describe('applySuggestedKey', () => {
  it('replaces the half-typed key and opens the value', () => {
    expect(applySuggestedKey('al', 'album')).toBe('album:')
  })

  it('leaves everything typed before the token untouched', () => {
    expect(applySuggestedKey('svatba year:2024 ty', 'type')).toBe('svatba year:2024 type:')
  })
})

describe('applySuggestedValue', () => {
  it('completes the value and finishes the token with a space', () => {
    expect(applySuggestedValue('type:vid', 'video')).toBe('type:video ')
  })

  it('quotes a name that would otherwise fall apart', () => {
    expect(applySuggestedValue('album:Lé', 'Léto 2024')).toBe('album:"Léto 2024" ')
  })

  it('replaces the opening quote rather than nesting one', () => {
    expect(applySuggestedValue('album:"Léto 2', 'Léto 2024')).toBe('album:"Léto 2024" ')
  })

  it('completes one alternative and leaves the earlier ones alone', () => {
    expect(applySuggestedValue('flag:pick|re', 'reject')).toBe('flag:pick|reject ')
  })

  it('keeps the filters typed before it', () => {
    expect(applySuggestedValue('svatba type:im', 'image')).toBe('svatba type:image ')
  })
})

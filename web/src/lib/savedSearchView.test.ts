import { describe, expect, it } from 'vitest'

import { isSearchParams, savedSearchHref, savedSearchSummary } from './savedSearchView'

describe('isSearchParams', () => {
  it('treats params with a non-empty mode as a search view', () => {
    expect(isSearchParams({ mode: 'hybrid' })).toBe(true)
    expect(isSearchParams({ mode: 'semantic', q: 'cat' })).toBe(true)
  })

  it('treats params without a mode as a library view', () => {
    expect(isSearchParams({ sort: 'oldest', camera: 'Canon' })).toBe(false)
    expect(isSearchParams({})).toBe(false)
    // A library filter query (q) is not a search view without a mode.
    expect(isSearchParams({ q: 'beach' })).toBe(false)
  })
})

describe('savedSearchHref', () => {
  it('routes a library view to the homepage with non-default filters encoded', () => {
    expect(savedSearchHref({ sort: 'oldest', camera: 'Canon' })).toBe('/?sort=oldest&camera=Canon')
  })

  it('routes an all-default library view to the bare homepage', () => {
    expect(savedSearchHref({ sort: 'newest', archived: 'false', camera: '' })).toBe('/')
  })

  it('routes a search view to /search with the query and mode encoded', () => {
    // Encoding order follows the defaults key order: q (a library field) before mode.
    expect(savedSearchHref({ mode: 'semantic', q: 'sunset' })).toBe(
      '/search?q=sunset&mode=semantic',
    )
  })

  it('omits a default mode from the /search URL', () => {
    expect(savedSearchHref({ mode: 'hybrid', q: 'sunset' })).toBe('/search?q=sunset')
  })

  it('ignores unknown/stale keys and fills missing keys from defaults', () => {
    // `bogus` is not a known view key, so it is dropped; sort defaults through.
    expect(savedSearchHref({ camera: 'Nikon', bogus: 'x' })).toBe('/?camera=Nikon')
  })
})

describe('savedSearchSummary', () => {
  it('returns the trimmed query and no badge for a bare query', () => {
    expect(savedSearchSummary({ q: '  beach iso:100  ' })).toEqual({
      query: 'beach iso:100',
      badge: null,
    })
  })

  it('returns an empty query when none was saved', () => {
    expect(savedSearchSummary({}).query).toBe('')
  })

  it('counts the non-default filters, the period as one', () => {
    expect(
      savedSearchSummary({
        q: 'svatba',
        sort: 'oldest',
        mode: 'semantic',
        person: 'su1,su2',
        taken_after: '1960-01-01',
        taken_before: '1969-12-31',
        favorite: 'true',
      }).badge,
    ).toEqual({ kind: 'filters', count: 3 })
  })

  it('ignores values equal to their defaults and unknown keys', () => {
    expect(
      savedSearchSummary({ archived: 'false', sort: 'newest', mode: 'hybrid', bogus: 'x' }).badge,
    ).toBeNull()
  })

  it('falls back to a non-default search mode when nothing filters', () => {
    expect(savedSearchSummary({ q: 'sunset', mode: 'semantic', sort: 'oldest' }).badge).toEqual({
      kind: 'mode',
      mode: 'semantic',
    })
  })

  it('falls back to a non-default sort', () => {
    expect(savedSearchSummary({ q: 'cat', sort: 'oldest' }).badge).toEqual({
      kind: 'sort',
      sort: 'oldest',
    })
  })
})

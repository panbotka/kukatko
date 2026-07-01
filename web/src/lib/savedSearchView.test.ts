import { describe, expect, it } from 'vitest'

import { isSearchParams, savedSearchHref } from './savedSearchView'

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
  it('routes a library view to /library with non-default filters encoded', () => {
    expect(savedSearchHref({ sort: 'oldest', camera: 'Canon' })).toBe(
      '/library?sort=oldest&camera=Canon',
    )
  })

  it('routes an all-default library view to bare /library', () => {
    expect(savedSearchHref({ sort: 'newest', archived: 'false', camera: '' })).toBe('/library')
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
    expect(savedSearchHref({ camera: 'Nikon', bogus: 'x' })).toBe('/library?camera=Nikon')
  })
})

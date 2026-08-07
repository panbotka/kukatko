import { describe, expect, it } from 'vitest'

import { LIBRARY_DEFAULTS } from './libraryView'
import { effectiveSearchMode, searchHref, toMode } from './searchView'

describe('toMode', () => {
  it('keeps a known mode', () => {
    expect(toMode('fulltext')).toBe('fulltext')
    expect(toMode('semantic')).toBe('semantic')
    expect(toMode('hybrid')).toBe('hybrid')
  })

  it('falls back to hybrid for anything else', () => {
    expect(toMode('')).toBe('hybrid')
    expect(toMode('bogus')).toBe('hybrid')
  })
})

describe('effectiveSearchMode', () => {
  it('leaves every mode alone while semantic search is available', () => {
    expect(effectiveSearchMode('hybrid', true)).toBe('hybrid')
    expect(effectiveSearchMode('semantic', true)).toBe('semantic')
    expect(effectiveSearchMode('fulltext', true)).toBe('fulltext')
  })

  it('replaces the embedding-backed modes with full-text when it is not', () => {
    // Both would be answered by full-text anyway — only after waiting out the
    // sidecar timeout first.
    expect(effectiveSearchMode('hybrid', false)).toBe('fulltext')
    expect(effectiveSearchMode('semantic', false)).toBe('fulltext')
  })

  it('leaves full-text alone when it is not, since it never needs the sidecar', () => {
    expect(effectiveSearchMode('fulltext', false)).toBe('fulltext')
  })

  it('is idempotent, so applying it along a call chain is safe', () => {
    expect(effectiveSearchMode(effectiveSearchMode('semantic', false), false)).toBe('fulltext')
    expect(effectiveSearchMode(effectiveSearchMode('semantic', true), true)).toBe('semantic')
  })
})

describe('searchHref', () => {
  it('carries the library filters over as a search', () => {
    // Encoding follows the defaults' key order, not the argument's.
    expect(searchHref({ ...LIBRARY_DEFAULTS, q: 'beach', camera: 'Canon' })).toBe(
      '/search?camera=Canon&q=beach',
    )
  })

  it('omits the default mode, keeping the URL minimal', () => {
    expect(searchHref({ ...LIBRARY_DEFAULTS, q: 'beach' })).toBe('/search?q=beach')
  })

  it('is the bare path for an all-default view', () => {
    expect(searchHref(LIBRARY_DEFAULTS)).toBe('/search')
  })
})

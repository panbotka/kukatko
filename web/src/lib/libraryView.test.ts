import { describe, expect, it } from 'vitest'

import {
  addToFilterList,
  hasActiveFilters,
  joinFilterList,
  LIBRARY_DEFAULTS,
  parseFilterList,
  removeFromFilterList,
  viewToParams,
} from './libraryView'
import { readUrlState, writeUrlState } from './urlState'

describe('filter-list encoding', () => {
  it('parses a comma-joined list, dropping empty segments', () => {
    expect(parseFilterList('')).toEqual([])
    expect(parseFilterList('al_1')).toEqual(['al_1'])
    expect(parseFilterList('al_1,al_2')).toEqual(['al_1', 'al_2'])
    expect(parseFilterList('al_1,,al_2,')).toEqual(['al_1', 'al_2'])
  })

  it('joins UIDs back into the comma-joined form', () => {
    expect(joinFilterList([])).toBe('')
    expect(joinFilterList(['al_1'])).toBe('al_1')
    expect(joinFilterList(['al_1', 'al_2'])).toBe('al_1,al_2')
  })

  it('appends a UID, ignoring empties and duplicates', () => {
    expect(addToFilterList('', 'al_1')).toBe('al_1')
    expect(addToFilterList('al_1', 'al_2')).toBe('al_1,al_2')
    expect(addToFilterList('al_1', 'al_1')).toBe('al_1')
    expect(addToFilterList('al_1', '')).toBe('al_1')
  })

  it('removes a single UID and clears the facet when the last one goes', () => {
    expect(removeFromFilterList('al_1,al_2', 'al_1')).toBe('al_2')
    expect(removeFromFilterList('al_1,al_2', 'al_2')).toBe('al_1')
    expect(removeFromFilterList('al_1', 'al_1')).toBe('')
    expect(removeFromFilterList('al_1,al_2', 'al_missing')).toBe('al_1,al_2')
  })
})

describe('viewToParams multi-value facets', () => {
  it('passes the comma-joined album/label lists through unchanged', () => {
    const params = viewToParams({
      ...LIBRARY_DEFAULTS,
      album: 'al_1,al_2',
      label: 'lb_1,lb_2',
    })
    expect(params.album).toBe('al_1,al_2')
    expect(params.label).toBe('lb_1,lb_2')
  })
})

describe('hasActiveFilters', () => {
  it('treats a non-empty album or label list as an active filter', () => {
    expect(hasActiveFilters({ ...LIBRARY_DEFAULTS, album: 'al_1,al_2' })).toBe(true)
    expect(hasActiveFilters({ ...LIBRARY_DEFAULTS, label: 'lb_1' })).toBe(true)
    expect(hasActiveFilters(LIBRARY_DEFAULTS)).toBe(false)
  })
})

describe('URL round-trip', () => {
  it('restores a multi-album, multi-label selection through the query string', () => {
    const view = { ...LIBRARY_DEFAULTS, album: 'al_1,al_2', label: 'lb_1,lb_2' }
    const params = writeUrlState(view, LIBRARY_DEFAULTS)
    // Comma stays in the single URL key — "Back always works" restores the whole set.
    expect(params.get('album')).toBe('al_1,al_2')
    expect(params.get('label')).toBe('lb_1,lb_2')

    const restored = readUrlState(params, LIBRARY_DEFAULTS)
    expect(parseFilterList(restored.album)).toEqual(['al_1', 'al_2'])
    expect(parseFilterList(restored.label)).toEqual(['lb_1', 'lb_2'])
  })
})

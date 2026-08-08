import { describe, expect, it } from 'vitest'

import {
  addToFilterList,
  hasActiveFilters,
  joinFilterList,
  LIBRARY_DEFAULTS,
  parseFilterList,
  periodOf,
  periodPatch,
  removeFromFilterList,
  viewToParams,
} from './libraryView'
import { ANY_PERIOD } from './period'
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
  it('passes the comma-joined album/label/person lists through unchanged', () => {
    const params = viewToParams({
      ...LIBRARY_DEFAULTS,
      album: 'al_1,al_2',
      label: 'lb_1,lb_2',
      person: 'su_1,su_2',
    })
    expect(params.album).toBe('al_1,al_2')
    expect(params.label).toBe('lb_1,lb_2')
    expect(params.person).toBe('su_1,su_2')
  })

  it('carries the favorites toggle through to the list params', () => {
    expect(viewToParams({ ...LIBRARY_DEFAULTS, favorite: 'true' }).favorite).toBe('true')
    expect(viewToParams(LIBRARY_DEFAULTS).favorite).toBe('')
  })
})

describe('the capture period', () => {
  it('reads the date bounds, dropping a bound the backend would reject', () => {
    expect(periodOf({ ...LIBRARY_DEFAULTS, taken_after: '1960-01-01' })).toEqual({
      from: '1960-01-01',
      to: '',
    })
    expect(periodOf({ ...LIBRARY_DEFAULTS, taken_before: 'loni' })).toEqual(ANY_PERIOD)
  })

  it('folds a legacy year= into the period it always meant', () => {
    // Bookmarks and saved searches minted before the year dropdown became a
    // period control still carry it; they must keep showing what they showed.
    expect(periodOf({ ...LIBRARY_DEFAULTS, year: '1965' })).toEqual({
      from: '1965-01-01',
      to: '1965-12-31',
    })
    expect(periodOf({ ...LIBRARY_DEFAULTS, year: '65' })).toEqual(ANY_PERIOD)
  })

  it('lets the date bounds win over the legacy key, and clears it on any write', () => {
    const view = { ...LIBRARY_DEFAULTS, year: '1965', taken_after: '2019-06-01' }
    expect(periodOf(view)).toEqual({ from: '2019-06-01', to: '' })
    expect(periodPatch(ANY_PERIOD)).toEqual({ taken_after: '', taken_before: '', year: '' })
  })

  it('sends the period as one inclusive range, the last day included', () => {
    const params = viewToParams({ ...LIBRARY_DEFAULTS, year: '1965' })
    expect(params.taken_after).toBe('1965-01-01')
    expect(params.taken_before).toBe('1965-12-31T23:59:59.999999Z')
    expect(viewToParams(LIBRARY_DEFAULTS).taken_before).toBe('')
  })
})

describe('hasActiveFilters', () => {
  it('treats a non-empty album, label or person list as an active filter', () => {
    expect(hasActiveFilters({ ...LIBRARY_DEFAULTS, album: 'al_1,al_2' })).toBe(true)
    expect(hasActiveFilters({ ...LIBRARY_DEFAULTS, label: 'lb_1' })).toBe(true)
    expect(hasActiveFilters({ ...LIBRARY_DEFAULTS, person: 'su_1' })).toBe(true)
    expect(hasActiveFilters(LIBRARY_DEFAULTS)).toBe(false)
  })

  it('treats the favorites toggle as an active filter', () => {
    expect(hasActiveFilters({ ...LIBRARY_DEFAULTS, favorite: 'true' })).toBe(true)
  })

  it('treats a period as an active filter, however it was set', () => {
    expect(hasActiveFilters({ ...LIBRARY_DEFAULTS, taken_after: '1960-01-01' })).toBe(true)
    expect(hasActiveFilters({ ...LIBRARY_DEFAULTS, year: '1965' })).toBe(true)
    // A bound the backend would reject filters nothing, so it is not active.
    expect(hasActiveFilters({ ...LIBRARY_DEFAULTS, taken_before: 'loni' })).toBe(false)
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

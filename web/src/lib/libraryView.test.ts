import { describe, expect, it } from 'vitest'

import {
  addToFilterList,
  hasActiveFilters,
  joinFilterList,
  LIBRARY_DEFAULTS,
  parseFilterList,
  periodOf,
  periodPatch,
  queryWithDated,
  removeFromFilterList,
  UPLOADER_NONE,
  uploaderHref,
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

  it('carries the uploader — a person or the imported group — through to the params', () => {
    expect(viewToParams({ ...LIBRARY_DEFAULTS, uploader: 'us_1' }).uploader).toBe('us_1')
    expect(viewToParams({ ...LIBRARY_DEFAULTS, uploader: UPLOADER_NONE }).uploader).toBe('none')
    expect(viewToParams(LIBRARY_DEFAULTS).uploader).toBe('')
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

  it('treats a picked uploader as an active filter', () => {
    expect(hasActiveFilters({ ...LIBRARY_DEFAULTS, uploader: 'us_1' })).toBe(true)
    expect(hasActiveFilters({ ...LIBRARY_DEFAULTS, uploader: UPLOADER_NONE })).toBe(true)
  })

  it('links to the library filtered by one uploader', () => {
    expect(uploaderHref('us_1')).toBe('/?uploader=us_1')
    expect(uploaderHref(UPLOADER_NONE)).toBe('/?uploader=none')
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

describe('the capture-date filter', () => {
  it('compiles the tri-state into the query language the backend speaks', () => {
    expect(viewToParams({ ...LIBRARY_DEFAULTS, dated: 'true' }).q).toBe('dated:yes')
    expect(viewToParams({ ...LIBRARY_DEFAULTS, dated: 'false' }).q).toBe('dated:no')
    expect(viewToParams(LIBRARY_DEFAULTS).q).toBe('')
  })

  it('appends the token to the reader’s own query rather than replacing it', () => {
    expect(viewToParams({ ...LIBRARY_DEFAULTS, q: 'svatba', dated: 'false' }).q).toBe(
      'svatba dated:no',
    )
  })

  it('leaves a dated: typed into the query in charge', () => {
    // Two contradictory tokens would match no photo at all, and a filter the
    // reader typed is the one they meant.
    expect(queryWithDated({ ...LIBRARY_DEFAULTS, q: 'dated:yes', dated: 'false' })).toBe(
      'dated:yes',
    )
  })

  it('round-trips through the URL query params', () => {
    const params = writeUrlState({ ...LIBRARY_DEFAULTS, dated: 'false' }, LIBRARY_DEFAULTS)
    expect(params.get('dated')).toBe('false')

    const restored = readUrlState(params, LIBRARY_DEFAULTS)
    expect(restored.dated).toBe('false')
    expect(viewToParams(restored).q).toBe('dated:no')

    // At rest it stays out of the URL, like every other default.
    expect(writeUrlState(LIBRARY_DEFAULTS, LIBRARY_DEFAULTS).has('dated')).toBe(false)
  })

  it('counts as an active filter, either way round', () => {
    expect(hasActiveFilters({ ...LIBRARY_DEFAULTS, dated: 'true' })).toBe(true)
    expect(hasActiveFilters({ ...LIBRARY_DEFAULTS, dated: 'false' })).toBe(true)
  })
})

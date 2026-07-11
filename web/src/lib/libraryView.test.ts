import { describe, expect, it } from 'vitest'

import { readUrlState, writeUrlState } from './urlState'
import {
  addUID,
  albumList,
  hasActiveFilters,
  labelList,
  LIBRARY_DEFAULTS,
  type LibraryView,
  removeUID,
  splitUIDs,
  viewToParams,
} from './libraryView'

describe('multi-value UID helpers', () => {
  it('splits and joins a delimiter-encoded field, dropping empties', () => {
    expect(splitUIDs('')).toEqual([])
    expect(splitUIDs('al_1')).toEqual(['al_1'])
    expect(splitUIDs('al_1,al_2')).toEqual(['al_1', 'al_2'])
    expect(splitUIDs('al_1,,al_2,')).toEqual(['al_1', 'al_2'])
  })

  it('appends a UID as a set (no duplicates)', () => {
    expect(addUID('', 'al_1')).toBe('al_1')
    expect(addUID('al_1', 'al_2')).toBe('al_1,al_2')
    expect(addUID('al_1,al_2', 'al_1')).toBe('al_1,al_2')
  })

  it('removes a single UID, leaving the rest', () => {
    expect(removeUID('al_1,al_2', 'al_1')).toBe('al_2')
    expect(removeUID('al_1,al_2', 'al_2')).toBe('al_1')
    expect(removeUID('al_1', 'al_1')).toBe('')
    // Removing an absent UID is a no-op.
    expect(removeUID('al_1', 'al_9')).toBe('al_1')
  })
})

describe('viewToParams album/label scope', () => {
  it('splits the joined fields into UID lists', () => {
    const params = viewToParams({ ...LIBRARY_DEFAULTS, album: 'al_1,al_2', label: 'lb_1' })
    expect(params.album).toEqual(['al_1', 'al_2'])
    expect(params.label).toEqual(['lb_1'])
    expect(albumList({ ...LIBRARY_DEFAULTS, album: 'al_1,al_2' })).toEqual(['al_1', 'al_2'])
    expect(labelList({ ...LIBRARY_DEFAULTS, label: 'lb_1' })).toEqual(['lb_1'])
  })

  it('maps an empty facet to an empty list (no filter)', () => {
    const params = viewToParams(LIBRARY_DEFAULTS)
    expect(params.album).toEqual([])
    expect(params.label).toEqual([])
  })
})

describe('hasActiveFilters with a multi-value facet', () => {
  it('treats a non-empty album/label list as active', () => {
    expect(hasActiveFilters(LIBRARY_DEFAULTS)).toBe(false)
    expect(hasActiveFilters({ ...LIBRARY_DEFAULTS, album: 'al_1,al_2' })).toBe(true)
    expect(hasActiveFilters({ ...LIBRARY_DEFAULTS, label: 'lb_1' })).toBe(true)
  })
})

describe('URL round-trip', () => {
  it('restores a multi-album/label selection from the query string', () => {
    const view: LibraryView = { ...LIBRARY_DEFAULTS, album: 'al_1,al_2', label: 'lb_1,lb_2' }
    const params = writeUrlState(view, LIBRARY_DEFAULTS)
    // The whole selection round-trips through the single query key ("Back works").
    const restored = readUrlState(new URLSearchParams(params.toString()), LIBRARY_DEFAULTS)
    expect(restored.album).toBe('al_1,al_2')
    expect(restored.label).toBe('lb_1,lb_2')
    expect(albumList(restored)).toEqual(['al_1', 'al_2'])
  })
})

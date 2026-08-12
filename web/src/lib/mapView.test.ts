import { describe, expect, it } from 'vitest'

import { activeMapFilterCount, hasActiveMapFilters, MAP_DEFAULTS } from './mapView'

/**
 * The badge on the phone's Filters button is the only thing that says the map is
 * showing a subset once every control is behind a drawer, so what it counts —
 * and what it must not count — is worth pinning down.
 */
describe('activeMapFilterCount', () => {
  it('counts nothing on the default view', () => {
    expect(activeMapFilterCount(MAP_DEFAULTS)).toBe(0)
    expect(hasActiveMapFilters(MAP_DEFAULTS)).toBe(false)
  })

  it('counts each photo filter that differs from its default', () => {
    expect(activeMapFilterCount({ ...MAP_DEFAULTS, taken_after: '2019-06-01' })).toBe(1)
    expect(
      activeMapFilterCount({
        ...MAP_DEFAULTS,
        taken_after: '2019-06-01',
        taken_before: '2019-08-31',
        archived: 'only',
      }),
    ).toBe(3)
  })

  it('counts the filters only a link can set, since a chip for them exists nowhere', () => {
    expect(activeMapFilterCount({ ...MAP_DEFAULTS, album: 'al1', label: 'la1' })).toBe(2)
  })

  it('ignores the mapset and the viewport, which narrow no photos', () => {
    const panned = { ...MAP_DEFAULTS, mapset: 'aerial', lat: '49.8', lng: '15.5', z: '12' }
    expect(activeMapFilterCount(panned)).toBe(0)
    expect(hasActiveMapFilters(panned)).toBe(false)
  })

  it('treats the default archive selector as no filter at all', () => {
    expect(activeMapFilterCount({ ...MAP_DEFAULTS, archived: MAP_DEFAULTS.archived })).toBe(0)
    expect(activeMapFilterCount({ ...MAP_DEFAULTS, archived: 'true' })).toBe(1)
  })
})

import { describe, expect, it } from 'vitest'

import {
  activeMapFilterCount,
  hasActiveMapFilters,
  MAP_DEFAULTS,
  MAP_PANEL_CLOSED,
  MAP_PANEL_HALF,
  panelOpenFromView,
  panelValueForSheet,
  PHOTO_MAP_ZOOM,
  photoMapHref,
  sheetStateFromView,
} from './mapView'
import { readUrlState, writeUrlState } from './urlState'

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

describe('the area panel in the URL', () => {
  it('peeks or rests open by default and round-trips every sheet stop', () => {
    expect(sheetStateFromView(MAP_DEFAULTS)).toBe('peek')
    expect(panelOpenFromView(MAP_DEFAULTS)).toBe(true)
    for (const state of ['peek', 'half', 'full'] as const) {
      const panel = panelValueForSheet(state)
      expect(sheetStateFromView({ panel })).toBe(state)
      // Every stop keeps the desktop column open: only an explicit close closes it.
      expect(panelOpenFromView({ panel })).toBe(true)
    }
    expect(panelValueForSheet('peek')).toBe(MAP_DEFAULTS.panel)
  })

  it('collapses the desktop column only on an explicit close, which a phone reads as a peek', () => {
    expect(panelOpenFromView({ panel: MAP_PANEL_CLOSED })).toBe(false)
    expect(sheetStateFromView({ panel: MAP_PANEL_CLOSED })).toBe('peek')
    expect(sheetStateFromView({ panel: 'bogus' })).toBe('peek')
  })

  it('survives a trip through the query string, defaults left out', () => {
    const view = { ...MAP_DEFAULTS, panel: MAP_PANEL_HALF, sel: '49,16,50,17', photo: 'ph1' }
    const query = writeUrlState(view, MAP_DEFAULTS)
    expect(query.toString()).toBe('panel=half&sel=49%2C16%2C50%2C17&photo=ph1')
    expect(readUrlState(query, MAP_DEFAULTS)).toEqual(view)
  })

  it('never counts the panel, an opened cluster or a highlight as a filter', () => {
    expect(
      activeMapFilterCount({
        ...MAP_DEFAULTS,
        panel: 'full',
        sel: '1,2,3,4',
        photo: 'p',
        area: 'a',
      }),
    ).toBe(0)
  })
})

describe('photoMapHref', () => {
  it('centres the map on the photo at street level with its tile highlighted', () => {
    expect(photoMapHref({ uid: 'ph1', lat: 49.123456, lng: 16.654321 })).toBe(
      `/map?lat=49.12346&lng=16.65432&z=${String(PHOTO_MAP_ZOOM)}&photo=ph1`,
    )
  })

  it('brings the archive along for an archived photo', () => {
    expect(photoMapHref({ uid: 'ph1', lat: 1, lng: 2, archived: true })).toContain('archived=true')
  })
})

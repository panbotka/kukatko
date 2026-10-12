import { describe, expect, it } from 'vitest'

import { mapDetailQuery } from './mapDetail'
import { MAP_DEFAULTS } from './mapView'

describe('mapDetailQuery', () => {
  const box = { south: 49.1, west: 16.5, north: 49.4, east: 16.9 }

  it('carries the area and the map filters into the viewer, nothing else', () => {
    const query = new URLSearchParams(
      mapDetailQuery(
        {
          ...MAP_DEFAULTS,
          taken_after: '2019-01-01',
          archived: 'true',
          label: 'lb1',
          // Map-only state never reaches the viewer.
          mapset: 'aerial',
          panel: 'full',
          lat: '49.2',
        },
        box,
      ),
    )
    expect(Object.fromEntries(query)).toEqual({
      bbox: '49.10000,16.50000,49.40000,16.90000',
      taken_after: '2019-01-01',
      archived: 'true',
      label: 'lb1',
    })
  })

  it('sanitises a tampered archive selector', () => {
    const query = new URLSearchParams(mapDetailQuery({ ...MAP_DEFAULTS, archived: 'bogus' }, box))
    expect(query.has('archived')).toBe(false)
  })
})

import L from 'leaflet'
import { describe, expect, it, vi } from 'vitest'

import { clusterCanSplit, lazyTileIcon, toGeoBounds } from './mapPhotoLayer'

/** A stand-in for markercluster's private cluster state. */
interface FakeCluster {
  _childClusters: FakeCluster[]
  _childCount: number
  _zoom: number
}

function cluster(zoom: number, count: number, children: FakeCluster[] = []): FakeCluster {
  return { _zoom: zoom, _childCount: count, _childClusters: children }
}

const asCluster = (c: FakeCluster) => c as unknown as L.MarkerCluster

describe('clusterCanSplit', () => {
  it('splits when a deeper zoom divides the photos', () => {
    const top = cluster(10, 5, [cluster(11, 3), cluster(11, 2)])
    expect(clusterCanSplit(asCluster(top), 20)).toBe(true)
  })

  it('splits when the chain of single children ends before the deepest zoom', () => {
    const top = cluster(10, 5, [cluster(11, 5, [cluster(12, 5)])])
    expect(clusterCanSplit(asCluster(top), 20)).toBe(true)
  })

  it('cannot split photos that stay together down to the deepest zoom', () => {
    const top = cluster(18, 4, [cluster(19, 4, [cluster(20, 4)])])
    expect(clusterCanSplit(asCluster(top), 20)).toBe(false)
  })
})

describe('lazyTileIcon', () => {
  it('builds the tile on the first draw only, and reuses it on every redraw', () => {
    const build = vi.fn(() => {
      const tile = document.createElement('div')
      tile.className = 'kk-map-tile'
      return tile
    })
    const icon = lazyTileIcon(build)
    expect(build).not.toHaveBeenCalled()

    const first = icon.createIcon()
    expect(first).toHaveClass('kk-map-tile-icon')
    const tile = first.querySelector('.kk-map-tile')
    expect(tile).not.toBeNull()

    const second = icon.createIcon()
    expect(build).toHaveBeenCalledTimes(1)
    expect(second.querySelector('.kk-map-tile')).toBe(tile)
  })
})

describe('toGeoBounds', () => {
  it('reads Leaflet bounds as a plain box', () => {
    const bounds = L.latLngBounds([49, 16], [50, 17])
    expect(toGeoBounds(bounds)).toEqual({ south: 49, west: 16, north: 50, east: 17 })
  })
})

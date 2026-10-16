import { describe, expect, it, vi } from 'vitest'

import { projectPathFor } from '../project/paths'
import { type MapFeature } from '../services/map'

import { buildPopupElement } from './mapPopup'

const FEATURE: MapFeature = {
  type: 'Feature',
  geometry: { type: 'Point', coordinates: [14.42, 50.08] },
  properties: { uid: 'ph1', title: 'Prague', media_type: 'image', thumb: '/t/ph1' },
}

describe('buildPopupElement', () => {
  it("links the photo inside the caller's project", () => {
    const link = buildPopupElement(FEATURE, vi.fn(), 'alt', (path) => projectPathFor('v', path))

    expect(link.getAttribute('href')).toBe('/p/v/photos/ph1')
  })

  it("keeps the photo page's own path without a project resolver", () => {
    const link = buildPopupElement(FEATURE, vi.fn(), 'alt')

    expect(link.getAttribute('href')).toBe('/photos/ph1')
  })

  it('replaces a thumbnail that fails to load with the "no preview" stand-in', () => {
    const link = buildPopupElement(FEATURE, vi.fn(), 'alt', undefined, 'Náhled není dostupný')
    const img = link.querySelector('img')
    expect(img).not.toBeNull()

    img?.dispatchEvent(new Event('error'))

    expect(link.querySelector('img')).toBeNull()
    expect(link.querySelector('[role="img"]')?.getAttribute('aria-label')).toBe(
      'Náhled není dostupný',
    )
  })
})

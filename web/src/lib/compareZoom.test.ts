import { describe, expect, it } from 'vitest'

import {
  type Box,
  IDENTITY_VIEW,
  MAX_SCALE,
  MIN_SCALE,
  clampView,
  isZoomed,
  panBy,
  viewTransform,
  zoomAt,
  zoomCentre,
} from './compareZoom'

const box: Box = { width: 800, height: 600 }

describe('clampView', () => {
  it('refuses to zoom out past fit-to-pane', () => {
    expect(clampView({ scale: 0.2, x: 0, y: 0 }, box).scale).toBe(MIN_SCALE)
  })

  it('caps the scale at the maximum', () => {
    expect(clampView({ scale: 99, x: 0, y: 0 }, box).scale).toBe(MAX_SCALE)
  })

  it('allows no pan at all while at fit-to-pane', () => {
    const view = clampView({ scale: 1, x: 500, y: 500 }, box)
    expect(view).toEqual({ scale: 1, x: 0, y: 0 })
  })

  it('bounds the pan so the image cannot be dragged out of the pane', () => {
    // At 2×, half the magnified image hangs outside the pane, so the pan bound is
    // half the pane on each axis.
    const view = clampView({ scale: 2, x: 9999, y: -9999 }, box)
    expect(view.x).toBe(400)
    expect(view.y).toBe(-300)
  })

  it('survives a pane that has not been measured yet', () => {
    expect(clampView({ scale: 2, x: 10, y: 10 }, { width: 0, height: 0 })).toEqual({
      scale: 2,
      x: 0,
      y: 0,
    })
  })
})

describe('zoomAt', () => {
  it('keeps the point under the cursor under the cursor', () => {
    // Zoom about a point 100px right of centre. That image point must still render
    // at the same place, which is what makes wheel-zoom track the detail you are
    // looking at rather than the middle of the photo.
    const px = box.width / 2 + 100
    const py = box.height / 2
    const before = IDENTITY_VIEW
    const after = zoomAt(before, 2, px, py, box)

    const rendered = (view: { scale: number; x: number }, imagePoint: number) =>
      view.x + view.scale * imagePoint
    // The image point currently under the cursor, relative to the pane centre.
    const imagePoint = (px - box.width / 2 - before.x) / before.scale
    expect(rendered(after, imagePoint)).toBeCloseTo(px - box.width / 2, 5)
  })

  it('is a no-op at the maximum scale', () => {
    const maxed = { scale: MAX_SCALE, x: 0, y: 0 }
    expect(zoomAt(maxed, 2, 400, 300, box).scale).toBe(MAX_SCALE)
  })

  it('returns to fit-to-pane when zooming out far enough, with the pan reset', () => {
    const zoomed = zoomAt(IDENTITY_VIEW, 4, 100, 100, box)
    expect(isZoomed(zoomed)).toBe(true)
    const out = zoomAt(zoomed, 0.01, 100, 100, box)
    expect(out).toEqual(IDENTITY_VIEW)
  })
})

describe('zoomCentre', () => {
  it('zooms about the pane centre without panning', () => {
    const view = zoomCentre(IDENTITY_VIEW, 2, box)
    expect(view.scale).toBe(2)
    expect(view.x).toBe(0)
    expect(view.y).toBe(0)
  })
})

describe('panBy', () => {
  it('moves the view by the delta while zoomed', () => {
    const view = panBy({ scale: 2, x: 0, y: 0 }, 30, -20, box)
    expect(view).toEqual({ scale: 2, x: 30, y: -20 })
  })

  it('does not move at fit-to-pane, where there is nothing to pan to', () => {
    expect(panBy(IDENTITY_VIEW, 50, 50, box)).toEqual(IDENTITY_VIEW)
  })

  it('clamps a drag that would push the image out of the pane', () => {
    expect(panBy({ scale: 2, x: 390, y: 0 }, 100, 0, box).x).toBe(400)
  })
})

describe('viewTransform', () => {
  it('renders the CSS transform both panes share', () => {
    expect(viewTransform({ scale: 2, x: 10, y: -5 })).toBe('translate(10px, -5px) scale(2)')
  })
})

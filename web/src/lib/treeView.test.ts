import { describe, expect, it } from 'vitest'

import {
  centreOn,
  clampScale,
  fitView,
  initialView,
  MAX_SCALE,
  MIN_SCALE,
  panBy,
  type TreeView,
  zoomAt,
} from './treeView'

describe('clampScale', () => {
  it('keeps a scale inside the readable range', () => {
    expect(clampScale(0.5)).toBe(0.5)
    expect(clampScale(0.0001)).toBe(MIN_SCALE)
    expect(clampScale(50)).toBe(MAX_SCALE)
  })
})

describe('fitView', () => {
  it('scales a wide drawing down and centres it', () => {
    const view = fitView({ width: 2000, height: 500 }, { width: 1000, height: 800 })

    expect(view.scale).toBe(0.5)
    expect(view.x).toBe(0)
    // 500 * 0.5 = 250 tall in a 800 tall viewport.
    expect(view.y).toBe(275)
  })

  it('never magnifies a small family to fill the screen', () => {
    const view = fitView({ width: 200, height: 100 }, { width: 1000, height: 800 })

    expect(view.scale).toBe(1)
    expect(view.x).toBe(400)
  })

  it('keeps a drawing taller than the viewport pinned to the top', () => {
    const view = fitView({ width: 100, height: 4000 }, { width: 1000, height: 800 })

    expect(view.y).toBe(0)
  })

  it('is the identity before the stage has been measured', () => {
    expect(fitView({ width: 100, height: 100 }, { width: 0, height: 0 })).toEqual({
      scale: 1,
      x: 0,
      y: 0,
    })
  })
})

describe('initialView', () => {
  // A three-generation family on a 390 px phone: far too wide to fit at 1.
  const family = { width: 1200, height: 420 }
  const phone = { width: 390, height: 600 }

  it('opens a family that does not fit at natural size, centred on the focal person', () => {
    const focus = { x: 300, y: 200 }

    const view = initialView(family, phone, focus)

    expect(view.scale).toBe(1)
    // The focal card's centre lands in the middle of the stage.
    expect(focus.x * view.scale + view.x).toBe(phone.width / 2)
    expect(focus.y * view.scale + view.y).toBe(phone.height / 2)
  })

  it('keeps the fitted view for a family that fits at natural size', () => {
    const desktop = { width: 1440, height: 640 }

    expect(initialView(family, desktop, { x: 300, y: 200 })).toEqual(fitView(family, desktop))
  })

  it('falls back to the fitted view when the drawing is about nobody', () => {
    expect(initialView(family, phone, null)).toEqual(fitView(family, phone))
  })
})

describe('zoomAt', () => {
  const view: TreeView = { scale: 1, x: 0, y: 0 }

  it('keeps the point under the cursor where it was', () => {
    const zoomed = zoomAt(view, 2, 300, 200)

    // The layout point that was at (300, 200) must still be there.
    const before = { x: (300 - view.x) / view.scale, y: (200 - view.y) / view.scale }
    expect(before.x * zoomed.scale + zoomed.x).toBeCloseTo(300)
    expect(before.y * zoomed.scale + zoomed.y).toBeCloseTo(200)
  })

  it('does not build up a reserve at the limit', () => {
    const far = zoomAt({ scale: MAX_SCALE, x: 0, y: 0 }, 4, 100, 100)

    expect(far.scale).toBe(MAX_SCALE)
    expect(far.x).toBe(0)
  })
})

describe('panBy', () => {
  it('drags without touching the scale', () => {
    expect(panBy({ scale: 0.5, x: 10, y: 20 }, -5, 7)).toEqual({ scale: 0.5, x: 5, y: 27 })
  })
})

describe('centreOn', () => {
  it('puts a layout point in the middle of the viewport', () => {
    const view = centreOn(
      { scale: 0.5, x: 0, y: 0 },
      { x: 400, y: 100 },
      { width: 800, height: 600 },
    )

    expect(400 * view.scale + view.x).toBe(400)
    expect(100 * view.scale + view.y).toBe(300)
  })
})

import { describe, expect, it } from 'vitest'

import {
  clampPan,
  clampScale,
  DOUBLE_TAP_SCALE,
  isDoubleTap,
  MAX_SCALE,
  MIN_SCALE,
  pinchScale,
  swipeAction,
  SWIPE_THRESHOLD,
  swipeVerdict,
  touchDistance,
  touchMidpoint,
  VERDICT_HINT_THRESHOLD,
  VERDICT_THRESHOLD,
} from './gestures'

describe('swipeAction', () => {
  it('pages to next on a leftward horizontal swipe past the threshold', () => {
    expect(swipeAction(-(SWIPE_THRESHOLD + 1), 0)).toBe('next')
    expect(swipeAction(-120, 10)).toBe('next')
  })

  it('pages to prev on a rightward horizontal swipe past the threshold', () => {
    expect(swipeAction(SWIPE_THRESHOLD + 1, 0)).toBe('prev')
    expect(swipeAction(120, -10)).toBe('prev')
  })

  it('ignores a drag shorter than the threshold', () => {
    expect(swipeAction(SWIPE_THRESHOLD - 1, 0)).toBeNull()
    expect(swipeAction(-(SWIPE_THRESHOLD - 1), 0)).toBeNull()
    expect(swipeAction(0, 0)).toBeNull()
  })

  it('ignores a mostly-vertical drag so scrolling still works', () => {
    // Long, but more vertical than horizontal.
    expect(swipeAction(60, 200)).toBeNull()
    expect(swipeAction(-60, -200)).toBeNull()
    // Exactly diagonal must not page either.
    expect(swipeAction(80, 80)).toBeNull()
  })

  it('honours a custom threshold', () => {
    expect(swipeAction(30, 0, { threshold: 20 })).toBe('prev')
    expect(swipeAction(30, 0, { threshold: 40 })).toBeNull()
  })

  it('honours a custom dominance ratio', () => {
    // |dx| must exceed ratio × |dy|.
    expect(swipeAction(100, 40, { ratio: 2 })).toBe('prev')
    expect(swipeAction(100, 60, { ratio: 2 })).toBeNull()
  })
})

describe('touch geometry', () => {
  it('measures the distance between two points', () => {
    expect(touchDistance({ x: 0, y: 0 }, { x: 3, y: 4 })).toBe(5)
  })

  it('finds the midpoint of two points', () => {
    expect(touchMidpoint({ x: 0, y: 0 }, { x: 4, y: 10 })).toEqual({ x: 2, y: 5 })
  })
})

describe('clampScale', () => {
  it('clamps below the minimum and above the maximum', () => {
    expect(clampScale(0.2)).toBe(MIN_SCALE)
    expect(clampScale(99)).toBe(MAX_SCALE)
    expect(clampScale(2)).toBe(2)
  })
})

describe('pinchScale', () => {
  it('scales the starting zoom by the finger-spread ratio', () => {
    expect(pinchScale(1, 100, 200)).toBe(2)
    expect(pinchScale(2, 200, 100)).toBe(1)
  })

  it('clamps into the allowed zoom range', () => {
    expect(pinchScale(1, 100, 1000)).toBe(MAX_SCALE)
    expect(pinchScale(2, 1000, 100)).toBe(MIN_SCALE)
  })

  it('leaves the scale unchanged when the start distance is degenerate', () => {
    expect(pinchScale(1.5, 0, 200)).toBe(1.5)
  })
})

describe('isDoubleTap', () => {
  it('is true for two quick, near-stationary taps', () => {
    expect(isDoubleTap(0, 0)).toBe(true)
    expect(isDoubleTap(200, 10)).toBe(true)
  })

  it('is false when the taps are too far apart in time or space', () => {
    expect(isDoubleTap(500, 0)).toBe(false)
    expect(isDoubleTap(100, 100)).toBe(false)
    // A negative gap (no previous tap seeded) never registers.
    expect(isDoubleTap(-1, 0)).toBe(false)
  })

  it('offers a sensible double-tap zoom target below the maximum', () => {
    expect(DOUBLE_TAP_SCALE).toBeGreaterThan(MIN_SCALE)
    expect(DOUBLE_TAP_SCALE).toBeLessThanOrEqual(MAX_SCALE)
  })
})

describe('clampPan', () => {
  it('allows no travel when the image is not zoomed', () => {
    expect(clampPan({ x: 50, y: 50 }, 1, 1000, 800)).toEqual({ x: 0, y: 0 })
  })

  it('bounds travel to half the overflow at the current scale', () => {
    // scale 2 over a 1000×800 viewport → overflow 1000×800, half is 500×400.
    expect(clampPan({ x: 900, y: -900 }, 2, 1000, 800)).toEqual({ x: 500, y: -400 })
    expect(clampPan({ x: 100, y: -100 }, 2, 1000, 800)).toEqual({ x: 100, y: -100 })
  })
})

describe('swipeVerdict', () => {
  it('gives the three answers the arrow keys give, in the same directions', () => {
    expect(swipeVerdict(VERDICT_THRESHOLD + 10, 0)).toBe('yes')
    expect(swipeVerdict(-(VERDICT_THRESHOLD + 10), 0)).toBe('no')
    expect(swipeVerdict(0, VERDICT_THRESHOLD + 10)).toBe('skip')
  })

  it('decides nothing below the threshold, so a nudge is not an answer', () => {
    expect(swipeVerdict(VERDICT_THRESHOLD - 1, 0)).toBeNull()
    expect(swipeVerdict(0, VERDICT_THRESHOLD - 1)).toBeNull()
  })

  it('resolves a diagonal by the axis it is mostly going, not by which crossed first', () => {
    // Mostly sideways with a bit of drift is still the sideways answer.
    expect(swipeVerdict(90, 40)).toBe('yes')
    // Mostly downwards with a bit of drift is the skip.
    expect(swipeVerdict(40, 90)).toBe('skip')
  })

  it('never answers on an upward drag', () => {
    // There is no fourth verdict to give it, and swiping up is how a phone is
    // scrolled — deciding something there would be an answer nobody meant.
    expect(swipeVerdict(0, -200)).toBeNull()
    expect(swipeVerdict(20, -200)).toBeNull()
  })

  it('shows the verdict long before it fires one', () => {
    // The hint threshold exists so the card can name the answer while the drag
    // can still be steered — or taken back.
    expect(VERDICT_HINT_THRESHOLD).toBeLessThan(VERDICT_THRESHOLD)
    const midDrag = VERDICT_HINT_THRESHOLD + 1
    expect(swipeVerdict(midDrag, 0, { threshold: VERDICT_HINT_THRESHOLD })).toBe('yes')
    expect(swipeVerdict(midDrag, 0)).toBeNull()
  })
})

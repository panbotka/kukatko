import { afterEach, describe, expect, it, vi } from 'vitest'

import { COARSE_POINTER_QUERY, enableTwoFingerPan, prefersTouchGestures } from './mapGestures'

/** A touch point in the shape a `TouchEvent`'s touch list carries. */
function touch(x: number, y: number): Touch {
  return { clientX: x, clientY: y } as Touch
}

/**
 * Dispatches a touch event with the given active touches. jsdom has no
 * `TouchEvent` constructor, so build an `Event` and hang the touch list on it —
 * that is all the handlers read.
 */
function fireTouch(target: EventTarget, type: string, touches: Touch[]): void {
  const event = new Event(type, { bubbles: true })
  Object.defineProperty(event, 'touches', { value: touches })
  target.dispatchEvent(event)
}

/** A stub of the slice of the Leaflet map the gesture handling drives. */
function stubMap() {
  return { dragging: { enable: vi.fn(), disable: vi.fn() } }
}

const originalMatchMedia = window.matchMedia

afterEach(() => {
  window.matchMedia = originalMatchMedia
})

/** Points `window.matchMedia` at a fixed answer for the coarse-pointer query. */
function pinPointer(coarse: boolean): void {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: coarse && query === COARSE_POINTER_QUERY,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
}

describe('prefersTouchGestures', () => {
  it('is on when the primary pointer is coarse', () => {
    pinPointer(true)
    expect(prefersTouchGestures()).toBe(true)
  })

  it('is off on a fine pointer, so a mouse keeps drag-to-pan', () => {
    pinPointer(false)
    expect(prefersTouchGestures()).toBe(false)
  })

  it('is off when the environment answers nothing usable', () => {
    // jsdom exposes `matchMedia` but a stub may return undefined; reading
    // `.matches` off that would throw and take the whole map down.
    window.matchMedia = vi.fn().mockReturnValue(undefined)
    expect(prefersTouchGestures()).toBe(false)
  })
})

describe('enableTwoFingerPan', () => {
  it('leaves a one-finger touch to the page scroller', () => {
    const map = stubMap()
    const container = document.createElement('div')
    enableTwoFingerPan(map, container, vi.fn())

    fireTouch(container, 'touchstart', [touch(100, 100)])

    expect(map.dragging.disable).toHaveBeenCalled()
    expect(map.dragging.enable).not.toHaveBeenCalled()
  })

  it('hands the map to a two-finger gesture and takes it back when it ends', () => {
    const map = stubMap()
    const container = document.createElement('div')
    enableTwoFingerPan(map, container, vi.fn())

    fireTouch(container, 'touchstart', [touch(100, 100), touch(160, 140)])
    expect(map.dragging.enable).toHaveBeenCalledTimes(1)

    // One finger lifted: the gesture is over, so the page gets its swipes back
    // before the next one starts.
    fireTouch(container, 'touchend', [touch(100, 100)])
    expect(map.dragging.disable).toHaveBeenCalled()
  })

  it('reports a one-finger drag once, after it has travelled far enough', () => {
    const map = stubMap()
    const container = document.createElement('div')
    const onOneFingerDrag = vi.fn()
    enableTwoFingerPan(map, container, onOneFingerDrag)

    fireTouch(container, 'touchstart', [touch(100, 100)])
    // Still within the tap threshold — the user may just be tapping a marker.
    fireTouch(container, 'touchmove', [touch(103, 102)])
    expect(onOneFingerDrag).not.toHaveBeenCalled()

    fireTouch(container, 'touchmove', [touch(100, 150)])
    fireTouch(container, 'touchmove', [touch(100, 200)])
    expect(onOneFingerDrag).toHaveBeenCalledTimes(1)
  })

  it('says nothing when the drag started on the picker pin', () => {
    const map = stubMap()
    const container = document.createElement('div')
    const pin = document.createElement('div')
    pin.className = 'leaflet-marker-icon leaflet-marker-draggable'
    container.appendChild(pin)
    const onOneFingerDrag = vi.fn()
    enableTwoFingerPan(map, container, onOneFingerDrag)

    // Dragging the pin IS a one-finger gesture that works; "use two fingers"
    // would be wrong advice at exactly the wrong moment.
    fireTouch(pin, 'touchstart', [touch(100, 100)])
    fireTouch(pin, 'touchmove', [touch(100, 200)])

    expect(onOneFingerDrag).not.toHaveBeenCalled()
  })

  it('says nothing about a two-finger gesture', () => {
    const map = stubMap()
    const container = document.createElement('div')
    const onOneFingerDrag = vi.fn()
    enableTwoFingerPan(map, container, onOneFingerDrag)

    fireTouch(container, 'touchstart', [touch(100, 100), touch(160, 140)])
    fireTouch(container, 'touchmove', [touch(80, 200), touch(200, 240)])

    expect(onOneFingerDrag).not.toHaveBeenCalled()
  })

  it('detaches every listener it attached', () => {
    const map = stubMap()
    const container = document.createElement('div')
    const onOneFingerDrag = vi.fn()
    const detach = enableTwoFingerPan(map, container, onOneFingerDrag)

    detach()
    fireTouch(container, 'touchstart', [touch(100, 100)])
    fireTouch(container, 'touchmove', [touch(100, 200)])

    expect(map.dragging.disable).not.toHaveBeenCalled()
    expect(onOneFingerDrag).not.toHaveBeenCalled()
  })
})

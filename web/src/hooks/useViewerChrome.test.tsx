import { act, fireEvent, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { DEFAULT_IDLE_MS } from './useAutoHideChrome'
import { CHROME_HINT_MS, FIRST_RUN_HOLD_MS, useViewerChrome } from './useViewerChrome'

beforeEach(() => {
  window.localStorage.clear()
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
  window.localStorage.clear()
})

/** Runs the clock forward inside `act`, so the state it produces is flushed. */
function tick(ms: number): void {
  act(() => {
    vi.advanceTimersByTime(ms)
  })
}

/**
 * The whole first-run wait, in its two steps: the hold, and only then the
 * ordinary idle countdown — which is exactly how the hook behaves, the timer
 * being scheduled when the hold is released rather than alongside it.
 */
function firstHide(): void {
  tick(FIRST_RUN_HOLD_MS)
  tick(DEFAULT_IDLE_MS)
}

describe('useViewerChrome on a touch device', () => {
  it('holds the chrome up past the ordinary idle the first time', () => {
    const { result } = renderHook(() => useViewerChrome({ paused: false, touch: true }))

    // The moment the bar would normally have gone: a first-time reader is still
    // looking at it, which is the point.
    tick(DEFAULT_IDLE_MS)
    expect(result.current.visible).toBe(true)

    tick(FIRST_RUN_HOLD_MS - DEFAULT_IDLE_MS)
    expect(result.current.visible).toBe(true)

    // The hold is over; from here the ordinary countdown runs.
    tick(DEFAULT_IDLE_MS)
    expect(result.current.visible).toBe(false)
  })

  it('explains the vanished chrome once it has vanished, then stops', () => {
    const { result } = renderHook(() => useViewerChrome({ paused: false, touch: true }))

    // Nothing to explain while the controls are still on screen.
    expect(result.current.hintVisible).toBe(false)

    firstHide()
    expect(result.current.visible).toBe(false)
    expect(result.current.hintVisible).toBe(true)

    tick(CHROME_HINT_MS)
    expect(result.current.hintVisible).toBe(false)
  })

  it('drops the hint the instant the chrome is back', () => {
    const { result } = renderHook(() => useViewerChrome({ paused: false, touch: true }))

    firstHide()
    expect(result.current.hintVisible).toBe(true)

    // The reader tapped: the controls are back, so a sentence about how to bring
    // them back is only clutter.
    act(() => {
      result.current.wake()
    })
    expect(result.current.visible).toBe(true)
    expect(result.current.hintVisible).toBe(false)
  })

  it('never runs again on the same device', () => {
    const first = renderHook(() => useViewerChrome({ paused: false, touch: true }))
    firstHide()
    expect(first.result.current.hintVisible).toBe(true)
    first.unmount()

    // The fiftieth photograph: no hold, no hint, just the ordinary idle.
    const again = renderHook(() => useViewerChrome({ paused: false, touch: true }))
    tick(DEFAULT_IDLE_MS)
    expect(again.result.current.visible).toBe(false)
    expect(again.result.current.hintVisible).toBe(false)
  })

  it('says nothing while the drawer pins the chrome open', () => {
    const { result } = renderHook(() => useViewerChrome({ paused: true, touch: true }))

    firstHide()
    tick(CHROME_HINT_MS)
    expect(result.current.visible).toBe(true)
    expect(result.current.hintVisible).toBe(false)
  })
})

describe('useViewerChrome on a touch device, with a keyboard', () => {
  /** Mounts one chrome control and one control outside it, like the viewer. */
  function mountControls(): { inside: HTMLButtonElement; outside: HTMLButtonElement } {
    document.body.innerHTML =
      '<div class="kk-viewer__chrome"><button id="inside">in</button></div>' +
      '<button id="outside">out</button>'
    const inside = document.getElementById('inside')
    const outside = document.getElementById('outside')
    if (!(inside instanceof HTMLButtonElement) || !(outside instanceof HTMLButtonElement)) {
      throw new Error('controls not mounted')
    }
    return { inside, outside }
  }

  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('stays up while keyboard focus is inside the chrome', () => {
    window.localStorage.setItem('kukatko.viewer.chromeHintSeen', 'true')
    const { inside, outside } = mountControls()
    const { result } = renderHook(() => useViewerChrome({ paused: false, touch: true }))

    // Tab into the bar: a focused control may not melt away under its ring.
    act(() => {
      fireEvent.keyDown(window, { key: 'Tab' })
      inside.focus()
    })
    tick(DEFAULT_IDLE_MS * 3)
    expect(result.current.visible).toBe(true)

    // Focus leaves the chrome: the ordinary idle takes over again.
    act(() => {
      outside.focus()
    })
    tick(DEFAULT_IDLE_MS)
    expect(result.current.visible).toBe(false)
  })

  it('does not pin for the focus a tap leaves behind', () => {
    // Some mobile browsers focus a tapped button; pinning for that would stop the
    // chrome ever fading after the first star given.
    window.localStorage.setItem('kukatko.viewer.chromeHintSeen', 'true')
    const { inside } = mountControls()
    const { result } = renderHook(() => useViewerChrome({ paused: false, touch: true }))

    act(() => {
      fireEvent.pointerDown(inside)
      inside.focus()
    })
    tick(DEFAULT_IDLE_MS)
    expect(result.current.visible).toBe(false)
  })
})

describe('useViewerChrome with a mouse', () => {
  it('never hides the chrome and stays silent', () => {
    const { result } = renderHook(() => useViewerChrome({ paused: false, touch: false }))

    // A pointer resting on the page is no reason to take the controls away — so
    // there is no idle, no hold and nothing to teach.
    tick(FIRST_RUN_HOLD_MS + DEFAULT_IDLE_MS * 4)
    expect(result.current.visible).toBe(true)

    tick(CHROME_HINT_MS)
    expect(result.current.hintVisible).toBe(false)
  })

  it('leaves the hint owed, so the same account still gets it on a phone', () => {
    renderHook(() => useViewerChrome({ paused: false, touch: false }))
    tick(DEFAULT_IDLE_MS + CHROME_HINT_MS)

    expect(window.localStorage.getItem('kukatko.viewer.chromeHintSeen')).toBeNull()
  })
})

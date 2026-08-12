import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { CHROME_IDLE_MS, useIdleChrome } from './useIdleChrome'

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

/** Runs the clock past the idle delay. */
function idle(ms = CHROME_IDLE_MS): void {
  act(() => {
    vi.advanceTimersByTime(ms)
  })
}

describe('useIdleChrome', () => {
  it('starts visible and hides itself once the delay passes', () => {
    const { result } = renderHook(() => useIdleChrome())

    // The controls state their case first — a bar nobody ever saw cannot be
    // asked for.
    expect(result.current.visible).toBe(true)

    idle()
    expect(result.current.visible).toBe(false)
  })

  it('does not hide before the delay is up', () => {
    const { result } = renderHook(() => useIdleChrome())

    idle(CHROME_IDLE_MS - 1)
    expect(result.current.visible).toBe(true)
  })

  it('comes back on wake, and stays for a fresh delay each time', () => {
    const { result } = renderHook(() => useIdleChrome())
    idle()

    act(() => {
      result.current.wake()
    })
    expect(result.current.visible).toBe(true)

    // Waking again half-way through must not let the first countdown finish:
    // a mouse that keeps moving keeps the controls.
    idle(CHROME_IDLE_MS / 2)
    act(() => {
      result.current.wake()
    })
    idle(CHROME_IDLE_MS / 2)
    expect(result.current.visible).toBe(true)

    idle()
    expect(result.current.visible).toBe(false)
  })

  it('toggles both ways, and a toggle back on starts the countdown again', () => {
    const { result } = renderHook(() => useIdleChrome())

    act(() => {
      result.current.toggle()
    })
    expect(result.current.visible).toBe(false)

    act(() => {
      result.current.toggle()
    })
    expect(result.current.visible).toBe(true)

    idle()
    expect(result.current.visible).toBe(false)
  })

  it('stays hidden once toggled away, however long nothing happens', () => {
    const { result } = renderHook(() => useIdleChrome())

    act(() => {
      result.current.toggle()
    })
    idle(CHROME_IDLE_MS * 10)

    expect(result.current.visible).toBe(false)
  })

  it('is pinned while held, and starts a fresh countdown when released', () => {
    const { result, rerender } = renderHook(({ held }) => useIdleChrome({ held }), {
      initialProps: { held: true },
    })

    idle(CHROME_IDLE_MS * 3)
    expect(result.current.visible).toBe(true)

    rerender({ held: false })
    // The countdown runs from the release, not from before the hold.
    idle(CHROME_IDLE_MS - 1)
    expect(result.current.visible).toBe(true)
    idle(1)
    expect(result.current.visible).toBe(false)
  })

  it('shows a hidden chrome again the moment it is held', () => {
    const { result, rerender } = renderHook(({ held }) => useIdleChrome({ held }), {
      initialProps: { held: false },
    })
    idle()
    expect(result.current.visible).toBe(false)

    // Focus landing inside the bar (or a panel opening) has to reveal it —
    // otherwise the reader would be using something they cannot see.
    rerender({ held: true })
    expect(result.current.visible).toBe(true)
  })

  it('honours a custom delay', () => {
    const { result } = renderHook(() => useIdleChrome({ delayMs: 100 }))

    idle(99)
    expect(result.current.visible).toBe(true)
    idle(1)
    expect(result.current.visible).toBe(false)
  })

  it('leaves no timer behind when it unmounts', () => {
    const { unmount } = renderHook(() => useIdleChrome())
    unmount()

    expect(vi.getTimerCount()).toBe(0)
  })
})

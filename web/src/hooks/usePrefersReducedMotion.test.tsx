import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { usePrefersReducedMotion } from './usePrefersReducedMotion'

/** Listeners registered by the hook on the fake media query, keyed by event name. */
type Listener = (event: MediaQueryListEvent) => void

/**
 * Installs a controllable `window.matchMedia` and returns a `flip` helper that
 * fires a `change` event the way a browser would when the OS setting is toggled.
 */
function stubMatchMedia(initial: boolean) {
  const listeners: Listener[] = []
  const query = {
    matches: initial,
    media: '(prefers-reduced-motion: reduce)',
    onchange: null,
    addEventListener: (type: string, listener: Listener) => {
      if (type === 'change') {
        listeners.push(listener)
      }
    },
    removeEventListener: (type: string, listener: Listener) => {
      const at = type === 'change' ? listeners.indexOf(listener) : -1
      if (at >= 0) {
        listeners.splice(at, 1)
      }
    },
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }
  const matchMedia = vi.fn().mockReturnValue(query)
  vi.stubGlobal('matchMedia', matchMedia)

  return {
    matchMedia,
    listenerCount: () => listeners.length,
    flip(matches: boolean) {
      query.matches = matches
      act(() => {
        for (const listener of [...listeners]) {
          listener({ matches } as MediaQueryListEvent)
        }
      })
    },
  }
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('usePrefersReducedMotion', () => {
  it('reports the preference on mount', () => {
    stubMatchMedia(true)

    expect(renderHook(() => usePrefersReducedMotion()).result.current).toBe(true)
  })

  it('reports no preference when the user has not asked for reduced motion', () => {
    stubMatchMedia(false)

    expect(renderHook(() => usePrefersReducedMotion()).result.current).toBe(false)
  })

  it('follows the preference when it changes while the app is open', () => {
    const media = stubMatchMedia(false)
    const { result } = renderHook(() => usePrefersReducedMotion())

    media.flip(true)
    expect(result.current).toBe(true)

    media.flip(false)
    expect(result.current).toBe(false)
  })

  it('unsubscribes on unmount', () => {
    const media = stubMatchMedia(false)
    const { unmount } = renderHook(() => usePrefersReducedMotion())
    expect(media.listenerCount()).toBe(1)

    unmount()
    expect(media.listenerCount()).toBe(0)
  })

  it('reports no preference where matchMedia is unavailable', () => {
    vi.stubGlobal('matchMedia', undefined)

    expect(renderHook(() => usePrefersReducedMotion()).result.current).toBe(false)
  })
})

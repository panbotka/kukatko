import { describe, expect, it, vi } from 'vitest'

import {
  MORPH_NAME,
  MORPH_SETTLE_TIMEOUT_MS,
  morphStarter,
  viewTransitionStarter,
} from './viewTransition'

/** A stand-in `ViewTransition`, since jsdom implements none of the API. */
function handle() {
  return { finished: Promise.resolve() }
}

describe('viewTransitionStarter', () => {
  it('gives up where the browser does not implement the API', () => {
    expect(viewTransitionStarter({})).toBeNull()
  })

  it('gives up on a missing document rather than throwing', () => {
    expect(viewTransitionStarter(null)).toBeNull()
    expect(viewTransitionStarter(undefined)).toBeNull()
  })

  it('gives up when the property is there but is not callable', () => {
    expect(viewTransitionStarter({ startViewTransition: 'yes' })).toBeNull()
  })

  it('calls the API with the update, bound to its own document', () => {
    const startViewTransition = vi.fn(handle)
    const host = { startViewTransition }
    const start = viewTransitionStarter(host)

    const update = () => undefined
    start?.(update)

    expect(startViewTransition).toHaveBeenCalledWith(update)
    expect(startViewTransition.mock.contexts[0]).toBe(host)
  })
})

describe('morphStarter', () => {
  it('runs the morph where the API is available and motion is not reduced', () => {
    expect(morphStarter({ startViewTransition: vi.fn(handle) }, false)).not.toBeNull()
  })

  it('refuses to morph when the reader asked for reduced motion', () => {
    expect(morphStarter({ startViewTransition: vi.fn(handle) }, true)).toBeNull()
  })

  it('refuses to morph where the API is missing, reduced motion or not', () => {
    expect(morphStarter({}, false)).toBeNull()
    expect(morphStarter({}, true)).toBeNull()
  })
})

describe('the morph constants', () => {
  // The name is consumed from CSS, not from TS, so nothing else would notice a
  // rename; `styles/viewTransition.test.ts` pins the stylesheet to this value.
  it('names the pair the stylesheet animates', () => {
    expect(MORPH_NAME).toBe('kk-morph')
  })

  // Long enough to cover a route change plus a render, short enough that a pop
  // the browser never delivers does not read as a hang.
  it('gives a deferred navigation a bounded time to land', () => {
    expect(MORPH_SETTLE_TIMEOUT_MS).toBeGreaterThan(200)
    expect(MORPH_SETTLE_TIMEOUT_MS).toBeLessThanOrEqual(1000)
  })
})

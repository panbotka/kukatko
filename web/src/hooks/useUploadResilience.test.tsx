import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  RESUME_BASE_DELAY_MS,
  RESUME_MAX_DELAY_MS,
  resumeDelay,
  type UploadResilienceOptions,
  useUploadResilience,
} from './useUploadResilience'

/** A stand-in for the browser's WakeLockSentinel. */
interface FakeSentinel {
  released: boolean
  release: () => Promise<void>
}

let visibility: DocumentVisibilityState = 'visible'
let onLine = true
let sentinels: FakeSentinel[] = []
const request = vi.fn((): Promise<FakeSentinel> => {
  const sentinel: FakeSentinel = {
    released: false,
    release: vi.fn(() => {
      sentinel.released = true
      return Promise.resolve()
    }),
  }
  sentinels.push(sentinel)
  return Promise.resolve(sentinel)
})

/** Flips the page's visibility and tells the listeners, as the browser does. */
function setVisibility(next: DocumentVisibilityState) {
  visibility = next
  if (next === 'hidden') {
    // The browser drops a held lock on every hide.
    for (const sentinel of sentinels) {
      sentinel.released = true
    }
  }
  document.dispatchEvent(new Event('visibilitychange'))
}

/** Flips the connectivity and fires the matching window event. */
function setOnline(next: boolean) {
  onLine = next
  window.dispatchEvent(new Event(next ? 'online' : 'offline'))
}

/** Lets the wake-lock promises settle. */
async function flush() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })
}

function renderResilience(initial: Partial<UploadResilienceOptions> = {}) {
  const onResume = vi.fn()
  const props: UploadResilienceOptions = {
    active: true,
    interrupted: false,
    online: true,
    onResume,
    ...initial,
  }
  const view = renderHook(
    (p: UploadResilienceOptions) => {
      useUploadResilience(p)
    },
    { initialProps: props },
  )
  return {
    onResume,
    rerender: (changes: Partial<UploadResilienceOptions>) => {
      Object.assign(props, changes)
      view.rerender({ ...props })
    },
    unmount: view.unmount,
  }
}

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true })
  visibility = 'visible'
  onLine = true
  sentinels = []
  request.mockClear()
  Object.defineProperty(document, 'visibilityState', {
    configurable: true,
    get: () => visibility,
  })
  Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => onLine })
  Object.defineProperty(navigator, 'wakeLock', { configurable: true, value: { request } })
})

afterEach(() => {
  vi.useRealTimers()
  Reflect.deleteProperty(navigator, 'wakeLock')
  Reflect.deleteProperty(navigator, 'onLine')
  Reflect.deleteProperty(document, 'visibilityState')
})

describe('useUploadResilience — wake lock', () => {
  it('keeps the screen awake while active and lets go when the batch ends', async () => {
    const view = renderResilience()
    await flush()
    expect(request).toHaveBeenCalledWith('screen')
    expect(sentinels).toHaveLength(1)

    view.rerender({ active: false })
    await flush()
    expect(sentinels[0].release).toHaveBeenCalled()
  })

  it('takes the lock again when the page comes back to the foreground', async () => {
    renderResilience()
    await flush()
    act(() => {
      setVisibility('hidden')
    })
    await flush()
    expect(request).toHaveBeenCalledTimes(1)

    act(() => {
      setVisibility('visible')
    })
    await flush()
    expect(request).toHaveBeenCalledTimes(2)
  })

  it('takes no lock while idle', async () => {
    renderResilience({ active: false })
    await flush()
    expect(request).not.toHaveBeenCalled()
  })

  it('degrades silently without the API or when the browser refuses it', async () => {
    Reflect.deleteProperty(navigator, 'wakeLock')
    const first = renderResilience()
    await flush()
    first.unmount()

    Object.defineProperty(navigator, 'wakeLock', {
      configurable: true,
      value: { request: () => Promise.reject(new DOMException('no', 'NotAllowedError')) },
    })
    const second = renderResilience()
    await flush()
    second.unmount()
  })
})

describe('useUploadResilience — resuming', () => {
  it('resumes interrupted files when the page returns to the foreground', () => {
    const view = renderResilience({ interrupted: true })
    act(() => {
      setVisibility('hidden')
    })
    expect(view.onResume).not.toHaveBeenCalled()
    act(() => {
      setVisibility('visible')
    })
    expect(view.onResume).toHaveBeenCalledTimes(1)
  })

  it('resumes when the network comes back, and not while it is away', () => {
    const view = renderResilience({ interrupted: true, online: false })
    onLine = false
    act(() => {
      vi.advanceTimersByTime(RESUME_MAX_DELAY_MS * 2)
    })
    expect(view.onResume).not.toHaveBeenCalled()

    act(() => {
      setOnline(true)
    })
    expect(view.onResume).toHaveBeenCalledTimes(1)
  })

  it('does nothing on return when nothing was interrupted', () => {
    const view = renderResilience()
    act(() => {
      setVisibility('visible')
      setOnline(true)
      vi.advanceTimersByTime(RESUME_MAX_DELAY_MS * 2)
    })
    expect(view.onResume).not.toHaveBeenCalled()
  })

  it('retries a blip on its own, backing off while it keeps failing', () => {
    const view = renderResilience({ interrupted: true })
    act(() => {
      vi.advanceTimersByTime(RESUME_BASE_DELAY_MS - 200)
    })
    expect(view.onResume).not.toHaveBeenCalled()
    act(() => {
      vi.advanceTimersByTime(200)
    })
    expect(view.onResume).toHaveBeenCalledTimes(1)

    // The retry went back to the queue and failed again: the next wait is longer.
    view.rerender({ interrupted: false })
    view.rerender({ interrupted: true })
    act(() => {
      vi.advanceTimersByTime(RESUME_BASE_DELAY_MS)
    })
    expect(view.onResume).toHaveBeenCalledTimes(1)
    act(() => {
      vi.advanceTimersByTime(RESUME_BASE_DELAY_MS)
    })
    expect(view.onResume).toHaveBeenCalledTimes(2)
  })

  it('caps the backoff', () => {
    expect(resumeDelay(0)).toBe(RESUME_BASE_DELAY_MS)
    expect(resumeDelay(1)).toBe(RESUME_BASE_DELAY_MS * 2)
    expect(resumeDelay(20)).toBe(RESUME_MAX_DELAY_MS)
  })
})

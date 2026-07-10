import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useImagePreloader } from './useImagePreloader'

/**
 * A stand-in for `HTMLImageElement`: jsdom neither fetches images nor implements
 * `decode()`, so the test drives each load to its outcome by hand.
 */
class StubImage {
  static readonly created = new Map<string, StubImage>()

  src = ''
  decoding = 'auto'
  onload: (() => void) | null = null
  onerror: (() => void) | null = null

  private settle: { ready: () => void; failed: () => void } | null = null

  decode(): Promise<void> {
    StubImage.created.set(this.src, this)
    return new Promise<void>((resolve, reject) => {
      this.settle = {
        ready: resolve,
        failed: () => {
          reject(new Error('decode failed'))
        },
      }
    })
  }

  removeAttribute(name: string): void {
    if (name === 'src') {
      this.src = ''
    }
  }

  /** Resolves this image's pending `decode()`. */
  finish(outcome: 'ready' | 'failed'): void {
    this.settle?.[outcome]()
  }
}

/** The stub created for `url`; fails loudly rather than silently no-op'ing. */
function imageFor(url: string): StubImage {
  const img = StubImage.created.get(url)
  if (img === undefined) {
    throw new Error(`no image was preloaded for ${url}`)
  }
  return img
}

/** Settles `url`'s decode and lets the resulting state update flush. */
async function settle(url: string, outcome: 'ready' | 'failed'): Promise<void> {
  await act(async () => {
    imageFor(url).finish(outcome)
    await Promise.resolve()
  })
}

beforeEach(() => {
  StubImage.created.clear()
  vi.stubGlobal('Image', StubImage)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useImagePreloader', () => {
  it('reports a primed image as pending until it has decoded', async () => {
    const { result } = renderHook(() => useImagePreloader())

    act(() => {
      result.current.prime(['/a', '/b'])
    })
    expect(result.current.statusOf('/a')).toBe('pending')
    expect(result.current.statusOf('/b')).toBe('pending')

    await settle('/a', 'ready')
    expect(result.current.statusOf('/a')).toBe('ready')
    expect(result.current.statusOf('/b')).toBe('pending')
  })

  it('reports an image that cannot load as an error, not as forever pending', async () => {
    const { result } = renderHook(() => useImagePreloader())

    act(() => {
      result.current.prime(['/a'])
    })
    await settle('/a', 'failed')
    expect(result.current.statusOf('/a')).toBe('error')
  })

  it('treats a URL outside the window as pending', () => {
    const { result } = renderHook(() => useImagePreloader())

    act(() => {
      result.current.prime(['/a'])
    })
    expect(result.current.statusOf('/elsewhere')).toBe('pending')
  })

  it('keeps an image that stays in the window and does not re-request it', async () => {
    const { result } = renderHook(() => useImagePreloader())

    act(() => {
      result.current.prime(['/a', '/b'])
    })
    await settle('/a', 'ready')

    act(() => {
      result.current.prime(['/a', '/b', '/c'])
    })
    expect(result.current.statusOf('/a')).toBe('ready')
    // `/a` was never rebuilt: its element still holds the original source.
    expect(imageFor('/a').src).toBe('/a')
  })

  it('releases images that leave the window, and ignores their late decode', async () => {
    const { result } = renderHook(() => useImagePreloader())

    act(() => {
      result.current.prime(['/a'])
    })
    const dropped = imageFor('/a')

    act(() => {
      result.current.prime(['/b'])
    })
    expect(dropped.src).toBe('')

    // The abandoned download finishing must not resurrect a status for it.
    await act(async () => {
      dropped.finish('ready')
      await Promise.resolve()
    })
    expect(result.current.statusOf('/a')).toBe('pending')
    expect(result.current.statusOf('/b')).toBe('pending')
  })

  it('releases the whole window on unmount, so a long show does not grow', () => {
    const { result, unmount } = renderHook(() => useImagePreloader())

    act(() => {
      result.current.prime(['/a', '/b'])
    })
    const images = [imageFor('/a'), imageFor('/b')]

    unmount()
    for (const img of images) {
      expect(img.src).toBe('')
    }
  })
})

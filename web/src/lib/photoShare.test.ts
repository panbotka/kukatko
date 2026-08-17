import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  canSharePhotoFiles,
  isShareAbort,
  SHARE_MAX_BYTES,
  SHARE_MAX_FILES,
  type ShareManifestFile,
  splitShareBatches,
} from './photoShare'

/** A manifest entry of the given size, named after its index. */
function entry(index: number, size = 1024): ShareManifestFile {
  return { uid: `p${index}`, name: `p${index}.jpg`, mime: 'image/jpeg', size, preview: false }
}

/** `count` entries of `size` bytes each. */
function entries(count: number, size = 1024): ShareManifestFile[] {
  return Array.from({ length: count }, (_v, i) => entry(i, size))
}

/**
 * Installs a `navigator.share`/`canShare` pair for the duration of one test and
 * returns the undo. jsdom implements neither, so the undo simply removes them again
 * — which is also the state a desktop Firefox is in.
 */
function stubShare(canShare?: (data: { files: File[] }) => boolean) {
  Object.defineProperty(navigator, 'share', { value: vi.fn(), configurable: true })
  if (canShare === undefined) {
    Reflect.deleteProperty(navigator, 'canShare')
  } else {
    Object.defineProperty(navigator, 'canShare', { value: canShare, configurable: true })
  }
  return () => {
    Reflect.deleteProperty(navigator, 'share')
    Reflect.deleteProperty(navigator, 'canShare')
  }
}

let restore: (() => void) | null = null
afterEach(() => {
  restore?.()
  restore = null
})

describe('splitShareBatches', () => {
  it('keeps a selection that fits in one batch whole', () => {
    const batches = splitShareBatches(entries(SHARE_MAX_FILES))

    expect(batches).toHaveLength(1)
    expect(batches[0]).toHaveLength(SHARE_MAX_FILES)
  })

  it('splits on the file count and never loses a photo', () => {
    const selection = entries(SHARE_MAX_FILES * 2 + 3)

    const batches = splitShareBatches(selection)

    expect(batches.map((b) => b.length)).toEqual([SHARE_MAX_FILES, SHARE_MAX_FILES, 3])
    expect(batches.flat().map((f) => f.uid)).toEqual(selection.map((f) => f.uid))
  })

  it('splits on the byte budget before the file count', () => {
    // Four files of 100 MB: the third would put the batch over 300 MB.
    const batches = splitShareBatches(entries(4, 100 * 1024 * 1024))

    expect(batches.map((b) => b.length)).toEqual([3, 1])
  })

  it('gives a file bigger than the whole budget a batch of its own', () => {
    const huge = { ...entry(0, SHARE_MAX_BYTES * 2), name: 'huge.mp4' }

    const batches = splitShareBatches([entry(1), huge, entry(2)])

    // Never truncated: the oversized clip is still shared, alone.
    expect(batches.map((b) => b.map((f) => f.name))).toEqual([['p1.jpg'], ['huge.mp4'], ['p2.jpg']])
  })

  it('yields no batches for an empty selection', () => {
    expect(splitShareBatches([])).toEqual([])
  })
})

describe('canSharePhotoFiles', () => {
  it('is false in a browser with no canShare at all', () => {
    restore = stubShare(undefined)

    expect(canSharePhotoFiles()).toBe(false)
  })

  it('is false where canShare refuses files (desktop Chrome on Linux)', () => {
    restore = stubShare(() => false)

    expect(canSharePhotoFiles()).toBe(false)
  })

  it('is true where a probe file is accepted, and is asked with a real file', () => {
    const canShare = vi.fn((data: { files: File[] }) => data.files.length === 1)
    restore = stubShare(canShare)

    expect(canSharePhotoFiles()).toBe(true)
    const probe = canShare.mock.calls[0][0].files[0]
    expect(probe).toBeInstanceOf(File)
    expect(probe.type).toBe('image/jpeg')
  })

  it('is false when the probe itself throws', () => {
    restore = stubShare(() => {
      throw new TypeError('illegal invocation')
    })

    expect(canSharePhotoFiles()).toBe(false)
  })
})

describe('isShareAbort', () => {
  it('recognises the cancelled share sheet', () => {
    expect(isShareAbort(new DOMException('cancelled', 'AbortError'))).toBe(true)
  })

  it('recognises an AbortError that is not a DOMException', () => {
    const error = new Error('aborted')
    error.name = 'AbortError'

    expect(isShareAbort(error)).toBe(true)
  })

  it('does not swallow a real failure', () => {
    expect(isShareAbort(new Error('network down'))).toBe(false)
    expect(isShareAbort(new DOMException('nope', 'NotAllowedError'))).toBe(false)
    expect(isShareAbort(null)).toBe(false)
    expect(isShareAbort('AbortError')).toBe(false)
  })
})

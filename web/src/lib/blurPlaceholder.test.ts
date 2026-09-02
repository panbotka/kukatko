import { beforeEach, describe, expect, it } from 'vitest'

import { STUB_CANVAS_DATA_URL, stubBlurCanvas } from '../test/canvas'

import {
  BLUR_PLACEHOLDER_CACHE_LIMIT,
  blurPlaceholderUrl,
  clearBlurPlaceholderCache,
} from './blurPlaceholder'

/** A real BlurHash of a real photograph (the woltapp reference string). */
const HASH = 'LEHV6nWB2yk8pyo0adR*.7kCMdnj'

/** The base83 alphabet, so a test can mint distinct hashes of the right length. */
const BASE83 = '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz#$%*+,-.:;=?@[]^_{|}~'

/**
 * `count` distinct, well-formed hashes: the reference string with its first AC
 * component varied. Only the coefficients change, so every one stays exactly as
 * long as the component count in its first character promises.
 */
function distinctHashes(count: number): string[] {
  const hashes: string[] = []
  for (let i = 0; i < count; i += 1) {
    const first = BASE83[i % BASE83.length] ?? '0'
    const second = BASE83[Math.floor(i / BASE83.length) % BASE83.length] ?? '0'
    hashes.push(`${HASH.slice(0, 6)}${first}${second}${HASH.slice(8)}`)
  }
  return hashes
}

describe('blurPlaceholderUrl', () => {
  beforeEach(() => {
    clearBlurPlaceholderCache()
  })

  it('decodes a hash into a data URL to paint', () => {
    stubBlurCanvas()

    expect(blurPlaceholderUrl(HASH)).toBe(STUB_CANVAS_DATA_URL)
  })

  it('has nothing to paint for a photo that carries no hash', () => {
    const getContext = stubBlurCanvas()

    expect(blurPlaceholderUrl(undefined)).toBeUndefined()
    expect(blurPlaceholderUrl('')).toBeUndefined()
    // Not even a canvas is created: the caller's neutral surface simply stays.
    expect(getContext).not.toHaveBeenCalled()
  })

  it('has nothing to paint for a malformed hash, and does not retry it', () => {
    const getContext = stubBlurCanvas()

    // Too short to be a hash at all, and the right length for another component
    // count than its first character declares.
    expect(blurPlaceholderUrl('nope')).toBeUndefined()
    expect(blurPlaceholderUrl(HASH.slice(0, -1))).toBeUndefined()
    expect(blurPlaceholderUrl('nope')).toBeUndefined()
    expect(getContext).not.toHaveBeenCalled()
  })

  it('has nothing to paint where there is no 2D canvas', () => {
    // What jsdom does on its own, and what a browser does for a canvas it will
    // not back: hand out no context at all.
    stubBlurCanvas().mockReturnValue(null)

    expect(blurPlaceholderUrl(HASH)).toBeUndefined()
  })

  it('decodes each hash once, however often it is asked for', () => {
    const getContext = stubBlurCanvas()

    const first = blurPlaceholderUrl(HASH)
    const second = blurPlaceholderUrl(HASH)
    const third = blurPlaceholderUrl(HASH)

    expect(second).toBe(first)
    expect(third).toBe(first)
    expect(getContext).toHaveBeenCalledTimes(1)
  })

  it('remembers that a hash could not be painted, instead of decoding it again', () => {
    const getContext = stubBlurCanvas()
    getContext.mockReturnValue(null)

    expect(blurPlaceholderUrl(HASH)).toBeUndefined()
    expect(blurPlaceholderUrl(HASH)).toBeUndefined()
    expect(getContext).toHaveBeenCalledTimes(1)
  })

  it('forgets the oldest placeholders rather than growing without bound', () => {
    const getContext = stubBlurCanvas()
    const hashes = distinctHashes(BLUR_PLACEHOLDER_CACHE_LIMIT + 1)

    for (const hash of hashes) {
      expect(blurPlaceholderUrl(hash)).toBe(STUB_CANVAS_DATA_URL)
    }
    expect(getContext).toHaveBeenCalledTimes(hashes.length)

    // The one scrolled past longest ago has been dropped and is decoded again;
    // the one asked for most recently is still remembered.
    const oldest = hashes[0] ?? ''
    const newest = hashes.at(-1) ?? ''
    expect(blurPlaceholderUrl(oldest)).toBe(STUB_CANVAS_DATA_URL)
    expect(getContext).toHaveBeenCalledTimes(hashes.length + 1)
    expect(blurPlaceholderUrl(newest)).toBe(STUB_CANVAS_DATA_URL)
    expect(getContext).toHaveBeenCalledTimes(hashes.length + 1)
  })
})

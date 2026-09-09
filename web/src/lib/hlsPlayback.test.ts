import { describe, expect, it, vi } from 'vitest'

import { HLS_MIME_TYPE, nativeHlsSupported, videoDelivery } from './hlsPlayback'

/** Makes every `<video>` in the document answer `answer` to `canPlayType`. */
function stubCanPlayType(answer: CanPlayTypeResult): void {
  vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue(answer)
}

describe('nativeHlsSupported', () => {
  it('believes a browser that answers anything but the empty string', () => {
    stubCanPlayType('maybe')
    expect(nativeHlsSupported()).toBe(true)

    stubCanPlayType('probably')
    expect(nativeHlsSupported()).toBe(true)
  })

  it('reads the empty answer as "no"', () => {
    stubCanPlayType('')
    expect(nativeHlsSupported()).toBe(false)
  })

  it('asks about the playlist MIME type', () => {
    const canPlayType = vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('')
    nativeHlsSupported()
    expect(canPlayType).toHaveBeenCalledWith(HLS_MIME_TYPE)
  })
})

describe('videoDelivery', () => {
  it('plays the original file when the photo has no rendition', () => {
    expect(videoDelivery(false, false)).toBe('progressive')
    // Even a browser that could stream has nothing to stream here.
    expect(videoDelivery(false, true)).toBe('progressive')
  })

  it('leaves a browser that plays HLS itself to do it', () => {
    expect(videoDelivery(true, true)).toBe('native')
  })

  it('reaches for the library only when the browser cannot', () => {
    expect(videoDelivery(true, false)).toBe('library')
  })
})

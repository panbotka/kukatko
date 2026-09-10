import { describe, expect, it } from 'vitest'

import { batchMedia } from './batchMedia'

/** A picked file, named and typed the way a picker hands it over. */
function file(name: string, type = ''): { name: string; type: string } {
  return { name, type }
}

describe('batchMedia', () => {
  it('counts a stills-only batch as photos', () => {
    expect(batchMedia([file('a.jpg'), file('b.png')])).toEqual({
      photos: 2,
      videos: 0,
      kind: 'photos',
    })
  })

  it('counts a clips-only batch as videos', () => {
    expect(batchMedia([file('a.mp4'), file('b.mov')])).toEqual({
      photos: 0,
      videos: 2,
      kind: 'videos',
    })
  })

  it('counts a mixed batch as both, each half on its own', () => {
    expect(batchMedia([file('a.jpg'), file('b.jpg'), file('c.jpg'), file('d.mov')])).toEqual({
      photos: 3,
      videos: 1,
      kind: 'mixed',
    })
  })

  it('keeps HEIC and RAW with the photographs, unpaintable as they are', () => {
    // `previewKind` says `none` for these — a browser cannot decode them — but
    // they are stills, and calling them videos would be the same lie inverted.
    expect(batchMedia([file('a.heic'), file('b.nef')])).toEqual({
      photos: 2,
      videos: 0,
      kind: 'photos',
    })
  })

  it('falls back to the MIME type for a file whose name has no extension', () => {
    expect(batchMedia([file('clip', 'video/mp4'), file('shot', 'image/jpeg')])).toEqual({
      photos: 1,
      videos: 1,
      kind: 'mixed',
    })
  })

  it('calls an empty batch photos — there is nothing to name', () => {
    expect(batchMedia([])).toEqual({ photos: 0, videos: 0, kind: 'photos' })
  })
})

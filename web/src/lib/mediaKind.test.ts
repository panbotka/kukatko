import { describe, expect, it } from 'vitest'

import { type Photo } from '../services/photos'

import { isPlayableClip, isVideo } from './mediaKind'

/** A catalogue row of the given media type; nothing else here is read. */
function photo(mediaType?: string): Photo {
  return {
    uid: 'p1',
    file_hash: 'h',
    file_name: 'p.jpg',
    file_size: 1,
    file_mime: 'image/jpeg',
    file_width: 1,
    file_height: 1,
    taken_at_source: 'exif',
    title: '',
    description: '',
    camera_make: '',
    camera_model: '',
    lens_model: '',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    media_type: mediaType,
    thumb_url: '/api/v1/photos/p1/thumb/tile_500',
    download_url: '/api/v1/photos/p1/download?original=true',
  }
}

describe('mediaKind', () => {
  it('calls only a video a video', () => {
    expect(isVideo(photo('video'))).toBe(true)
    expect(isVideo(photo('live'))).toBe(false)
    expect(isVideo(photo('image'))).toBe(false)
  })

  it('counts a live photo among the items with something to play', () => {
    expect(isPlayableClip(photo('video'))).toBe(true)
    expect(isPlayableClip(photo('live'))).toBe(true)
    expect(isPlayableClip(photo('image'))).toBe(false)
  })

  it('reads a row with no media type as a plain image', () => {
    expect(isVideo(photo())).toBe(false)
    expect(isPlayableClip(photo())).toBe(false)
  })
})

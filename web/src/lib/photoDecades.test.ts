import { describe, expect, it } from 'vitest'

import { type Photo } from '../services/photos'

import { decadeAnchorId, formatDecade, groupPhotosByDecade } from './photoDecades'

/** A minimal photo carrying only what the grouping reads: a uid and a date. */
function photo(uid: string, takenAt?: string): Photo {
  return {
    uid,
    file_hash: uid,
    file_name: `${uid}.jpg`,
    file_size: 1,
    file_mime: 'image/jpeg',
    file_width: 1,
    file_height: 1,
    taken_at: takenAt,
    taken_at_source: 'exif',
    thumb_url: '',
    download_url: '',
    title: '',
    description: '',
    camera_make: '',
    camera_model: '',
    lens_model: '',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

/** The uids of each section, for compact assertions. */
function shape(sections: ReturnType<typeof groupPhotosByDecade>) {
  return sections.map((section) => ({
    decade: section.decade,
    uids: section.photos.map((p) => p.uid),
  }))
}

describe('groupPhotosByDecade', () => {
  it('splits a newest-first gallery into decades, in that order', () => {
    const sections = groupPhotosByDecade([
      photo('a', '1968-05-01T00:00:00Z'),
      photo('b', '1962-01-01T00:00:00Z'),
      photo('c', '1959-12-31T00:00:00Z'),
      photo('d', '1950-01-01T00:00:00Z'),
    ])
    expect(shape(sections)).toEqual([
      { decade: 1960, uids: ['a', 'b'] },
      { decade: 1950, uids: ['c', 'd'] },
    ])
  })

  it('gives the undated photos a section of their own rather than dropping them', () => {
    const sections = groupPhotosByDecade([photo('a', '1968-05-01T00:00:00Z'), photo('b')])
    expect(shape(sections)).toEqual([
      { decade: 1960, uids: ['a'] },
      { decade: null, uids: ['b'] },
    ])
  })

  it('keeps every decade exactly once even if the input is out of order', () => {
    // The endpoint never does this, but a decade appearing twice in the
    // navigation would be a bug the reader has to make sense of.
    const sections = groupPhotosByDecade([
      photo('a', '1968-05-01T00:00:00Z'),
      photo('b', '1955-01-01T00:00:00Z'),
      photo('c', '1961-01-01T00:00:00Z'),
    ])
    expect(shape(sections)).toEqual([
      { decade: 1960, uids: ['a', 'c'] },
      { decade: 1950, uids: ['b'] },
    ])
  })

  it('has no sections for an empty gallery', () => {
    expect(groupPhotosByDecade([])).toEqual([])
  })
})

describe('formatDecade', () => {
  it('writes the calendar decade as an en-dashed range', () => {
    expect(formatDecade(1950)).toBe('1950–1959')
  })

  it('leaves the undated section to the caller to name', () => {
    expect(formatDecade(null)).toBeNull()
  })
})

describe('decadeAnchorId', () => {
  it('names a decade and the undated section distinctly', () => {
    expect(decadeAnchorId(1950)).toBe('kk-decade-1950')
    expect(decadeAnchorId(null)).toBe('kk-decade-undated')
  })
})

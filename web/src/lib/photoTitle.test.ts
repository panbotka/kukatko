import { describe, expect, it } from 'vitest'

import { photoDisplayTitle, photoTitleText, type TitleSource } from './photoTitle'

/** A photo with no title, no date and no place — the empty starting point. */
function photo(over: Partial<TitleSource> = {}): TitleSource {
  return { title: '', ...over }
}

/** A geocoded place hierarchy, blank at every level unless the case fills one. */
function place(over: Partial<NonNullable<TitleSource['place']>> = {}) {
  return { city: '', place_name: '', country: '', ...over }
}

describe('photoDisplayTitle', () => {
  it("uses the user's title when there is one", () => {
    const got = photoDisplayTitle(photo({ title: 'Vánoce u babičky' }), '12. 7. 2026 23:03')
    expect(got).toEqual({ kind: 'title', text: 'Vánoce u babičky' })
  })

  it('trims a padded title', () => {
    expect(photoDisplayTitle(photo({ title: '  Léto  ' }), '')).toEqual({
      kind: 'title',
      text: 'Léto',
    })
  })

  it('treats a whitespace-only title as no title', () => {
    expect(photoDisplayTitle(photo({ title: '   ' }), '12. 7. 2026').kind).toBe('facts')
  })

  it('falls back to the capture date', () => {
    expect(photoDisplayTitle(photo(), '12. 7. 2026 23:03')).toEqual({
      kind: 'facts',
      date: '12. 7. 2026 23:03',
      place: '',
    })
  })

  it('adds the place to the date when the photo has one', () => {
    const got = photoDisplayTitle(photo({ place: place({ city: 'Brno' }) }), '12. 7. 2026')
    expect(got).toEqual({ kind: 'facts', date: '12. 7. 2026', place: 'Brno' })
  })

  it('prefers the narrowest place the geocoder resolved', () => {
    const got = photoDisplayTitle(
      photo({ place: place({ place_name: 'Špilberk', city: 'Brno', country: 'Česko' }) }),
      '',
    )
    expect(got).toEqual({ kind: 'facts', date: '', place: 'Špilberk' })
  })

  it('skips the blank levels the geocoder left behind', () => {
    const got = photoDisplayTitle(
      photo({ place: place({ place_name: '', city: '', country: 'Česko' }) }),
      '',
    )
    expect(got).toEqual({ kind: 'facts', date: '', place: 'Česko' })
  })

  it('shows a place alone for a photo with no date', () => {
    expect(photoDisplayTitle(photo({ place: place({ city: 'Brno' }) }), '').kind).toBe('facts')
  })

  it('admits it knows no name rather than reaching for the filename', () => {
    expect(photoDisplayTitle(photo(), '')).toEqual({ kind: 'unknown' })
  })

  it('never surfaces a filename, whatever the photo carries', () => {
    // The rule cannot even see a filename: it is not part of its input, which is
    // the point — the camera's name for a photo is not the photo's name.
    const source = photo({ place: place({ city: 'Brno' }) })
    expect(JSON.stringify(photoDisplayTitle(source, '12. 7. 2026'))).not.toContain('IMG_')
  })
})

describe('photoTitleText', () => {
  it('is the title itself for a titled photo', () => {
    expect(photoTitleText({ kind: 'title', text: 'Léto' }, 'Untitled')).toBe('Léto')
  })

  it('joins a date and a place with an en dash', () => {
    expect(photoTitleText({ kind: 'facts', date: '12. 7. 2026', place: 'Brno' }, 'Untitled')).toBe(
      '12. 7. 2026 – Brno',
    )
  })

  it('leaves no dangling dash when only the date is known', () => {
    expect(photoTitleText({ kind: 'facts', date: '12. 7. 2026', place: '' }, 'Untitled')).toBe(
      '12. 7. 2026',
    )
  })

  it('leaves no dangling dash when only the place is known', () => {
    expect(photoTitleText({ kind: 'facts', date: '', place: 'Brno' }, 'Untitled')).toBe('Brno')
  })

  it('uses the untitled wording when the photo has no identity', () => {
    expect(photoTitleText({ kind: 'unknown' }, 'Bez názvu')).toBe('Bez názvu')
  })
})

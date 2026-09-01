import { fireEvent, render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'
import { type SubjectCount, type SubjectFace } from '../../services/people'

import { SubjectTile } from './SubjectTile'

/**
 * A subject with no cover and no face, so the tile falls back to the placeholder
 * and the test is about the caption rather than about image loading. The two
 * counts always differ here: a fixture where they agree could not tell a fixed
 * tile from a broken one.
 */
function subject(over: Partial<SubjectCount> = {}): SubjectCount {
  return {
    uid: 's1',
    slug: 'anna',
    name: 'Anna',
    type: 'person',
    favorite: false,
    private: false,
    notes: '',
    birth_year: null,
    death_year: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    marker_count: 5,
    photo_count: 3,
    ...over,
  }
}

/**
 * A cover face `share` of the frame's width across, on a 4032x3024 photo — the
 * shape most of the catalogue is.
 */
function coverFace(share: number): SubjectFace {
  return {
    photo_uid: 'p1',
    x: 0.4,
    y: 0.3,
    w: share,
    h: (share * 4032) / 3024,
    width: 4032,
    height: 3024,
    orientation: 1,
  }
}

function renderTile(over: Partial<SubjectCount> = {}) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <SubjectTile subject={subject(over)} />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('SubjectTile', () => {
  it('counts the photos the person appears on, not the faces found on them', () => {
    // The caption says "photos", and one photo can hold several of a person's
    // faces — the marker count would promise a gallery bigger than the one the
    // tile links to.
    renderTile()
    expect(screen.getByText('3 photos')).toBeInTheDocument()
    expect(screen.queryByText('5 photos')).not.toBeInTheDocument()
  })

  it('says "1 photo" for a person seen once, however many faces that photo holds', () => {
    renderTile({ photo_count: 1, marker_count: 4 })
    expect(screen.getByText('1 photo')).toBeInTheDocument()
  })

  it('says "no photos" as a plural for a subject with nothing visible', () => {
    renderTile({ photo_count: 0, marker_count: 0 })
    expect(screen.getByText('0 photos')).toBeInTheDocument()
  })

  it('picks the Czech plural form the count calls for', async () => {
    // Czech splits where English does not: one takes "fotka", two to four "fotky",
    // five and up "fotek". Getting the count right is only half the fix.
    await i18n.changeLanguage('cs')
    renderTile({ photo_count: 1 })
    expect(screen.getByText('1 fotka')).toBeInTheDocument()

    renderTile({ photo_count: 3 })
    expect(screen.getByText('3 fotky')).toBeInTheDocument()

    renderTile({ photo_count: 12 })
    expect(screen.getByText('12 fotek')).toBeInTheDocument()
  })

  it('shows a life span beside the count for whoever has one', () => {
    renderTile({ birth_year: 1923, death_year: 1998 })
    expect(screen.getByText('3 photos · 1923–1998')).toBeInTheDocument()
  })

  it('shows a birth year alone when that is all there is', () => {
    renderTile({ birth_year: 1923 })
    expect(screen.getByText('3 photos · *1923')).toBeInTheDocument()
  })

  it('says nothing about years for a subject carrying none', () => {
    renderTile()
    expect(screen.getByText('3 photos')).toBeInTheDocument()
  })

  it('links to the subject page', () => {
    renderTile()
    expect(screen.getByRole('link', { name: 'Anna' })).toHaveAttribute('href', '/people/s1')
  })
})

describe('SubjectTile picture', () => {
  it('loads the backend avatar rendition, not a whole-frame preview', () => {
    // The regression this tile is named for: 72 of these squares pulled 125 Mpx
    // because each cropped a face out of a full preview in the browser. The crop
    // is cut server-side now, so a tile fetches the pixels it paints.
    renderTile({ cover_face: coverFace(0.3) })
    const src = screen.getByRole('img', { name: 'Anna' }).getAttribute('src')
    expect(src).toBe('/api/v1/subjects/s1/avatar')
    expect(src).not.toContain('/thumb/')
  })

  it('asks for the same avatar for a chosen cover photo', () => {
    // The rendition follows the subject, not the picture behind it, so the tile
    // needs no second code path for a hand-picked cover.
    renderTile({ cover_photo_uid: 'p9' })
    expect(screen.getByRole('img', { name: 'Anna' })).toHaveAttribute(
      'src',
      '/api/v1/subjects/s1/avatar',
    )
  })

  it('asks for nothing for a subject with no picture at all', () => {
    // A request that can only 404 is worse than the placeholder it would end in.
    renderTile()
    expect(screen.queryByRole('img', { name: 'Anna' })).toBeNull()
    expect(screen.getByText('No cover')).toBeInTheDocument()
  })

  it('keeps the placeholder for a face on a frame the crop maths cannot use', () => {
    renderTile({ cover_face: { ...coverFace(0.3), width: 0, height: 0 } })
    expect(screen.queryByRole('img', { name: 'Anna' })).toBeNull()
  })

  it('shimmers until the avatar has decoded, so a filling page is not a broken one', () => {
    const { container } = renderTile({ cover_face: coverFace(0.3) })
    expect(container.querySelector('.kk-skeleton')).not.toBeNull()

    fireEvent.load(screen.getByRole('img', { name: 'Anna' }))
    expect(container.querySelector('.kk-skeleton')).toBeNull()
  })

  it('falls back to the placeholder when the avatar will never arrive', () => {
    // A placeholder that pulses forever says "still loading" about an image that
    // has already given up, and a broken-image glyph says even less.
    const { container } = renderTile({ cover_face: coverFace(0.3) })
    fireEvent.error(screen.getByRole('img', { name: 'Anna' }))
    expect(container.querySelector('.kk-skeleton')).toBeNull()
    expect(screen.queryByRole('img', { name: 'Anna' })).toBeNull()
    expect(screen.getByText('No cover')).toBeInTheDocument()
  })

  it('shimmers under a chosen cover photo too', () => {
    const { container } = renderTile({ cover_photo_uid: 'p9' })
    expect(container.querySelector('.kk-skeleton')).not.toBeNull()

    fireEvent.load(screen.getByRole('img', { name: 'Anna' }))
    expect(container.querySelector('.kk-skeleton')).toBeNull()
  })
})

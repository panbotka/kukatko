import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'
import { type SubjectCount } from '../../services/people'

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
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    marker_count: 5,
    photo_count: 3,
    ...over,
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

  it('links to the subject page', () => {
    renderTile()
    expect(screen.getByRole('link', { name: 'Anna' })).toHaveAttribute('href', '/people/s1')
  })
})

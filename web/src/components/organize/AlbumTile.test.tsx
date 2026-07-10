import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'
import { type AlbumSummary } from '../../services/organize'

import { AlbumTile } from './AlbumTile'

/** Builds an album summary fixture, overriding the fields a case cares about. */
function album(overrides: Partial<AlbumSummary> = {}): AlbumSummary {
  return {
    uid: 'al1',
    slug: 'pout-2024',
    title: 'Pouť 2024',
    description: '',
    type: 'album',
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: 3,
    ...overrides,
  }
}

function renderTile(a: AlbumSummary) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <AlbumTile album={a} />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('AlbumTile', () => {
  it('renders the effective cover thumbnail, whether hand-picked or derived', () => {
    renderTile(album({ cover_uid: 'ph_newest' }))
    const img = screen.getByRole('img', { name: 'Pouť 2024' })
    expect(img).toHaveAttribute('src', expect.stringContaining('/photos/ph_newest/thumb/'))
    expect(screen.queryByTestId('empty-state')).toBeNull()
  })

  it('falls back to the shared empty state only for an album with nothing to show', () => {
    renderTile(album({ photo_count: 0 }))
    expect(screen.getByTestId('empty-state')).toBeInTheDocument()
    expect(screen.getByText('Empty album')).toBeInTheDocument()
    expect(screen.queryByRole('img')).toBeNull()
  })

  it('shows the capture range under the title', () => {
    renderTile(
      album({
        cover_uid: 'ph1',
        taken_from: '1998-07-15T12:00:00Z',
        taken_to: '1999-04-15T12:00:00Z',
      }),
    )
    expect(screen.getByText('1998–1999')).toBeInTheDocument()
  })

  it('shows no range line when no photo in the album is dated', () => {
    // A title free of digits, so the only four-digit text a match could find
    // would be the range line itself.
    renderTile(album({ cover_uid: 'ph1', title: 'Sraz rodáků' }))
    expect(screen.getByText('3 photos')).toBeInTheDocument()
    expect(screen.queryByText(/\d{4}/)).toBeNull()
  })
})

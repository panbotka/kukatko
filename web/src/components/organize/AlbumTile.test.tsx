import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'
import { type AlbumCover } from '../../lib/albumCovers'
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

function renderTile(a: AlbumSummary, cover?: AlbumCover) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <AlbumTile album={a} cover={cover} />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

/** The `src` of every image the tile drew, in document order. */
function sources(container: HTMLElement): string[] {
  return [...container.querySelectorAll('img')].map((img) => img.getAttribute('src') ?? '')
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

  it('renders a machine-made English title in Czech, everywhere the title is used', async () => {
    await i18n.changeLanguage('cs')
    renderTile(album({ cover_uid: 'ph1', title: 'January 2026' }))

    // The link's accessible name, its hover text and the alt text all come from
    // the same display name, so none of them can drift back to the raw title.
    const link = screen.getByRole('link', { name: 'leden 2026' })
    expect(link).toHaveAttribute('title', 'leden 2026')
    expect(screen.getByRole('img', { name: 'leden 2026' })).toBeInTheDocument()
    expect(screen.queryByText('January 2026')).toBeNull()
  })

  it('leaves a hand-written title untouched', async () => {
    await i18n.changeLanguage('cs')
    renderTile(album({ cover_uid: 'ph1' }))
    expect(screen.getByRole('link', { name: 'Pouť 2024' })).toBeInTheDocument()
  })

  it('draws the planned collage, one image per photo', () => {
    const { container } = renderTile(album({ cover_uid: 'p1', photo_count: 40 }), {
      kind: 'collage',
      photoUids: ['p1', 'p2', 'p3', 'p4'],
    })

    expect(sources(container)).toEqual([
      '/api/v1/photos/p1/thumb/tile_224',
      '/api/v1/photos/p2/thumb/tile_224',
      '/api/v1/photos/p3/thumb/tile_224',
      '/api/v1/photos/p4/thumb/tile_224',
    ])
    // The cells are decoration: the link is already named after the album, and
    // four identical alt texts would only make the page longer to listen to.
    expect(screen.queryAllByRole('img')).toHaveLength(0)
    expect(screen.getByRole('link', { name: 'Pouť 2024' })).toBeInTheDocument()
  })

  it('collages an album of its own accord when no plan is passed', () => {
    // A tile rendered alone has nobody to collide with, so it plans for itself.
    const { container } = renderTile(
      album({ cover_uid: 'p1', cover_uids: ['p1', 'p2', 'p3', 'p4', 'p5'], photo_count: 5 }),
    )
    expect(sources(container)).toEqual([
      '/api/v1/photos/p1/thumb/tile_224',
      '/api/v1/photos/p2/thumb/tile_224',
      '/api/v1/photos/p3/thumb/tile_224',
      '/api/v1/photos/p4/thumb/tile_224',
    ])
  })

  it('draws one photo, at grid size, for an album too small to collage', () => {
    const { container } = renderTile(
      album({ cover_uid: 'p1', cover_uids: ['p1', 'p2'], photo_count: 2 }),
    )
    expect(sources(container)).toEqual(['/api/v1/photos/p1/thumb/tile_500'])
    expect(screen.getByRole('img', { name: 'Pouť 2024' })).toBeInTheDocument()
  })

  it('shows no range line when no photo in the album is dated', () => {
    // A title free of digits, so the only four-digit text a match could find
    // would be the range line itself.
    renderTile(album({ cover_uid: 'ph1', title: 'Sraz rodáků' }))
    expect(screen.getByText('3 photos')).toBeInTheDocument()
    expect(screen.queryByText(/\d{4}/)).toBeNull()
  })
})

describe('AlbumTile description', () => {
  it('says what the album is, where a title alone often does not', () => {
    renderTile(album({ description: 'Sjezd rodáků, dva dny, 780 let obce.' }))
    const note = screen.getByText('Sjezd rodáků, dva dny, 780 let obce.')
    // Clamped to two lines: a card grid whose rows are different heights reads
    // as broken, and the detail page shows the whole text.
    expect(note).toHaveClass('kk-prose-clamp')
  })

  it('spends no line on an album that has no description', () => {
    const { container } = renderTile(album())
    expect(container.querySelector('.kk-prose-clamp')).toBeNull()
  })
})

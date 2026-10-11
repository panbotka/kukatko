import { render, screen, waitFor } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { clearBlurPlaceholderCache } from '../../lib/blurPlaceholder'
import { type FeaturedAlbum, type FeaturedAlbums } from '../../services/featured'
import { STUB_CANVAS_DATA_URL, stubBlurCanvas } from '../../test/canvas'

import { FeaturedAlbumsStrip } from './FeaturedAlbumsStrip'

vi.mock('../../services/featured', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/featured')>()
  return { ...actual, fetchFeaturedAlbums: vi.fn() }
})

const { fetchFeaturedAlbums } = await import('../../services/featured')
const fetchMock = vi.mocked(fetchFeaturedAlbums)

/** A featured album fixture, overriding the fields a case cares about. */
function featured(overrides: Partial<FeaturedAlbum> = {}): FeaturedAlbum {
  return {
    uid: 'al1',
    slug: 'pout-2026',
    title: 'Pouť 2026',
    description: '',
    type: 'album',
    private: false,
    created_at: '2026-10-01T00:00:00Z',
    updated_at: '2026-10-01T00:00:00Z',
    featured_until: '2026-12-11T22:59:59Z',
    featured_at: '2026-10-11T10:00:00Z',
    photo_count: 42,
    cover_uid: 'ph1',
    thumb_url: 'https://media.example/thumb/ph1.jpg',
    ...overrides,
  }
}

/** A response body holding the given albums. */
function body(albums: FeaturedAlbum[]): FeaturedAlbums {
  return { albums, default_days: 61, max_days: 366 }
}

function renderStrip() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <FeaturedAlbumsStrip />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
})

describe('FeaturedAlbumsStrip', () => {
  it('lists the featured albums as cards linking to each album', async () => {
    fetchMock.mockResolvedValue(
      body([
        featured(),
        featured({
          uid: 'al2',
          title: 'Hody',
          photo_count: 1,
          cover_uid: 'ph2',
          thumb_url: undefined,
        }),
      ]),
    )
    const { container } = renderStrip()

    const heading = await screen.findByRole('heading', { name: 'Featured' })
    const region = screen.getByRole('region', { name: 'Featured' })
    expect(region).toContainElement(heading)
    expect(screen.getByRole('link', { name: /Pouť 2026/ })).toHaveAttribute('href', '/albums/al1')
    expect(screen.getByRole('link', { name: /Hody/ })).toHaveAttribute('href', '/albums/al2')
    expect(screen.getByText('42 photos')).toBeInTheDocument()
    expect(screen.getByText('1 photo')).toBeInTheDocument()

    // The server's URL wins; a cover without one falls back to the app's route.
    const sources = [...container.querySelectorAll('img')].map((img) => img.getAttribute('src'))
    expect(sources[0]).toBe('https://media.example/thumb/ph1.jpg')
    expect(sources[1]).toContain('/photos/ph2/thumb/')
  })

  it('paints the cover blurhash behind the image', async () => {
    clearBlurPlaceholderCache()
    stubBlurCanvas()
    fetchMock.mockResolvedValue(
      body([featured({ cover_blurhash: 'LEHV6nWB2yk8pyo0adR*.7kCMdnj' })]),
    )
    const { container } = renderStrip()
    await screen.findByRole('heading', { name: 'Featured' })
    const blur = container.querySelector<HTMLElement>('.kk-media-blur')
    expect(blur?.style.backgroundImage).toContain(STUB_CANVAS_DATA_URL)
  })

  it('shows an album with nothing to show as the empty state', async () => {
    fetchMock.mockResolvedValue(
      body([featured({ cover_uid: undefined, thumb_url: undefined, photo_count: 0 })]),
    )
    renderStrip()
    await screen.findByRole('heading', { name: 'Featured' })
    expect(screen.getByText('Empty album')).toBeInTheDocument()
  })

  it('renders nothing at all when nothing is featured', async () => {
    fetchMock.mockResolvedValue(body([]))
    const { container } = renderStrip()
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled()
    })
    expect(container).toBeEmptyDOMElement()
  })

  it('renders nothing when the list cannot be loaded', async () => {
    fetchMock.mockRejectedValue(new Error('offline'))
    const { container } = renderStrip()
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled()
    })
    expect(container).toBeEmptyDOMElement()
  })
})

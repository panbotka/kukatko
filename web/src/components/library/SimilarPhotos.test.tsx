import { render, screen, waitFor } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type SimilarPhoto } from '../../services/photos'

import { SimilarPhotos } from './SimilarPhotos'

// Keep the real thumbUrl/GRID_THUMB_SIZE; only the network call is faked.
vi.mock('../../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/photos')>()
  return { ...actual, fetchSimilar: vi.fn() }
})

const { fetchSimilar } = await import('../../services/photos')
const fetchMock = vi.mocked(fetchSimilar)

function similar(uid: string, name: string, distance: number): SimilarPhoto {
  return {
    uid,
    file_hash: uid,
    file_name: name,
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
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    distance,
  }
}

function renderSimilar(uid = 'ph-source') {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <SimilarPhotos uid={uid} />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('SimilarPhotos', () => {
  it('renders the similar thumbnails as links to their detail routes', async () => {
    fetchMock.mockResolvedValue([similar('ph-a', 'a.jpg', 0.1), similar('ph-b', 'b.jpg', 0.2)])
    renderSimilar('ph-source')

    const linkA = await screen.findByRole('link', { name: 'a.jpg' })
    expect(linkA).toHaveAttribute('href', '/photos/ph-a')
    const linkB = screen.getByRole('link', { name: 'b.jpg' })
    expect(linkB).toHaveAttribute('href', '/photos/ph-b')

    // It fetched for the source photo.
    expect(fetchMock.mock.calls[0][0]).toBe('ph-source')
  })

  it('renders nothing when there are no similar photos', async () => {
    fetchMock.mockResolvedValue([])
    const { container } = renderSimilar()

    await waitFor(() => {
      expect(container).toBeEmptyDOMElement()
    })
  })

  it('shows an error message when the request fails', async () => {
    fetchMock.mockRejectedValue(new Error('boom'))
    renderSimilar()

    expect(await screen.findByText('Could not load similar photos.')).toBeInTheDocument()
  })

  it('refetches when the uid changes', async () => {
    fetchMock.mockResolvedValue([similar('ph-a', 'a.jpg', 0.1)])
    const { rerender } = renderSimilar('ph-1')
    await screen.findByRole('link', { name: 'a.jpg' })
    expect(fetchMock.mock.calls[0][0]).toBe('ph-1')

    fetchMock.mockResolvedValue([similar('ph-z', 'z.jpg', 0.1)])
    rerender(
      <I18nextProvider i18n={i18n}>
        <MemoryRouter>
          <SimilarPhotos uid="ph-2" />
        </MemoryRouter>
      </I18nextProvider>,
    )

    await screen.findByRole('link', { name: 'z.jpg' })
    const calls = fetchMock.mock.calls
    expect(calls[calls.length - 1][0]).toBe('ph-2')
  })
})

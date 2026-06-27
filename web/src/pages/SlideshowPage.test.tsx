import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { type Photo, type PhotoListResponse } from '../services/photos'

import { SlideshowPage } from './SlideshowPage'

// Keep the real helpers; only the network call is faked.
vi.mock('../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/photos')>()
  return { ...actual, fetchPhotos: vi.fn() }
})

const { fetchPhotos } = await import('../services/photos')
const fetchMock = vi.mocked(fetchPhotos)

function photo(uid: string, name: string): Photo {
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
  }
}

function page(photos: Photo[], extra: Partial<PhotoListResponse> = {}): PhotoListResponse {
  return { photos, total: photos.length, limit: 100, offset: 0, next_offset: null, ...extra }
}

function renderPage(initialEntry = '/slideshow') {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={[initialEntry]}>
        <SlideshowPage />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  window.localStorage.clear()
  fetchMock.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
  window.localStorage.clear()
})

describe('SlideshowPage', () => {
  it('scopes the fetch to the album / label and filters from the URL', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage('/slideshow?album=al1&sort=oldest')

    await screen.findByRole('img')
    const params = fetchMock.mock.calls[0][0]
    expect(params.album).toBe('al1')
    expect(params.sort).toBe('oldest')
  })

  it('renders the slideshow stage when photos load', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')]))
    renderPage('/slideshow?label=lb1')

    await screen.findByRole('img')
    expect(screen.getByText('1 / 2')).toBeInTheDocument()
  })

  it('shows a graceful empty state for an empty set', async () => {
    fetchMock.mockResolvedValue(page([]))
    renderPage('/slideshow?album=al1')

    expect(await screen.findByText('No photos to play')).toBeInTheDocument()
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
  })

  it('shows an error state with retry when loading fails', async () => {
    fetchMock.mockRejectedValueOnce(new Error('boom'))
    renderPage('/slideshow?album=al1')

    expect(await screen.findByText('Could not load photos.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument()
  })

  it('persists the chosen effect to localStorage from the settings panel', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderPage('/slideshow?album=al1')

    await screen.findByRole('img')
    await user.click(screen.getByRole('button', { name: 'Settings' }))
    await user.selectOptions(screen.getByLabelText('Transition'), 'slide')

    await waitFor(() => {
      const stored = JSON.parse(
        window.localStorage.getItem('kukatko.slideshow.settings') ?? '{}',
      ) as { effect?: string }
      expect(stored.effect).toBe('slide')
    })
  })
})

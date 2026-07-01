import { fireEvent, render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import type { Photo } from '../../services/photos'

import { PhotoTile } from './PhotoTile'

// Only the rating network call is faked; the optimistic hook logic runs for real.
vi.mock('../../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/photos')>()
  return { ...actual, ratePhoto: vi.fn() }
})

const { ratePhoto } = await import('../../services/photos')
const rateMock = vi.mocked(ratePhoto)

/** Builds a minimal Photo with the given overrides. */
function photo(overrides: Partial<Photo> = {}): Photo {
  return {
    uid: 'ph1',
    file_hash: 'h',
    file_name: 'clip.mp4',
    file_size: 100,
    file_mime: 'video/mp4',
    file_width: 1920,
    file_height: 1080,
    taken_at_source: 'unknown',
    title: 'Clip',
    description: '',
    camera_make: '',
    camera_model: '',
    lens_model: '',
    private: false,
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    ...overrides,
  }
}

function renderTile(p: Photo, ratable = false) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <PhotoTile photo={p} ratable={ratable} />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  rateMock.mockReset()
  rateMock.mockResolvedValue(undefined)
})

describe('PhotoTile video badge', () => {
  it('shows a video badge with formatted duration for videos', () => {
    renderTile(photo({ media_type: 'video', duration_ms: 154000 }))
    const badge = screen.getByRole('img', { name: 'Video' })
    expect(badge).toBeInTheDocument()
    expect(badge).toHaveTextContent('2:34')
  })

  it('shows a live badge for live photos', () => {
    renderTile(photo({ media_type: 'live', file_name: 'live.heic' }))
    expect(screen.getByRole('img', { name: 'Live' })).toBeInTheDocument()
  })

  it('shows the badge without a duration when the length is unknown', () => {
    renderTile(photo({ media_type: 'video' }))
    const badge = screen.getByRole('img', { name: 'Video' })
    expect(badge).toBeInTheDocument()
    expect(badge.textContent).not.toMatch(/\d:\d\d/)
  })

  it('shows no video badge for still images', () => {
    renderTile(photo({ media_type: 'image', file_name: 'still.jpg' }))
    expect(screen.queryByRole('img', { name: 'Video' })).not.toBeInTheDocument()
    expect(screen.queryByRole('img', { name: 'Live' })).not.toBeInTheDocument()
  })
})

describe('PhotoTile rating overlay', () => {
  it('shows a star/flag overlay only when ratable', () => {
    const { rerender } = renderTile(photo({ media_type: 'image', file_name: 'still.jpg' }))
    expect(screen.queryByRole('button', { name: 'Rate 3 of 5' })).not.toBeInTheDocument()

    rerender(
      <I18nextProvider i18n={i18n}>
        <MemoryRouter>
          <PhotoTile photo={photo({ media_type: 'image' })} ratable />
        </MemoryRouter>
      </I18nextProvider>,
    )
    expect(screen.getByRole('button', { name: 'Rate 3 of 5' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Pick' })).toBeInTheDocument()
  })

  it('sets the rating via a number-key hotkey on the focused tile', () => {
    renderTile(photo({ media_type: 'image' }), true)
    const link = screen.getByRole('link', { name: 'Clip' })

    fireEvent.keyDown(link, { key: '5' })
    expect(rateMock).toHaveBeenCalledWith('ph1', { rating: 5 })
  })

  it('sets pick/reject flags via the p and r hotkeys', () => {
    renderTile(photo({ media_type: 'image' }), true)
    const link = screen.getByRole('link', { name: 'Clip' })

    fireEvent.keyDown(link, { key: 'p' })
    expect(rateMock).toHaveBeenCalledWith('ph1', { flag: 'pick' })

    fireEvent.keyDown(link, { key: 'r' })
    expect(rateMock).toHaveBeenCalledWith('ph1', { flag: 'reject' })
  })

  it('dims the tile and shows a badge for reject-flagged photos', () => {
    renderTile(photo({ media_type: 'image', flag: 'reject' }), true)
    expect(screen.getByText('Rejected')).toBeInTheDocument()
  })
})

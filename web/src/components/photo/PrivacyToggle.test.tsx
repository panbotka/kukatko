import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type PhotoDetail } from '../../services/photos'

import { PrivacyToggle } from './PrivacyToggle'

vi.mock('../../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/photos')>()
  return { ...actual, updatePhoto: vi.fn() }
})

const { updatePhoto } = await import('../../services/photos')
const updatePhotoMock = vi.mocked(updatePhoto)

function photo(overrides: Partial<PhotoDetail> = {}): PhotoDetail {
  return {
    uid: 'b',
    file_hash: 'b',
    file_name: 'b.jpg',
    file_size: 1,
    file_mime: 'image/jpeg',
    file_width: 1,
    file_height: 1,
    taken_at_source: 'exif',
    thumb_url: '/api/v1/photos/b/thumb/tile_500',
    download_url: '/api/v1/photos/b/download?original=true',
    title: 'Beach',
    description: '',
    camera_make: '',
    camera_model: '',
    lens_model: '',
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    files: [],
    albums: [],
    labels: [],
    ...overrides,
  }
}

function renderToggle(props: { photo?: PhotoDetail; onUpdated?: () => void } = {}) {
  return render(
    <I18nextProvider i18n={i18n}>
      <PrivacyToggle photo={props.photo ?? photo()} onUpdated={props.onUpdated ?? vi.fn()} />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.clearAllMocks()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('PrivacyToggle', () => {
  it('PATCHes the photo private when it is currently public', async () => {
    const onUpdated = vi.fn()
    updatePhotoMock.mockResolvedValue(photo({ private: true }))
    const user = userEvent.setup()
    renderToggle({ onUpdated })

    const toggle = screen.getByRole('button', { name: 'Make private' })
    expect(toggle).toHaveAttribute('aria-pressed', 'false')
    await user.click(toggle)
    await waitFor(() => {
      expect(updatePhotoMock).toHaveBeenCalledWith('b', { private: true })
    })
    expect(onUpdated).toHaveBeenCalled()
  })

  it('PATCHes the photo public when it is currently private', async () => {
    updatePhotoMock.mockResolvedValue(photo({ private: false }))
    const user = userEvent.setup()
    renderToggle({ photo: photo({ private: true }) })

    const toggle = screen.getByRole('button', { name: 'Make public' })
    expect(toggle).toHaveAttribute('aria-pressed', 'true')
    await user.click(toggle)
    await waitFor(() => {
      expect(updatePhotoMock).toHaveBeenCalledWith('b', { private: false })
    })
  })
})

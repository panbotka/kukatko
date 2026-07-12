import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type PhotoDetail } from '../../services/photos'

import { TechnicalDetails } from './TechnicalDetails'

function photo(overrides: Partial<PhotoDetail> = {}): PhotoDetail {
  return {
    uid: 'b',
    file_hash: 'b',
    file_name: 'b.jpg',
    file_size: 100,
    file_mime: 'image/jpeg',
    file_width: 4000,
    file_height: 3000,
    taken_at_source: 'exif',
    thumb_url: '/api/v1/photos/b/thumb/tile_500',
    download_url: '/api/v1/photos/b/download?original=true',
    title: 'Beach',
    description: '',
    notes: '',
    camera_make: 'Canon',
    camera_model: 'EOS R5',
    lens_model: 'RF 24-70',
    iso: 200,
    aperture: 2.8,
    exposure: '1/250',
    focal_length: 50,
    private: false,
    created_at: '2026-01-02T10:00:00Z',
    updated_at: '2026-01-02T10:00:00Z',
    files: [],
    albums: [],
    labels: [],
    ...overrides,
  }
}

function renderDetails(overrides: Partial<PhotoDetail> = {}, canWrite = false) {
  return render(
    <I18nextProvider i18n={i18n}>
      <TechnicalDetails
        photo={photo(overrides)}
        canWrite={canWrite}
        onThumbnailRegenerated={vi.fn()}
      />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('TechnicalDetails', () => {
  it('is collapsed on first render', () => {
    renderDetails()

    expect(screen.getByRole('button', { name: 'Technical details' })).toHaveAttribute(
      'aria-expanded',
      'false',
    )
    expect(screen.queryByText('EOS R5')).not.toBeInTheDocument()
    expect(screen.queryByText('ISO 200')).not.toBeInTheDocument()
    expect(screen.queryByText('b.jpg')).not.toBeInTheDocument()
  })

  it('reveals the camera, lens, EXIF, file name and dimensions when expanded', async () => {
    const user = userEvent.setup()
    renderDetails()

    await user.click(screen.getByRole('button', { name: 'Technical details' }))

    expect(screen.getByRole('button', { name: 'Technical details' })).toHaveAttribute(
      'aria-expanded',
      'true',
    )
    expect(screen.getByText('EOS R5')).toBeInTheDocument()
    expect(screen.getByText('RF 24-70')).toBeInTheDocument()
    expect(screen.getByText('f/2.8')).toBeInTheDocument()
    expect(screen.getByText('1/250 s')).toBeInTheDocument()
    expect(screen.getByText('50 mm')).toBeInTheDocument()
    expect(screen.getByText('ISO 200')).toBeInTheDocument()
    expect(screen.getByText('b.jpg')).toBeInTheDocument()
    expect(screen.getByText('4000 × 3000 px')).toBeInTheDocument()
  })

  it('collapses again on a second click', async () => {
    const user = userEvent.setup()
    renderDetails()

    const toggle = screen.getByRole('button', { name: 'Technical details' })
    await user.click(toggle)
    await user.click(toggle)

    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('EOS R5')).not.toBeInTheDocument()
  })

  it('shows the resolved uploader when expanded', async () => {
    const user = userEvent.setup()
    renderDetails({ uploader: { uid: 'u1', name: 'Camera Man' } })

    // The uploader is an intrinsic reference fact, so it lives here with the EXIF.
    expect(screen.queryByText('Uploaded by')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Technical details' }))
    expect(screen.getByText('Uploaded by')).toBeInTheDocument()
    expect(screen.getByText('Camera Man')).toBeInTheDocument()
  })

  it('falls back to a neutral uploader value when none is set', async () => {
    const user = userEvent.setup()
    renderDetails()

    await user.click(screen.getByRole('button', { name: 'Technical details' }))
    const label = screen.getByText('Uploaded by')
    expect(label.nextElementSibling).toHaveTextContent('—')
  })

  it('shows the regenerate-thumbnail button to editors when expanded', async () => {
    const user = userEvent.setup()
    renderDetails({}, true)

    // Hidden until the section is expanded — it is a maintenance action, not a
    // primary control.
    expect(screen.queryByRole('button', { name: /regenerate thumbnail/i })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Technical details' }))
    expect(screen.getByRole('button', { name: /regenerate thumbnail/i })).toBeInTheDocument()
  })

  it('hides the regenerate-thumbnail button from viewers', async () => {
    const user = userEvent.setup()
    renderDetails({}, false)

    await user.click(screen.getByRole('button', { name: 'Technical details' }))
    expect(screen.queryByRole('button', { name: /regenerate thumbnail/i })).not.toBeInTheDocument()
  })

  it('omits rows a photo has no value for', async () => {
    const user = userEvent.setup()
    renderDetails({
      camera_make: '',
      camera_model: '',
      lens_model: '',
      iso: undefined,
      aperture: undefined,
      exposure: '',
      focal_length: undefined,
      file_width: 0,
      file_height: 0,
    })

    await user.click(screen.getByRole('button', { name: 'Technical details' }))

    expect(screen.queryByText('Camera')).not.toBeInTheDocument()
    expect(screen.queryByText('Lens')).not.toBeInTheDocument()
    expect(screen.queryByText('Dimensions')).not.toBeInTheDocument()
    // The file name always survives.
    expect(screen.getByText('b.jpg')).toBeInTheDocument()
  })
})

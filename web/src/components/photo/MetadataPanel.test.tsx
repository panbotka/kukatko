import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type PhotoDetail } from '../../services/photos'

import { MetadataPanel } from './MetadataPanel'

/** Minimal picker surface the test drives on the mocked map. */
interface MockLeafletProps {
  picker?: {
    position: { lat: number; lng: number } | null
    onPick: (lat: number, lng: number) => void
  }
}

// Leaflet needs a real DOM/layout; stub the map bridge, exposing the controlled
// marker position and a button that simulates dropping the marker (drag/click).
vi.mock('../map/LeafletMap', () => ({
  LeafletMap: ({ picker }: MockLeafletProps) => (
    <div>
      <div
        data-testid="marker"
        data-lat={picker?.position ? String(picker.position.lat) : ''}
        data-lng={picker?.position ? String(picker.position.lng) : ''}
      >
        {picker?.position ? `${picker.position.lat},${picker.position.lng}` : 'none'}
      </div>
      <button
        type="button"
        onClick={() => {
          picker?.onPick(51.5, -0.12)
        }}
      >
        drop-marker
      </button>
    </div>
  ),
}))

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
    lat: 50.08,
    lng: 14.42,
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    files: [],
    albums: [],
    labels: [],
    ...overrides,
  }
}

function renderPanel(props: { photo?: PhotoDetail; onUpdated?: () => void } = {}) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MetadataPanel
        photo={props.photo ?? photo()}
        canWrite
        onUpdated={props.onUpdated ?? vi.fn()}
      />
    </I18nextProvider>,
  )
}

/** Enters the edit form via the Edit button. */
async function startEditing(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: 'Edit' }))
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.clearAllMocks()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('MetadataPanel location picker', () => {
  it('prefills the coordinate field in canonical decimal degrees', async () => {
    const user = userEvent.setup()
    renderPanel()
    await startEditing(user)
    expect(screen.getByLabelText('Coordinates')).toHaveValue('50.080000, 14.420000')
    expect(screen.getByTestId('marker')).toHaveTextContent('50.08,14.42')
  })

  it('moves the marker when valid coordinate text is typed', async () => {
    const user = userEvent.setup()
    renderPanel()
    await startEditing(user)
    const input = screen.getByLabelText('Coordinates')
    await user.clear(input)
    await user.type(input, '49.1234, 16.5678')
    expect(screen.getByTestId('marker')).toHaveTextContent('49.1234,16.5678')
  })

  it('parses DMS notation into the marker position', async () => {
    const user = userEvent.setup()
    renderPanel()
    await startEditing(user)
    const input = screen.getByLabelText('Coordinates')
    await user.clear(input)
    await user.type(input, '49°7\'24.2"N 16°34\'12.5"E')
    const marker = screen.getByTestId('marker')
    expect(Number(marker.getAttribute('data-lat'))).toBeCloseTo(49.1234, 3)
    expect(Number(marker.getAttribute('data-lng'))).toBeCloseTo(16.5701, 3)
  })

  it('shows a validation message and blocks saving on invalid coordinates', async () => {
    const user = userEvent.setup()
    renderPanel()
    await startEditing(user)
    const input = screen.getByLabelText('Coordinates')
    await user.clear(input)
    await user.type(input, 'nonsense')
    expect(
      screen.getByText(
        'Unrecognised coordinates. Use decimal degrees, DMS or degrees-decimal-minutes.',
      ),
    ).toBeInTheDocument()
    expect(screen.getByTestId('marker')).toHaveTextContent('none')
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  it('writes the picked marker position back and PATCHes decimal degrees', async () => {
    const onUpdated = vi.fn()
    updatePhotoMock.mockResolvedValue(photo({ lat: 51.5, lng: -0.12 }))
    const user = userEvent.setup()
    renderPanel({ onUpdated })
    await startEditing(user)

    await user.click(screen.getByRole('button', { name: 'drop-marker' }))
    expect(screen.getByLabelText('Coordinates')).toHaveValue('51.500000, -0.120000')

    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => {
      expect(updatePhotoMock).toHaveBeenCalledWith(
        'b',
        expect.objectContaining({ lat: 51.5, lng: -0.12 }),
      )
    })
    expect(onUpdated).toHaveBeenCalled()
  })

  it('clears the location and PATCHes null coordinates', async () => {
    updatePhotoMock.mockResolvedValue(photo({ lat: undefined, lng: undefined }))
    const user = userEvent.setup()
    renderPanel()
    await startEditing(user)

    await user.click(screen.getByRole('button', { name: 'Clear location' }))
    expect(screen.getByLabelText('Coordinates')).toHaveValue('')
    expect(screen.getByTestId('marker')).toHaveTextContent('none')

    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => {
      expect(updatePhotoMock).toHaveBeenCalledWith(
        'b',
        expect.objectContaining({ lat: null, lng: null }),
      )
    })
  })
})

import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type GeocodeResult } from '../../services/map'
import { type PhotoDetail } from '../../services/photos'
import { expectLive, expectOff } from '../../test/reasoned'

import { PhotoLocation } from './PhotoLocation'

// Leaflet needs a real DOM/layout; stub the map bridge.
vi.mock('../map/LeafletMap', () => ({
  LeafletMap: () => <div data-testid="map" />,
}))

vi.mock('../../services/map', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/map')>()
  return { ...actual, reverseGeocode: vi.fn() }
})
vi.mock('../../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/photos')>()
  return { ...actual, updatePhoto: vi.fn() }
})

const { reverseGeocode } = await import('../../services/map')
const { updatePhoto } = await import('../../services/photos')
const reverseGeocodeMock = vi.mocked(reverseGeocode)
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
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    files: [],
    albums: [],
    labels: [],
    ...overrides,
  }
}

function renderLocation(props: {
  canWrite?: boolean
  photo?: PhotoDetail
  onUpdated?: () => void
}) {
  return render(
    <I18nextProvider i18n={i18n}>
      <PhotoLocation
        photo={props.photo ?? photo()}
        canWrite={props.canWrite ?? true}
        onUpdated={props.onUpdated ?? vi.fn()}
      />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.clearAllMocks()
})

describe('PhotoLocation', () => {
  it('names the place under the map and keeps the numbers on hover', () => {
    renderLocation({
      photo: photo({
        place: {
          country: 'Česko',
          region: 'Jihomoravský kraj',
          city: 'Brno',
          place_name: 'Špilberk',
        },
      }),
    })

    expect(screen.getByTestId('map')).toBeInTheDocument()
    // "50.08000, 14.42000" is not an answer to "where is this?" — the place is.
    expect(screen.queryByText('50.08000, 14.42000')).not.toBeInTheDocument()
    const line = screen.getByText('Špilberk, Brno, Česko')
    expect(line).toHaveAttribute('title', '50.08000, 14.42000')
  })

  it('says the place is unknown rather than falling back to the numbers', () => {
    renderLocation({})

    const line = screen.getByText('The place is not known yet.')
    expect(line).toHaveAttribute('title', '50.08000, 14.42000')
  })

  it('replaces the cached place with the one just looked up', async () => {
    reverseGeocodeMock.mockResolvedValue({
      name: 'Špilberk Castle',
      location: 'Brno, Czechia',
      regional_structure: [],
    })
    const user = userEvent.setup()
    renderLocation({
      photo: photo({
        place: {
          country: 'Česko',
          region: 'Jihomoravský kraj',
          city: 'Brno',
          place_name: 'Špilberk',
        },
      }),
    })

    await user.click(screen.getByRole('button', { name: 'Look up place' }))

    // Answering a click with the string that was already on screen would read as
    // the button having done nothing.
    expect(await screen.findByText('Špilberk Castle')).toBeInTheDocument()
    expect(screen.queryByText('Špilberk, Brno, Česko')).not.toBeInTheDocument()
  })

  it('reverse-geocodes the place on demand', async () => {
    const place: GeocodeResult = {
      name: 'Prague',
      location: 'Prague, Czechia',
      regional_structure: [],
    }
    reverseGeocodeMock.mockResolvedValue(place)
    const user = userEvent.setup()
    renderLocation({})

    await user.click(screen.getByRole('button', { name: 'Look up place' }))
    await waitFor(() => {
      expect(reverseGeocodeMock).toHaveBeenCalledWith(50.08, 14.42)
    })
    expect(await screen.findByText('Prague')).toBeInTheDocument()
  })

  it('clears the location for editors via the API', async () => {
    const onUpdated = vi.fn()
    updatePhotoMock.mockResolvedValue(photo({ lat: undefined, lng: undefined }))
    const user = userEvent.setup()
    renderLocation({ onUpdated })

    await user.click(screen.getByRole('button', { name: 'Clear location' }))
    await waitFor(() => {
      expect(updatePhotoMock).toHaveBeenCalledWith('b', { lat: null, lng: null })
    })
    expect(onUpdated).toHaveBeenCalled()
  })

  it('hides the clear control from viewers and shows a hint without a location', () => {
    renderLocation({ canWrite: false })
    expect(screen.queryByRole('button', { name: 'Clear location' })).not.toBeInTheDocument()

    renderLocation({ photo: photo({ lat: undefined, lng: undefined }) })
    expect(screen.getByText('This photo has no stored location.')).toBeInTheDocument()
  })

  it('keeps the lookup live for a viewer — it is a read, not a write', () => {
    renderLocation({ canWrite: false })

    expectLive(screen.getByRole('button', { name: 'Look up place' }))
  })

  it('says the lookup is running rather than greying out in silence', async () => {
    let settle: (place: GeocodeResult) => void = () => undefined
    reverseGeocodeMock.mockReturnValue(
      new Promise<GeocodeResult>((resolve) => {
        settle = resolve
      }),
    )
    const user = userEvent.setup()
    renderLocation({})

    await user.click(screen.getByRole('button', { name: 'Look up place' }))
    expectOff(
      screen.getByRole('button', { name: 'Look up place' }),
      'The place is being looked up — one moment.',
    )

    settle({ name: 'Prague', location: 'Prague, Czechia', regional_structure: [] })
    expect(await screen.findByText('Prague')).toBeInTheDocument()
    expectLive(screen.getByRole('button', { name: 'Look up place' }))
  })

  it('says the location is being cleared while the write is in flight', async () => {
    let settle: (photo: PhotoDetail) => void = () => undefined
    updatePhotoMock.mockReturnValue(
      new Promise<PhotoDetail>((resolve) => {
        settle = resolve
      }),
    )
    const onUpdated = vi.fn()
    const user = userEvent.setup()
    renderLocation({ onUpdated })

    await user.click(screen.getByRole('button', { name: 'Clear location' }))
    expectOff(
      screen.getByRole('button', { name: 'Clear location' }),
      'The location is being cleared — one moment.',
    )

    settle(photo({ lat: undefined, lng: undefined }))
    await waitFor(() => {
      expect(onUpdated).toHaveBeenCalled()
    })
  })
})

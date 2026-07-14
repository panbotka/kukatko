import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type PhotoDetail, type PhotoMetadataUpdate } from '../../services/photos'

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
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    files: [],
    albums: [],
    labels: [],
    ...overrides,
  }
}

/**
 * Stands in for the API: applies the patch to the photo and answers with the full
 * detail body — files, albums, labels and all — which is exactly what
 * `PATCH /photos/{uid}` returns (it shares the detail endpoint's body; the Go
 * integration test pins that contract). The old mock returned a hand-made complete
 * PhotoDetail whatever the endpoint really sent back, which is why a PATCH response
 * that dropped `albums`/`labels`/`files` — and blanked the detail page — slipped
 * through the suite.
 */
function apiResponse(current: PhotoDetail, patch: PhotoMetadataUpdate): PhotoDetail {
  const estimated = patch.taken_at_estimated ?? current.taken_at_estimated ?? false
  const note = patch.taken_at_note ?? current.taken_at_note ?? ''
  return {
    ...current,
    title: patch.title ?? current.title,
    description: patch.description ?? current.description,
    notes: patch.notes ?? current.notes,
    ai_note: patch.ai_note ?? current.ai_note,
    // A null clears the field; an absent key leaves it as it was.
    taken_at: patch.taken_at === undefined ? current.taken_at : (patch.taken_at ?? undefined),
    taken_at_source: patch.taken_at === undefined ? current.taken_at_source : 'manual',
    taken_at_estimated: estimated,
    // The backend keeps the dating note only while the flag is set — clearing the
    // flag drops the note with it, whatever the photo held before.
    taken_at_note: estimated ? note : '',
    lat: patch.lat === undefined ? current.lat : (patch.lat ?? undefined),
    lng: patch.lng === undefined ? current.lng : (patch.lng ?? undefined),
  }
}

/** Makes the mocked `updatePhoto` answer like the real endpoint does for `current`. */
function mockApi(current: PhotoDetail) {
  updatePhotoMock.mockImplementation((_uid, patch) => Promise.resolve(apiResponse(current, patch)))
}

/** The patch body of the single `updatePhoto` call the test made. */
function sentPatch(): PhotoMetadataUpdate {
  expect(updatePhotoMock).toHaveBeenCalledTimes(1)
  return updatePhotoMock.mock.calls[0][1]
}

/**
 * The detail page in miniature: it holds the photo in state and feeds `onUpdated`
 * straight back in (the page's `setPhoto`), while — like the real OrganizePanel —
 * mapping over `photo.albums` and `photo.labels`. A save whose response lacks those
 * arrays throws here exactly as it did on the page, so the crash cannot come back
 * unnoticed.
 */
function DetailHarness({ initial }: { initial: PhotoDetail }) {
  const [current, setCurrent] = useState(initial)
  return (
    <>
      <MetadataPanel photo={current} canWrite onUpdated={setCurrent} />
      <ul aria-label="organization">
        {current.albums.map((album) => (
          <li key={album.uid}>{album.title}</li>
        ))}
        {current.labels.map((label) => (
          <li key={label.uid}>{label.name}</li>
        ))}
      </ul>
    </>
  )
}

function renderPanel(
  props: { photo?: PhotoDetail; onUpdated?: () => void; canWrite?: boolean } = {},
) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MetadataPanel
        photo={props.photo ?? photo()}
        canWrite={props.canWrite ?? true}
        onUpdated={props.onUpdated ?? vi.fn()}
      />
    </I18nextProvider>,
  )
}

function renderHarness(initial: PhotoDetail) {
  return render(
    <I18nextProvider i18n={i18n}>
      <DetailHarness initial={initial} />
    </I18nextProvider>,
  )
}

/**
 * Enters the shared caption form via a per-field edit affordance. There is no
 * global "Edit" button any more — each field is its own inline edit control — so
 * clicking any of them (here the title's) reveals the whole form.
 */
async function startEditing(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: 'Edit Title' }))
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

  it('writes the picked marker position back and PATCHes decimal degrees', async () => {
    const onUpdated = vi.fn()
    const current = photo()
    mockApi(current)
    const user = userEvent.setup()
    renderPanel({ photo: current, onUpdated })
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
    const current = photo()
    mockApi(current)
    const user = userEvent.setup()
    renderPanel({ photo: current })
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

describe('MetadataPanel per-field editing', () => {
  it('exposes a discoverable inline edit affordance for each caption field', () => {
    // The whole point of the rework: no hidden global "Edit" button — every
    // caption field is its own editable control an editor can find in place.
    renderPanel({ photo: photo({ title: 'Beach', description: '', ai_note: 'cat' }) })

    expect(screen.getByRole('button', { name: 'Edit Title' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Edit Description' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Edit AI note' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Edit Location' })).toBeInTheDocument()
    // No single global "Edit" button remains.
    expect(screen.queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument()
  })

  it('shows a muted "add…" placeholder for an empty field', () => {
    renderPanel({ photo: photo({ title: 'Beach', description: '' }) })
    // Title carries a value; the empty description invites adding one.
    expect(screen.getByRole('button', { name: 'Edit Description' })).toHaveTextContent('Add…')
    expect(screen.getByRole('button', { name: 'Edit Title' })).toHaveTextContent('Beach')
  })

  it('shows values read-only to a viewer with no edit affordances', () => {
    renderPanel({
      canWrite: false,
      photo: photo({ title: 'Beach', description: 'Sunny', ai_note: 'cat' }),
    })
    expect(screen.getByText('Beach')).toBeInTheDocument()
    expect(screen.getByText('Sunny')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Edit Title' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Edit Description' })).not.toBeInTheDocument()
  })

  it('shows the AI note read-only and PATCHes an edited value from its own field', async () => {
    const onUpdated = vi.fn()
    const current = photo({ ai_note: 'detected: dog, beach' })
    mockApi(current)
    const user = userEvent.setup()
    renderPanel({ photo: current, onUpdated })

    // The read-only summary shows the AI note under its own label.
    expect(screen.getByText('AI note')).toBeInTheDocument()
    expect(screen.getByText('detected: dog, beach')).toBeInTheDocument()

    // Its own inline affordance opens the shared form.
    await user.click(screen.getByRole('button', { name: 'Edit AI note' }))
    const field = screen.getByLabelText('AI note')
    expect(field).toHaveValue('detected: dog, beach')
    await user.clear(field)
    await user.type(field, 'detected: cat, sofa')

    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => {
      expect(updatePhotoMock).toHaveBeenCalledWith(
        'b',
        expect.objectContaining({ ai_note: 'detected: cat, sofa' }),
      )
    })
    expect(onUpdated).toHaveBeenCalled()
  })
})

describe('MetadataPanel approximate date', () => {
  it('offers the dating note only once the date is marked an estimate', async () => {
    const user = userEvent.setup()
    renderPanel()
    await startEditing(user)

    // An empty note on a photo whose date is a fact means nothing, so the field is
    // not there at all until the checkbox says otherwise.
    expect(screen.queryByLabelText('Dating note')).not.toBeInTheDocument()
    await user.click(screen.getByLabelText('Date is an estimate'))
    expect(screen.getByLabelText('Dating note')).toBeInTheDocument()
  })

  it('PATCHes the estimate flag together with the dating note', async () => {
    const current = photo({ taken_at: '1950-06-01T12:00:00Z' })
    mockApi(current)
    const user = userEvent.setup()
    renderPanel({ photo: current })
    await startEditing(user)

    await user.click(screen.getByLabelText('Date is an estimate'))
    await user.type(screen.getByLabelText('Dating note'), 'around 1950, before the wedding')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(updatePhotoMock).toHaveBeenCalled()
    })
    const patch = sentPatch()
    expect(patch.taken_at_estimated).toBe(true)
    expect(patch.taken_at_note).toBe('around 1950, before the wedding')
    // The date itself was not touched, so it stays out of the patch — resending it
    // would flip taken_at_source to manual.
    expect(patch).not.toHaveProperty('taken_at')
  })

  it('marks an estimated date with the circa marker, the note and an accessible title', () => {
    renderPanel({
      photo: photo({
        taken_at: '1950-06-01T12:00:00Z',
        taken_at_estimated: true,
        taken_at_note: 'around 1950',
      }),
    })

    const field = screen.getByRole('button', { name: 'Edit Taken at' })
    expect(field).toHaveTextContent('c.')
    expect(field).toHaveTextContent('around 1950')
    // Not glyph- or colour-only: the marker carries the note in its title.
    expect(screen.getByTitle('Estimated date, not an exact one: around 1950')).toBeInTheDocument()
  })

  it('marks an estimated date that has no capture time at all', () => {
    // The photo is undated — the note carries the whole meaning.
    renderPanel({
      photo: photo({ taken_at_estimated: true, taken_at_note: 'sometime in the forties' }),
    })

    const field = screen.getByRole('button', { name: 'Edit Taken at' })
    expect(field).toHaveTextContent('c.')
    expect(field).toHaveTextContent('sometime in the forties')
    expect(field).not.toHaveTextContent('Add…')
  })

  it('renders a known date without any circa marker', () => {
    renderPanel({ photo: photo({ taken_at: '2026-01-02T00:33:39Z' }) })

    const field = screen.getByRole('button', { name: 'Edit Taken at' })
    expect(field).not.toHaveTextContent('c.')
    expect(field).toHaveTextContent(new Date('2026-01-02T00:33:39Z').toLocaleString('en'))
    expect(screen.queryByTitle(/Estimated date/)).not.toBeInTheDocument()
  })

  it('drops the dating note when the estimate is unchecked', async () => {
    const current = photo({
      taken_at: '1950-06-01T12:00:00Z',
      taken_at_estimated: true,
      taken_at_note: 'around 1950',
    })
    mockApi(current)
    const user = userEvent.setup()
    renderHarness(current)

    await startEditing(user)
    // The note is in the form, prefilled, until the flag goes away.
    expect(screen.getByLabelText('Dating note')).toHaveValue('around 1950')
    await user.click(screen.getByLabelText('Date is an estimate'))
    expect(screen.queryByLabelText('Dating note')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => {
      expect(updatePhotoMock).toHaveBeenCalled()
    })
    const patch = sentPatch()
    expect(patch.taken_at_estimated).toBe(false)
    // The backend clears the note with the flag, so the form need not send one.
    expect(patch).not.toHaveProperty('taken_at_note')

    // And the refreshed photo shows a plain, unmarked date.
    const field = await screen.findByRole('button', { name: 'Edit Taken at' })
    expect(field).not.toHaveTextContent('c.')
    expect(field).not.toHaveTextContent('around 1950')
  })
})

describe('MetadataPanel saving', () => {
  it('keeps the detail page alive, with its albums and labels, after a save', async () => {
    const current = photo({
      albums: [{ uid: 'al1', title: 'Trip' }],
      labels: [{ uid: 'lb1', name: 'beach' }],
    })
    mockApi(current)
    const user = userEvent.setup()
    renderHarness(current)

    await user.click(screen.getByRole('button', { name: 'Edit Description' }))
    await user.type(screen.getByLabelText('Description'), 'Sunny day')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    // The page swaps in the PATCH response: it must still carry everything the
    // detail view renders, or mapping over albums/labels throws and blanks it.
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Edit Description' })).toHaveTextContent(
        'Sunny day',
      )
    })
    expect(screen.getByText('Trip')).toBeInTheDocument()
    expect(screen.getByText('beach')).toBeInTheDocument()
  })

  it('leaves an untouched capture time and coordinate out of the patch', async () => {
    // Resending them would rewrite the catalogue behind the user's back: taken_at
    // would flip taken_at_source exif → manual and drop the seconds, and the
    // coordinate would be rounded to the six decimals the text field shows.
    const current = photo({
      taken_at: '2026-01-02T00:33:39Z',
      lat: 49.1234567891011,
      lng: 16.7083583333333,
    })
    mockApi(current)
    const user = userEvent.setup()
    renderPanel({ photo: current })

    await startEditing(user)
    await user.type(screen.getByLabelText('Description'), 'Sunny day')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(updatePhotoMock).toHaveBeenCalled()
    })
    const patch = sentPatch()
    expect(patch.description).toBe('Sunny day')
    expect(patch).not.toHaveProperty('taken_at')
    expect(patch).not.toHaveProperty('lat')
    expect(patch).not.toHaveProperty('lng')
  })

  it('keeps the seconds of an edited capture time', async () => {
    const current = photo({ taken_at: '2026-01-02T00:33:39Z' })
    mockApi(current)
    const user = userEvent.setup()
    renderPanel({ photo: current })
    await startEditing(user)

    // The field itself carries seconds (it is step="1"), so editing the hour no
    // longer truncates 00:33:39 to 00:33:00.
    const field = screen.getByLabelText<HTMLInputElement>('Taken at')
    expect(field.value).toMatch(/:39(\.\d+)?$/)
    fireEvent.change(field, { target: { value: field.value.replace(/T\d\d:/, 'T05:') } })

    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => {
      expect(updatePhotoMock).toHaveBeenCalled()
    })
    const takenAt = sentPatch().taken_at
    expect(typeof takenAt).toBe('string')
    expect(new Date(String(takenAt)).getSeconds()).toBe(39)
  })

  it('reports an unparseable coordinate on the field and still saves the other fields', async () => {
    const current = photo()
    mockApi(current)
    const user = userEvent.setup()
    renderPanel({ photo: current })
    await startEditing(user)

    const coords = screen.getByLabelText('Coordinates')
    await user.clear(coords)
    await user.type(coords, 'nonsense')
    await user.type(screen.getByLabelText('Description'), 'Sunny day')

    // The field says why, and the marker has nowhere to go…
    expect(
      screen.getByText(
        'Unrecognised coordinates. Use decimal degrees, DMS or degrees-decimal-minutes.',
      ),
    ).toBeInTheDocument()
    expect(screen.getByTestId('marker')).toHaveTextContent('none')

    // …but the caption is not held hostage to it: it saves, the location does not,
    // and no generic "saving failed" alert appears.
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => {
      expect(updatePhotoMock).toHaveBeenCalled()
    })
    const patch = sentPatch()
    expect(patch.description).toBe('Sunny day')
    expect(patch).not.toHaveProperty('lat')
    expect(patch).not.toHaveProperty('lng')
    expect(screen.queryByText('Saving failed. Check the entered values.')).not.toBeInTheDocument()
    // The form stays open with the offending coordinate still on screen.
    expect(screen.getByLabelText('Coordinates')).toHaveValue('nonsense')
  })

  it('shows the save error when the API rejects the patch', async () => {
    updatePhotoMock.mockRejectedValue(new Error('boom'))
    const user = userEvent.setup()
    renderPanel()
    await startEditing(user)
    await user.type(screen.getByLabelText('Description'), 'Sunny day')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    expect(await screen.findByText('Saving failed. Check the entered values.')).toBeInTheDocument()
  })
})

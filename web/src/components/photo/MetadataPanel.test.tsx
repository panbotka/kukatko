import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { ApiError } from '../../services/auth'
import { type Place } from '../../services/map'
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
vi.mock('../../services/map', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/map')>()
  return { ...actual, searchPlaces: vi.fn() }
})

const { updatePhoto } = await import('../../services/photos')
const updatePhotoMock = vi.mocked(updatePhoto)
const { searchPlaces } = await import('../../services/map')
const searchPlacesMock = vi.mocked(searchPlaces)

/** The place the scanned-photo case is named after: you know the name, not the numbers. */
const VESELI: Place = {
  name: 'Veselí nad Moravou',
  label: 'Town',
  type: 'regional.municipality',
  location: 'Czechia',
  lat: 48.95363,
  lng: 17.37649,
}

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
    subject: patch.subject ?? current.subject,
    artist: patch.artist ?? current.artist,
    copyright: patch.copyright ?? current.copyright,
    license: patch.license ?? current.license,
    keywords: patch.keywords ?? current.keywords,
    scan: patch.scan ?? current.scan,
    lat: patch.lat === undefined ? current.lat : (patch.lat ?? undefined),
    lng: patch.lng === undefined ? current.lng : (patch.lng ?? undefined),
    // Provenance follows the coordinates: touching either stamps the location the
    // user's own, whether they moved it or cleared it. Clearing deliberately keeps
    // "manual" rather than reverting to unknown — that is the tombstone that stops
    // the backfill re-estimating what the user threw away.
    location_source:
      patch.location_source ??
      (patch.lat === undefined && patch.lng === undefined ? current.location_source : 'manual'),
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

/**
 * Opens the credit sub-section of the already-open form. It starts collapsed, so
 * the six credit inputs exist only once an editor asks for them.
 */
async function openCredits(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: 'Credit and keywords' }))
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.clearAllMocks()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('MetadataPanel edit form', () => {
  it('keeps Save and Cancel together in the actions bar', async () => {
    // The actions live in kk-viewer__panel-actions so the viewer can pin them to
    // the drawer's footer while the long form scrolls — a quick edit needs no
    // scroll to reach Save. Without a footer node (here) the bar renders inline;
    // this guards that the two controls live in that bar, not loose in the form.
    const user = userEvent.setup()
    const { container } = renderPanel()
    await startEditing(user)

    const actions = container.querySelector('.kk-viewer__panel-actions')
    expect(actions).not.toBeNull()
    // Buttons drive save/cancel directly (not a form submit) so they still work
    // when portaled out of the form into the footer.
    expect(actions?.querySelectorAll('button')).toHaveLength(2)
    expect(actions?.textContent).toContain('Save')
    expect(actions?.textContent).toContain('Cancel')
  })

  it('portals the actions into a provided footer node', async () => {
    // With a footer node the bar renders THERE (pinned outside the scroll body),
    // not inline in the form — this is what keeps Save reachable on a tall screen.
    const user = userEvent.setup()
    const footer = document.createElement('div')
    document.body.append(footer)
    try {
      const { container } = render(
        <I18nextProvider i18n={i18n}>
          <MetadataPanel photo={photo()} canWrite onUpdated={vi.fn()} footer={footer} />
        </I18nextProvider>,
      )
      await user.click(screen.getByRole('button', { name: 'Edit Title' }))

      expect(footer.querySelector('.kk-viewer__panel-actions')).not.toBeNull()
      expect(footer.textContent).toContain('Save')
      // The form itself no longer carries the actions bar when they are portaled out.
      expect(container.querySelector('.kk-viewer__panel-actions')).toBeNull()
    } finally {
      footer.remove()
    }
  })
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

  it('fills the coordinates and moves the map from a searched-for place name', async () => {
    searchPlacesMock.mockResolvedValue([VESELI])
    const current = photo()
    mockApi(current)
    const user = userEvent.setup()
    renderPanel({ photo: current })
    await startEditing(user)

    await user.type(screen.getByLabelText('Find a place by name'), 'Veselí')
    await user.click(await screen.findByRole('option', { name: /Veselí nad Moravou/ }))

    // The search writes the same field the map click does, so both the coordinate
    // text and the marker follow from one pick.
    expect(screen.getByLabelText('Coordinates')).toHaveValue('48.953630, 17.376490')
    expect(screen.getByTestId('marker')).toHaveTextContent('48.95363,17.37649')

    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => {
      expect(updatePhotoMock).toHaveBeenCalledWith(
        'b',
        expect.objectContaining({ lat: 48.95363, lng: 17.37649 }),
      )
    })
  })

  it('keeps the rest of the location editor working when place search is down', async () => {
    searchPlacesMock.mockRejectedValue(new ApiError(503, 'place search is not configured'))
    const current = photo()
    mockApi(current)
    const user = userEvent.setup()
    renderPanel({ photo: current })
    await startEditing(user)

    await user.type(screen.getByLabelText('Find a place by name'), 'Veselí')
    expect(
      await screen.findByText(
        'Place search is unavailable. You can still type coordinates or click the map.',
      ),
    ).toBeInTheDocument()

    // A dead place search is one line of text, not a broken editor: the map click
    // still sets a location and the form still saves it.
    await user.click(screen.getByRole('button', { name: 'drop-marker' }))
    expect(screen.getByLabelText('Coordinates')).toHaveValue('51.500000, -0.120000')
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => {
      expect(updatePhotoMock).toHaveBeenCalledWith(
        'b',
        expect.objectContaining({ lat: 51.5, lng: -0.12 }),
      )
    })
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
    expect(screen.getByRole('button', { name: 'Edit Automatic description' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Edit Location' })).toBeInTheDocument()
    // No single global "Edit" button remains.
    expect(screen.queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument()
  })

  it('prompts an empty field with what that field is for, not a generic "add…"', () => {
    renderPanel({ photo: photo({ title: 'Beach', description: '' }) })
    // Title carries a value; the empty description invites adding one — in words
    // about descriptions.
    const description = screen.getByRole('button', { name: 'Edit Description' })
    expect(description).toHaveTextContent('What is happening here, and why it is worth remembering')
    expect(description).not.toHaveTextContent('Add…')
    expect(screen.getByRole('button', { name: 'Edit Title' })).toHaveTextContent('Beach')
  })

  it('gives every empty field its own prompt rather than one repeated placeholder', () => {
    // The regression this guards: four fields sharing one "Add…" is a column of
    // identical grey text that says nothing about any of them.
    renderPanel({ photo: photo({ title: '', description: '', ai_note: '', notes: '' }) })
    const prompts = (['Title', 'Description', 'Automatic description', 'Notes'] as const).map(
      (field) => screen.getByRole('button', { name: `Edit ${field}` }).textContent,
    )
    expect(new Set(prompts).size).toBe(prompts.length)
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

    // The read-only summary shows the automatic description under its own label.
    expect(screen.getByText('Automatic description')).toBeInTheDocument()
    expect(screen.getByText('detected: dog, beach')).toBeInTheDocument()

    // Its own inline affordance opens the shared form.
    await user.click(screen.getByRole('button', { name: 'Edit Automatic description' }))
    const field = screen.getByLabelText('Automatic description')
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
    expect(field).not.toHaveTextContent('For example 24 December 1998')
  })

  it('renders a known date to the minute, without any circa marker or seconds', () => {
    renderPanel({ photo: photo({ taken_at: '2026-01-02T00:33:39Z' }) })

    const field = screen.getByRole('button', { name: 'Edit Taken at' })
    expect(field).not.toHaveTextContent('c.')
    expect(field).toHaveTextContent(
      new Date('2026-01-02T00:33:39Z').toLocaleString('en', {
        year: 'numeric',
        month: 'numeric',
        day: 'numeric',
        hour: 'numeric',
        minute: '2-digit',
      }),
    )
    // Nobody needs the second a photo was taken on; it stays in technical details.
    expect(field).not.toHaveTextContent(':39')
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

describe('MetadataPanel credit fields', () => {
  it('keeps the credit sub-section collapsed until it is asked for', async () => {
    const user = userEvent.setup()
    renderPanel()
    await startEditing(user)

    // Typing a title must not mean scrolling past six more inputs first.
    expect(screen.queryByLabelText('Subject')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Keywords')).not.toBeInTheDocument()
    const toggle = screen.getByRole('button', { name: 'Credit and keywords' })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')

    await openCredits(user)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByLabelText('Subject')).toBeInTheDocument()
    expect(screen.getByLabelText('Artist')).toBeInTheDocument()
    expect(screen.getByLabelText('Copyright')).toBeInTheDocument()
    expect(screen.getByLabelText('Licence')).toBeInTheDocument()
    expect(screen.getByLabelText('Keywords')).toBeInTheDocument()
    expect(screen.getByLabelText('Scan')).toBeInTheDocument()
  })

  it('sends every edited credit field in the form’s single PATCH', async () => {
    const current = photo()
    mockApi(current)
    const user = userEvent.setup()
    renderPanel({ photo: current })
    await startEditing(user)
    await openCredits(user)

    await user.type(screen.getByLabelText('Subject'), 'Wedding at the mill')
    await user.type(screen.getByLabelText('Artist'), 'Josef Kozák')
    await user.type(screen.getByLabelText('Copyright'), '© Kozák family')
    await user.type(screen.getByLabelText('Licence'), 'CC BY-SA 4.0')
    await user.type(screen.getByLabelText('Keywords'), 'wedding{Enter}')
    await user.click(screen.getByLabelText('Scan'))
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(updatePhotoMock).toHaveBeenCalled()
    })
    // One PATCH, carrying the lot — no second form and no second request.
    expect(sentPatch()).toMatchObject({
      subject: 'Wedding at the mill',
      artist: 'Josef Kozák',
      copyright: '© Kozák family',
      license: 'CC BY-SA 4.0',
      keywords: 'wedding',
      scan: true,
    })
  })

  it('adds keyword chips on Enter and on a comma, de-duplicates and removes on click', async () => {
    const current = photo({ keywords: 'beach' })
    mockApi(current)
    const user = userEvent.setup()
    renderPanel({ photo: current })
    await startEditing(user)
    await openCredits(user)

    // The photo's own keyword is already a chip.
    expect(screen.getByRole('button', { name: 'Remove keyword beach' })).toBeInTheDocument()

    const input = screen.getByLabelText('Keywords')
    await user.type(input, 'sunset{Enter}')
    await user.type(input, 'boat,')
    // Already there — it does not become a second chip.
    await user.type(input, 'beach{Enter}')
    expect(screen.getAllByRole('button', { name: /^Remove keyword/ })).toHaveLength(3)
    expect(input).toHaveValue('')

    await user.click(screen.getByRole('button', { name: 'Remove keyword sunset' }))
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(updatePhotoMock).toHaveBeenCalled()
    })
    expect(sentPatch().keywords).toBe('beach, boat')
  })

  it('takes the last keyword back on backspace in an empty input', async () => {
    const current = photo({ keywords: 'beach, sunset' })
    mockApi(current)
    const user = userEvent.setup()
    renderPanel({ photo: current })
    await startEditing(user)
    await openCredits(user)

    const input = screen.getByLabelText('Keywords')
    input.focus()
    await user.keyboard('{Backspace}')
    expect(screen.queryByRole('button', { name: 'Remove keyword sunset' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => {
      expect(updatePhotoMock).toHaveBeenCalled()
    })
    expect(sentPatch().keywords).toBe('beach')
  })

  it('commits a typed keyword that was never entered rather than dropping it', async () => {
    const current = photo()
    mockApi(current)
    const user = userEvent.setup()
    renderPanel({ photo: current })
    await startEditing(user)
    await openCredits(user)

    // Typed, then straight to Save — the field commits on blur, so the keyword lives.
    await user.type(screen.getByLabelText('Keywords'), 'wedding')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(updatePhotoMock).toHaveBeenCalled()
    })
    expect(sentPatch().keywords).toBe('wedding')
  })

  it('clears an emptied credit field and leaves the untouched ones out of the patch', async () => {
    const current = photo({ subject: 'Wedding', artist: 'Josef Kozák', keywords: 'beach' })
    mockApi(current)
    const user = userEvent.setup()
    renderPanel({ photo: current })
    await startEditing(user)
    await openCredits(user)

    await user.clear(screen.getByLabelText('Subject'))
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(updatePhotoMock).toHaveBeenCalled()
    })
    const patch = sentPatch()
    // An emptied field is cleared, not left as it was…
    expect(patch.subject).toBe('')
    // …and an untouched one is not resent, so the form's normalisation ("beach" →
    // "beach") never rewrites what the source file wrote.
    expect(patch).not.toHaveProperty('artist')
    expect(patch).not.toHaveProperty('keywords')
    expect(patch).not.toHaveProperty('scan')
  })

  it('keeps the typed credit values in the form when the PATCH fails', async () => {
    updatePhotoMock.mockRejectedValue(new Error('boom'))
    const user = userEvent.setup()
    renderPanel()
    await startEditing(user)
    await openCredits(user)

    await user.type(screen.getByLabelText('Artist'), 'Josef Kozák')
    await user.type(screen.getByLabelText('Keywords'), 'wedding{Enter}')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    // The form's own error surface, and not a keystroke of the input thrown away.
    expect(await screen.findByText('Saving failed. Check the entered values.')).toBeInTheDocument()
    expect(screen.getByLabelText('Artist')).toHaveValue('Josef Kozák')
    expect(screen.getByRole('button', { name: 'Remove keyword wedding' })).toBeInTheDocument()
  })

  it('offers a viewer no credit editing UI at all', () => {
    renderPanel({
      canWrite: false,
      photo: photo({ subject: 'Wedding', artist: 'Josef Kozák', keywords: 'beach' }),
    })

    expect(screen.queryByRole('button', { name: 'Credit and keywords' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Subject')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Keywords')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Scan')).not.toBeInTheDocument()
  })
})

describe('MetadataPanel estimated location', () => {
  it('marks an estimated location distinctly, with an explanation', async () => {
    const current = photo({ lat: 50.09, lng: 14.4, location_source: 'estimate' })
    mockApi(current)
    renderPanel({ photo: current })

    // A badge in words, not a shade or a glyph: the marker has to survive both a
    // glance and a screen reader.
    const badge = await screen.findByTitle(
      'Estimated from photos taken the same day, not a measured position',
    )
    expect(badge).toHaveTextContent('estimate')
    expect(screen.getByText('Estimated from photos taken the same day.')).toBeInTheDocument()
  })

  it('leaves a measured location unmarked', () => {
    const current = photo({ lat: 50.09, lng: 14.4, location_source: 'exif' })
    mockApi(current)
    renderPanel({ photo: current })

    expect(screen.queryByText('Estimated from photos taken the same day.')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Accept estimate' })).not.toBeInTheDocument()
  })

  it('leaves a location of unknown provenance unmarked', () => {
    // An older row carries no source. It is not an estimate, so it must not be
    // labelled one — "we don't know" is not "we guessed".
    const current = photo({ lat: 50.09, lng: 14.4, location_source: '' })
    mockApi(current)
    renderPanel({ photo: current })

    expect(screen.queryByText('Estimated from photos taken the same day.')).not.toBeInTheDocument()
  })

  it('offers accept and discard to an editor', () => {
    const current = photo({ lat: 50.09, lng: 14.4, location_source: 'estimate' })
    mockApi(current)
    renderPanel({ photo: current })

    expect(screen.getByRole('button', { name: 'Accept estimate' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Discard estimate' })).toBeInTheDocument()
  })

  it('offers neither to a viewer, who may not decide', () => {
    const current = photo({ lat: 50.09, lng: 14.4, location_source: 'estimate' })
    mockApi(current)
    renderPanel({ photo: current, canWrite: false })

    // The marker still shows — a viewer deserves to know the pin is a guess — but
    // the actions do not.
    expect(screen.getByText('Estimated from photos taken the same day.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Accept estimate' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Discard estimate' })).not.toBeInTheDocument()
  })

  it('accepts the estimate by promoting the source, never resending coordinates', async () => {
    const current = photo({ lat: 50.09, lng: 14.4, location_source: 'estimate' })
    mockApi(current)
    const user = userEvent.setup()
    renderHarness(current)

    await user.click(screen.getByRole('button', { name: 'Accept estimate' }))
    await waitFor(() => {
      expect(updatePhotoMock).toHaveBeenCalled()
    })

    const patch = sentPatch()
    expect(patch.location_source).toBe('manual')
    // Echoing the coordinates back would round them to the six decimals the form
    // renders, moving the pin as the price of agreeing with it.
    expect(patch).not.toHaveProperty('lat')
    expect(patch).not.toHaveProperty('lng')

    // And the accepted location is a plain one: no badge, no buttons.
    await waitFor(() => {
      expect(
        screen.queryByText('Estimated from photos taken the same day.'),
      ).not.toBeInTheDocument()
    })
    expect(screen.queryByRole('button', { name: 'Accept estimate' })).not.toBeInTheDocument()
  })

  it('discards the estimate by clearing the coordinates', async () => {
    const current = photo({ lat: 50.09, lng: 14.4, location_source: 'estimate' })
    mockApi(current)
    const user = userEvent.setup()
    renderHarness(current)

    await user.click(screen.getByRole('button', { name: 'Discard estimate' }))
    await waitFor(() => {
      expect(updatePhotoMock).toHaveBeenCalled()
    })

    const patch = sentPatch()
    expect(patch.lat).toBeNull()
    expect(patch.lng).toBeNull()

    // The backend records the clear as a decision ("manual", no coordinates), which
    // is what stops the backfill offering the same guess again tomorrow.
    await waitFor(() => {
      expect(
        screen.queryByText('Estimated from photos taken the same day.'),
      ).not.toBeInTheDocument()
    })
  })

  it('reports a failed accept instead of pretending it worked', async () => {
    const current = photo({ lat: 50.09, lng: 14.4, location_source: 'estimate' })
    updatePhotoMock.mockRejectedValue(new Error('nope'))
    const user = userEvent.setup()
    renderHarness(current)

    await user.click(screen.getByRole('button', { name: 'Accept estimate' }))

    expect(
      await screen.findByText('The location could not be saved. Please try again.'),
    ).toBeInTheDocument()
    // The estimate is still an estimate, and still actionable.
    expect(screen.getByRole('button', { name: 'Accept estimate' })).toBeEnabled()
  })
})

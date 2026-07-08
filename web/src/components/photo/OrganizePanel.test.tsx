import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type AlbumCount, type LabelCount } from '../../services/organize'
import { type PhotoDetail } from '../../services/photos'

import { OrganizePanel } from './OrganizePanel'

vi.mock('../../services/organize', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/organize')>()
  return {
    ...actual,
    fetchAlbums: vi.fn(),
    fetchLabels: vi.fn(),
    addAlbumPhotos: vi.fn(),
    attachLabel: vi.fn(),
  }
})

const { fetchAlbums, fetchLabels, addAlbumPhotos, attachLabel } =
  await import('../../services/organize')
const fetchAlbumsMock = vi.mocked(fetchAlbums)
const fetchLabelsMock = vi.mocked(fetchLabels)
const addAlbumPhotosMock = vi.mocked(addAlbumPhotos)
const attachLabelMock = vi.mocked(attachLabel)

function album(uid: string, title: string): AlbumCount {
  return {
    uid,
    slug: uid,
    title,
    description: '',
    type: 'album',
    private: false,
    order_by: 'added',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: 0,
  }
}

function label(uid: string, name: string): LabelCount {
  return {
    uid,
    slug: uid,
    name,
    priority: 0,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: 0,
  }
}

function photo(overrides: Partial<PhotoDetail> = {}): PhotoDetail {
  return {
    uid: 'p1',
    file_hash: 'p1',
    file_name: 'p1.jpg',
    file_size: 1,
    file_mime: 'image/jpeg',
    file_width: 1,
    file_height: 1,
    taken_at_source: 'exif',
    title: 'Photo',
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

function renderPanel(props: {
  photo?: PhotoDetail
  canWrite?: boolean
  onChanged?: (photo: PhotoDetail) => void
}) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <OrganizePanel
          photo={props.photo ?? photo()}
          canWrite={props.canWrite ?? true}
          onChanged={props.onChanged ?? vi.fn()}
        />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.clearAllMocks()
  fetchAlbumsMock.mockResolvedValue([
    album('a1', 'Holidays'),
    album('a2', 'Náměstí'),
    album('a3', 'Work'),
  ])
  fetchLabelsMock.mockResolvedValue([label('l1', 'sunset'), label('l2', 'winter')])
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('OrganizePanel autocomplete', () => {
  it('filters album suggestions case- and accent-insensitively as the user types', async () => {
    const user = userEvent.setup()
    renderPanel({})

    const input = await screen.findByRole('combobox', { name: 'Add to album' })
    await user.type(input, 'namesti')

    const listbox = await screen.findByRole('listbox', { name: 'Add to album' })
    const options = within(listbox).getAllByRole('option')
    expect(options).toHaveLength(1)
    expect(options[0]).toHaveTextContent('Náměstí')
  })

  it('does not suggest albums the photo is already in', async () => {
    const user = userEvent.setup()
    renderPanel({ photo: photo({ albums: [{ uid: 'a1', title: 'Holidays' }] }) })

    const input = await screen.findByRole('combobox', { name: 'Add to album' })
    await user.type(input, 'holi')

    expect(await screen.findByText('No matches.')).toBeInTheDocument()
  })

  it('adds the photo to a clicked album, updates chips and clears the input', async () => {
    const onChanged = vi.fn()
    addAlbumPhotosMock.mockResolvedValue(['p1'])
    const user = userEvent.setup()
    renderPanel({ onChanged })

    const input = await screen.findByRole('combobox', { name: 'Add to album' })
    await user.type(input, 'work')
    await user.click(await screen.findByRole('option', { name: 'Work' }))

    await waitFor(() => {
      expect(addAlbumPhotosMock).toHaveBeenCalledWith('a3', ['p1'])
    })
    expect(onChanged).toHaveBeenCalledWith(
      expect.objectContaining({ albums: [{ uid: 'a3', title: 'Work' }] }),
    )
    expect(input).toHaveValue('')
  })

  it('selects a suggestion with the keyboard (ArrowDown + Enter)', async () => {
    const onChanged = vi.fn()
    addAlbumPhotosMock.mockResolvedValue(['p1'])
    const user = userEvent.setup()
    renderPanel({ onChanged })

    const input = await screen.findByRole('combobox', { name: 'Add to album' })
    await user.type(input, 'o') // matches "Holidays" and "Work"
    await screen.findByRole('listbox', { name: 'Add to album' })

    await user.keyboard('{ArrowDown}{ArrowDown}{Enter}')

    await waitFor(() => {
      expect(addAlbumPhotosMock).toHaveBeenCalledTimes(1)
    })
    // Two matches sorted as fetched: Holidays (a1), Work (a3); second is Work.
    expect(addAlbumPhotosMock).toHaveBeenCalledWith('a3', ['p1'])
  })

  it('closes the suggestion list on Escape', async () => {
    const user = userEvent.setup()
    renderPanel({})

    const input = await screen.findByRole('combobox', { name: 'Add to album' })
    await user.type(input, 'work')
    expect(await screen.findByRole('listbox', { name: 'Add to album' })).toBeInTheDocument()

    await user.keyboard('{Escape}')
    expect(screen.queryByRole('listbox', { name: 'Add to album' })).not.toBeInTheDocument()
  })

  it('applies the same autocomplete to labels', async () => {
    const onChanged = vi.fn()
    attachLabelMock.mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderPanel({ onChanged })

    const input = await screen.findByRole('combobox', { name: 'Add label' })
    await user.type(input, 'sun')
    await user.click(await screen.findByRole('option', { name: 'sunset' }))

    await waitFor(() => {
      expect(attachLabelMock).toHaveBeenCalledWith('l1', 'p1')
    })
    expect(onChanged).toHaveBeenCalledWith(
      expect.objectContaining({ labels: [{ uid: 'l1', name: 'sunset' }] }),
    )
  })

  it('hides the add controls from viewers', async () => {
    renderPanel({ canWrite: false })
    // Give any (skipped) fetch a tick; controls must never appear for viewers.
    await Promise.resolve()
    expect(screen.queryByRole('combobox', { name: 'Add to album' })).not.toBeInTheDocument()
    expect(screen.queryByRole('combobox', { name: 'Add label' })).not.toBeInTheDocument()
    expect(fetchAlbumsMock).not.toHaveBeenCalled()
  })
})

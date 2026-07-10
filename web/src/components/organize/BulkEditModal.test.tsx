import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../../auth/AuthContext'
import i18n from '../../i18n'
import { ApiError } from '../../services/auth'
import { type BulkResult } from '../../services/bulk'
import { type AlbumCount, type LabelCount } from '../../services/organize'

import { BulkEditModal } from './BulkEditModal'

vi.mock('../../services/bulk', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/bulk')>()
  return { ...actual, bulkUpdatePhotos: vi.fn() }
})
vi.mock('../../services/organize', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/organize')>()
  return {
    ...actual,
    fetchAlbums: vi.fn(),
    fetchLabels: vi.fn(),
    createAlbum: vi.fn(),
    createLabel: vi.fn(),
  }
})

const { bulkUpdatePhotos } = await import('../../services/bulk')
const { fetchAlbums, fetchLabels, createAlbum, createLabel } =
  await import('../../services/organize')
const bulkMock = vi.mocked(bulkUpdatePhotos)
const albumsMock = vi.mocked(fetchAlbums)
const labelsMock = vi.mocked(fetchLabels)
const createAlbumMock = vi.mocked(createAlbum)
const createLabelMock = vi.mocked(createLabel)

function album(uid: string, title: string): AlbumCount {
  return {
    uid,
    slug: title.toLowerCase(),
    title,
    description: '',
    type: 'album',
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: 0,
  }
}

function label(uid: string, name: string): LabelCount {
  return {
    uid,
    slug: name.toLowerCase(),
    name,
    priority: 0,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: 0,
  }
}

function result(
  counts: Partial<BulkResult['counts']>,
  results: BulkResult['results'] = [],
): BulkResult {
  return {
    results,
    counts: { total: 0, updated: 0, skipped: 0, errored: 0, ...counts },
  }
}

const onHide = vi.fn()
const onDone = vi.fn()

/** A signed-in editor by default; a viewer when `canWrite` is false. */
function auth(canWrite: boolean): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'U', role: canWrite ? 'editor' : 'viewer' },
    role: canWrite ? 'editor' : 'viewer',
    downloadToken: null,
    canWrite,
    isAdmin: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

function renderModal(photoUids = ['ph1', 'ph2'], canWrite = true) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <BulkEditModal show photoUids={photoUids} onHide={onHide} onDone={onDone} />
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/**
 * Types `query` into a multi-select and picks the option whose label matches.
 * The field is found by role, not by label text: an open listbox carries the
 * same accessible name as the input it belongs to.
 */
async function pick(user: ReturnType<typeof userEvent.setup>, field: string, query: string) {
  const input = await screen.findByRole('combobox', { name: field })
  await user.clear(input)
  await user.type(input, query)
  const listbox = screen.getByRole('listbox', { name: field })
  await user.click(within(listbox).getByRole('option', { name: new RegExp(`^${query}`, 'i') }))
}

/** Types a brand-new `name` into a multi-select and picks its create entry. */
async function pickCreate(user: ReturnType<typeof userEvent.setup>, field: string, name: string) {
  const input = await screen.findByRole('combobox', { name: field })
  await user.clear(input)
  await user.type(input, name)
  const listbox = screen.getByRole('listbox', { name: field })
  await user.click(within(listbox).getByRole('option', { name: `Create “${name}”` }))
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  bulkMock.mockReset()
  albumsMock.mockReset()
  labelsMock.mockReset()
  createAlbumMock.mockReset()
  createLabelMock.mockReset()
  onHide.mockReset()
  onDone.mockReset()
  albumsMock.mockResolvedValue([album('al1', 'Trips'), album('al2', 'Weddings')])
  labelsMock.mockResolvedValue([label('lb1', 'Sunset'), label('lb2', 'Léto')])
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('BulkEditModal', () => {
  it('applies the selected operations with the right payload and renders the result', async () => {
    bulkMock.mockResolvedValue(result({ total: 2, updated: 2 }))
    const user = userEvent.setup()
    renderModal()

    // Wait for the album/label options to load.
    await pick(user, 'Add to albums', 'Trips')
    await user.selectOptions(screen.getByLabelText('Private'), 'true')
    await user.selectOptions(screen.getByLabelText('Description'), 'set')
    await user.type(await screen.findByLabelText('New description…'), 'Hello')

    await user.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledTimes(1)
    })
    expect(bulkMock).toHaveBeenCalledWith(['ph1', 'ph2'], {
      add_to_albums: ['al1'],
      set_private: true,
      set_description: 'Hello',
    })

    // The per-photo result summary replaces the form.
    expect(await screen.findByText(/2 updated/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Done' }))
    expect(onDone).toHaveBeenCalledTimes(1)
  })

  it('renders as a fullscreen sheet on small viewports', async () => {
    renderModal()
    // react-bootstrap maps `fullscreen="sm-down"` to this dialog class, which the
    // Bootstrap stylesheet turns into a full-screen sheet below the `sm`
    // breakpoint (phones) while staying a centered dialog on larger screens.
    await screen.findByRole('dialog')
    expect(document.querySelector('.modal-dialog')?.className).toContain('modal-fullscreen-sm-down')
  })

  it('blocks applying with no operations chosen', async () => {
    const user = userEvent.setup()
    renderModal()

    await screen.findByRole('combobox', { name: 'Add to albums' })
    expect(screen.getByText('Nothing chosen yet.')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    expect(await screen.findByText('Choose at least one change to apply.')).toBeInTheDocument()
    expect(bulkMock).not.toHaveBeenCalled()
  })

  it('lists per-photo failures in the result summary', async () => {
    bulkMock.mockResolvedValue(
      result({ total: 2, updated: 1, errored: 1 }, [
        { photo_uid: 'ph1', status: 'updated' },
        { photo_uid: 'ph2', status: 'error', error: 'not found' },
      ]),
    )
    const user = userEvent.setup()
    renderModal()

    await user.selectOptions(await screen.findByLabelText('Archive'), 'archive')
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    expect(await screen.findByText('ph2')).toBeInTheDocument()
    expect(screen.getByText(/not found/)).toBeInTheDocument()
    expect(bulkMock).toHaveBeenCalledWith(['ph1', 'ph2'], { archive: true })
  })

  it('filters the options as the reader types, case- and accent-insensitively', async () => {
    const user = userEvent.setup()
    renderModal()

    const addLabels = await screen.findByRole('combobox', { name: 'Add labels' })
    await user.type(addLabels, 'leto')

    const listbox = screen.getByRole('listbox', { name: 'Add labels' })
    expect(within(listbox).getByRole('option', { name: /^Léto/ })).toBeInTheDocument()
    expect(within(listbox).queryByRole('option', { name: /^Sunset/ })).not.toBeInTheDocument()

    // A query nothing matches offers to create the entry instead of a stale
    // list (this field belongs to an editor; a viewer gets "No matches.").
    await user.clear(addLabels)
    await user.type(addLabels, 'zzz')
    expect(within(listbox).queryByRole('option', { name: /^Sunset|^Léto/ })).not.toBeInTheDocument()
    expect(within(listbox).getByRole('option', { name: 'Create “zzz”' })).toBeInTheDocument()
  })

  it('adds and removes several albums and labels in one apply', async () => {
    bulkMock.mockResolvedValue(result({ total: 2, updated: 2 }))
    const user = userEvent.setup()
    renderModal()

    await pick(user, 'Add to albums', 'Trips')
    await pick(user, 'Add to albums', 'Weddings')
    await pick(user, 'Remove from albums', 'Trips')
    await pick(user, 'Add labels', 'Sunset')
    await pick(user, 'Add labels', 'Léto')
    await pick(user, 'Remove labels', 'Sunset')

    // Every pick is a chip, and the summary states the whole batch in prose.
    expect(screen.getByText('Add to albums: Trips, Weddings')).toBeInTheDocument()
    expect(screen.getByText('Remove from albums: Trips')).toBeInTheDocument()
    expect(screen.getByText('Add labels: Sunset, Léto')).toBeInTheDocument()
    expect(screen.getByText('Remove labels: Sunset')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['ph1', 'ph2'], {
        add_to_albums: ['al1', 'al2'],
        remove_from_albums: ['al1'],
        add_labels: ['lb1', 'lb2'],
        remove_labels: ['lb1'],
      })
    })
  })

  it('drops a chosen album again when its chip is dismissed', async () => {
    const user = userEvent.setup()
    renderModal()

    await pick(user, 'Add to albums', 'Trips')
    expect(screen.getByText('Add to albums: Trips')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Remove Trips' }))
    expect(screen.getByText('Nothing chosen yet.')).toBeInTheDocument()
  })

  it('requires an explicit confirmation for a selection larger than 50 photos', async () => {
    bulkMock.mockResolvedValue(result({ total: 51, updated: 51 }))
    const user = userEvent.setup()
    const many = Array.from({ length: 51 }, (_, i) => `ph${String(i)}`)
    renderModal(many)

    expect(await screen.findByText('Applies to 51 selected photos.')).toBeInTheDocument()
    await user.selectOptions(screen.getByLabelText('Archive'), 'archive')
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    // The first Apply only asks; nothing has been sent yet.
    expect(bulkMock).not.toHaveBeenCalled()
    expect(screen.getByText(/This change affects 51 photos at once/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Yes, apply to 51 photos' }))
    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(many, { archive: true })
    })
  })

  it('withdraws a granted confirmation when the form changes again', async () => {
    const user = userEvent.setup()
    renderModal(Array.from({ length: 51 }, (_, i) => `ph${String(i)}`))

    await user.selectOptions(await screen.findByLabelText('Archive'), 'archive')
    await user.click(screen.getByRole('button', { name: 'Apply' }))
    expect(screen.getByRole('button', { name: 'Yes, apply to 51 photos' })).toBeInTheDocument()

    await user.selectOptions(screen.getByLabelText('Favorite'), 'true')
    expect(
      screen.queryByRole('button', { name: 'Yes, apply to 51 photos' }),
    ).not.toBeInTheDocument()
    expect(bulkMock).not.toHaveBeenCalled()
  })

  it('applies a small selection without asking for confirmation', async () => {
    bulkMock.mockResolvedValue(result({ total: 50, updated: 50 }))
    const user = userEvent.setup()
    const fifty = Array.from({ length: 50 }, (_, i) => `ph${String(i)}`)
    renderModal(fifty)

    await user.selectOptions(await screen.findByLabelText('Archive'), 'archive')
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(fifty, { archive: true })
    })
  })

  it('creates a picked new album and label on apply, then assigns the photos', async () => {
    createAlbumMock.mockResolvedValue(album('al9', 'Dovolená'))
    createLabelMock.mockResolvedValue(label('lb9', 'Zima'))
    bulkMock.mockResolvedValue(result({ total: 2, updated: 2 }))
    const user = userEvent.setup()
    renderModal()

    await pickCreate(user, 'Add to albums', 'Dovolená')
    // The new entry is selected in the field immediately, before any request.
    expect(screen.getByRole('button', { name: 'Remove Dovolená' })).toBeInTheDocument()
    expect(createAlbumMock).not.toHaveBeenCalled()
    await pickCreate(user, 'Add labels', 'Zima')

    // The summary already states the batch under the new names.
    expect(screen.getByText('Add to albums: Dovolená')).toBeInTheDocument()
    expect(screen.getByText('Add labels: Zima')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledTimes(1)
    })
    expect(createAlbumMock).toHaveBeenCalledWith({
      title: 'Dovolená',
      description: '',
      private: false,
    })
    expect(createLabelMock).toHaveBeenCalledWith({ name: 'Zima', priority: 0 })
    // The entities exist before the batch that references them is sent.
    expect(createAlbumMock.mock.invocationCallOrder[0]).toBeLessThan(
      bulkMock.mock.invocationCallOrder[0],
    )
    expect(createLabelMock.mock.invocationCallOrder[0]).toBeLessThan(
      bulkMock.mock.invocationCallOrder[0],
    )
    expect(bulkMock).toHaveBeenCalledWith(['ph1', 'ph2'], {
      add_to_albums: ['al9'],
      add_labels: ['lb9'],
    })
    expect(await screen.findByText(/2 updated/)).toBeInTheDocument()
  })

  it('offers the existing entry instead of creating a folded duplicate', async () => {
    const user = userEvent.setup()
    renderModal()

    const input = await screen.findByRole('combobox', { name: 'Add to albums' })
    await user.type(input, ' trips ')

    const listbox = screen.getByRole('listbox', { name: 'Add to albums' })
    expect(within(listbox).getByRole('option', { name: /^Trips/ })).toBeInTheDocument()
    expect(within(listbox).queryByRole('option', { name: /^Create/ })).not.toBeInTheDocument()
  })

  it('surfaces a failed creation and keeps every selection for a retry', async () => {
    createAlbumMock.mockRejectedValue(new ApiError(409, 'an album of that name already exists'))
    const user = userEvent.setup()
    renderModal()

    await pick(user, 'Add to albums', 'Trips')
    await pickCreate(user, 'Add to albums', 'Dovolená')
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    // The server's reason is shown, nothing was assigned, and both chips
    // survive so the user retries without redoing their work.
    expect(
      await screen.findByText('Could not create “Dovolená”: an album of that name already exists'),
    ).toBeInTheDocument()
    expect(bulkMock).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Remove Trips' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Remove Dovolená' })).toBeInTheDocument()
  })

  it('says the entries exist when only the assignment fails, then retries without re-creating', async () => {
    createAlbumMock.mockResolvedValue(album('al9', 'Dovolená'))
    bulkMock.mockRejectedValueOnce(new ApiError(500, 'boom'))
    bulkMock.mockResolvedValueOnce(result({ total: 2, updated: 2 }))
    const user = userEvent.setup()
    renderModal()

    await pickCreate(user, 'Add to albums', 'Dovolená')
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    expect(
      await screen.findByText(
        'The new albums and labels were created; only assigning the photos failed: boom',
      ),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Remove Dovolená' })).toBeInTheDocument()

    // The chip now carries the real UID, so a retry only re-sends the batch.
    await user.click(screen.getByRole('button', { name: 'Apply' }))
    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledTimes(2)
    })
    expect(createAlbumMock).toHaveBeenCalledTimes(1)
    expect(bulkMock).toHaveBeenLastCalledWith(['ph1', 'ph2'], { add_to_albums: ['al9'] })
    expect(await screen.findByText(/2 updated/)).toBeInTheDocument()
  })

  it('offers no create entry to a viewer', async () => {
    const user = userEvent.setup()
    renderModal(['ph1', 'ph2'], false)

    const input = await screen.findByRole('combobox', { name: 'Add to albums' })
    await user.type(input, 'Dovolená')

    const listbox = screen.getByRole('listbox', { name: 'Add to albums' })
    expect(within(listbox).queryByRole('option', { name: /^Create/ })).not.toBeInTheDocument()
    expect(within(listbox).getByText('No matches.')).toBeInTheDocument()
  })
})

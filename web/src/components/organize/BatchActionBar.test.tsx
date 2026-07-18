import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../../auth/AuthContext'
import { type UseBulkEditResult } from '../../hooks/useBulkEdit'
import i18n from '../../i18n'
import { ApiError } from '../../services/auth'
import { type BulkResult } from '../../services/bulk'
import { type AlbumCount } from '../../services/organize'
import { ToastProvider } from '../toast/ToastProvider'

import { BatchActionBar } from './BatchActionBar'

vi.mock('../../services/bulk', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/bulk')>()
  return { ...actual, bulkUpdatePhotos: vi.fn() }
})
vi.mock('../../services/organize', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/organize')>()
  return { ...actual, fetchAlbums: vi.fn(), fetchLabels: vi.fn() }
})

const { bulkUpdatePhotos } = await import('../../services/bulk')
const { fetchAlbums, fetchLabels } = await import('../../services/organize')
const bulkMock = vi.mocked(bulkUpdatePhotos)
const albumsMock = vi.mocked(fetchAlbums)
const labelsMock = vi.mocked(fetchLabels)

/** A bulk result with `updated` photos touched and nothing skipped/errored. */
function result(updated: number): BulkResult {
  return { results: [], counts: { total: updated, updated, skipped: 0, errored: 0 } }
}

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

/** Builds a bulk-edit result over a two-photo selection with spy callbacks. */
function makeBulk(): UseBulkEditResult {
  return {
    canBulkEdit: true,
    selection: {
      active: false,
      selected: new Set(['a', 'b']),
      count: 2,
      enable: vi.fn(),
      disable: vi.fn(),
      toggle: vi.fn(),
      toggleRange: vi.fn(),
      selectMany: vi.fn(),
      clear: vi.fn(),
    },
    photoUids: ['a', 'b'],
    gridSelection: undefined,
    editing: false,
    open: vi.fn(),
    close: vi.fn(),
    finish: vi.fn(),
  }
}

/** An editor auth context — the nested full editor modal reads write access. */
const editorAuth: AuthContextValue = {
  status: 'authenticated',
  user: null,
  role: 'editor',
  downloadToken: null,
  canWrite: true,
  isAdmin: false,
  canImport: false,
  login: vi.fn(),
  logout: vi.fn(),
  refresh: vi.fn(),
}

function renderBar(bulk: UseBulkEditResult) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={editorAuth}>
        <ToastProvider>
          <MemoryRouter>
            <BatchActionBar bulk={bulk} onSelectAll={vi.fn()} />
          </MemoryRouter>
        </ToastProvider>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeAll(async () => {
  await i18n.changeLanguage('en')
})

beforeEach(() => {
  bulkMock.mockReset()
  albumsMock.mockReset()
  labelsMock.mockReset()
})

describe('BatchActionBar', () => {
  it('shows the live count and the batch actions', () => {
    renderBar(makeBulk())

    expect(screen.getByText('2 selected')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Add to album' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Labels' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Favorite' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Archive' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Select all' })).toBeInTheDocument()
  })

  it('applies favorite to the whole batch, toasts success and clears the selection', async () => {
    const bulk = makeBulk()
    bulkMock.mockResolvedValue(result(2))
    const user = userEvent.setup()
    renderBar(bulk)

    await user.click(screen.getByRole('button', { name: 'Favorite' }))

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['a', 'b'], { set_favorite: true })
    })
    expect(await screen.findByText('Applied to 2 photos')).toBeInTheDocument()
    // A successful apply clears the selection and reloads via finish().
    expect(bulk.finish).toHaveBeenCalledTimes(1)
  })

  it('leaves the selection intact and toasts the reason when a batch fails', async () => {
    const bulk = makeBulk()
    bulkMock.mockRejectedValue(new ApiError(409, 'Conflicting operation'))
    const user = userEvent.setup()
    renderBar(bulk)

    await user.click(screen.getByRole('button', { name: 'Archive' }))

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['a', 'b'], { archive: true })
    })
    expect(await screen.findByText('Conflicting operation')).toBeInTheDocument()
    // A failed apply must not clear the selection, so it can be retried.
    expect(bulk.finish).not.toHaveBeenCalled()
  })

  it('clears the selection from the close control', async () => {
    const bulk = makeBulk()
    const user = userEvent.setup()
    renderBar(bulk)

    await user.click(screen.getByRole('button', { name: 'Clear selection' }))

    expect(bulk.selection.clear).toHaveBeenCalledTimes(1)
  })

  it('opens the album picker, loading its options, with apply gated on a choice', async () => {
    const bulk = makeBulk()
    albumsMock.mockResolvedValue([{ ...album('al1', 'Trip'), photo_count: 3 }])
    labelsMock.mockResolvedValue([])
    const user = userEvent.setup()
    renderBar(bulk)

    await user.click(screen.getByRole('button', { name: 'Add to album' }))

    // The picker fetches albums and offers them, with Apply disabled until one
    // is chosen — an empty add would be a no-op.
    expect(await screen.findByLabelText('Add to albums')).toBeInTheDocument()
    expect(albumsMock).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('button', { name: 'Apply' })).toBeDisabled()
  })
})

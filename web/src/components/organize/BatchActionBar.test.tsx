import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../../auth/AuthContext'
import { type UseBulkEditResult } from '../../hooks/useBulkEdit'
import i18n from '../../i18n'
import { ApiError } from '../../services/auth'
import { type BulkResult } from '../../services/bulk'
import { type AlbumCount, type LabelCount } from '../../services/organize'
import { ToastProvider } from '../toast/ToastProvider'

import { BatchActionBar, type BatchExtraAction } from './BatchActionBar'

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

/**
 * A `fetchAlbums`/`fetchLabels` stub that honours the AbortSignal exactly as the
 * real service does: it rejects with an `AbortError` the moment the signal
 * aborts, and otherwise resolves with `value` on a later tick. This reproduces
 * the self-aborting-effect bug — a stub that ignored the signal (plain
 * `mockResolvedValue`) would pass even against the broken effect, because the
 * abort the effect triggered on itself would never take effect.
 */
function abortable<T>(value: T): (signal?: AbortSignal) => Promise<T> {
  return (signal) =>
    new Promise<T>((resolve, reject) => {
      const abortError = () => new DOMException('The operation was aborted.', 'AbortError')
      if (signal?.aborted === true) {
        reject(abortError())
        return
      }
      const timer = setTimeout(() => {
        resolve(value)
      }, 0)
      signal?.addEventListener('abort', () => {
        clearTimeout(timer)
        reject(abortError())
      })
    })
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
  isMaintainer: false,
  canImport: false,
  login: vi.fn(),
  logout: vi.fn(),
  refresh: vi.fn(),
}

function renderBar(bulk: UseBulkEditResult, extraActions?: BatchExtraAction[]) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={editorAuth}>
        <ToastProvider>
          <MemoryRouter>
            <BatchActionBar bulk={bulk} onSelectAll={vi.fn()} extraActions={extraActions} />
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

  it("joins a page's own actions onto the same bar, honouring their disabled state", async () => {
    const setCover = vi.fn()
    const remove = vi.fn()
    const user = userEvent.setup()
    renderBar(makeBulk(), [
      // A cover is one photo, and two are picked here: the action is offered but
      // not applicable, exactly as an album's is.
      { id: 'cover', icon: 'image', label: 'Set as cover', disabled: true, onClick: setCover },
      { id: 'remove', icon: 'dash-lg', label: 'Remove from album', danger: true, onClick: remove },
    ])

    // One bar carries both vocabularies: the shared batch actions and the page's.
    const bar = screen.getByRole('toolbar', { name: 'Batch actions' })
    expect(within(bar).getByRole('button', { name: 'Add to album' })).toBeInTheDocument()
    expect(within(bar).getByRole('button', { name: 'Set as cover' })).toBeDisabled()

    await user.click(within(bar).getByRole('button', { name: 'Remove from album' }))
    expect(remove).toHaveBeenCalledTimes(1)
    expect(setCover).not.toHaveBeenCalled()
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
    // The stubs honour the AbortSignal, so if the effect aborted its own fetch
    // (the bug) the options would never load and this test would hang.
    albumsMock.mockImplementation(abortable([{ ...album('al1', 'Trip'), photo_count: 3 }]))
    labelsMock.mockImplementation(abortable([]))
    const user = userEvent.setup()
    renderBar(bulk)

    await user.click(screen.getByRole('button', { name: 'Add to album' }))

    // The picker fetches albums and offers them, with Apply disabled until one
    // is chosen — an empty add would be a no-op.
    expect(await screen.findByLabelText('Add to albums')).toBeInTheDocument()
    expect(albumsMock).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('button', { name: 'Apply' })).toBeDisabled()
  })

  it('opens the label picker, loading add and remove fields', async () => {
    const bulk = makeBulk()
    albumsMock.mockImplementation(abortable([]))
    labelsMock.mockImplementation(abortable([label('la1', 'Beach')]))
    const user = userEvent.setup()
    renderBar(bulk)

    await user.click(screen.getByRole('button', { name: 'Labels' }))

    // Both the add and the remove label fields render once the labels load.
    expect(await screen.findByLabelText('Add labels')).toBeInTheDocument()
    expect(screen.getByLabelText('Remove labels')).toBeInTheDocument()
    expect(labelsMock).toHaveBeenCalledTimes(1)
  })

  it('offers the album suggestions in a fixed overlay the scrollable picker cannot clip', async () => {
    const bulk = makeBulk()
    albumsMock.mockImplementation(abortable([{ ...album('al1', 'Trip'), photo_count: 3 }]))
    labelsMock.mockImplementation(abortable([]))
    const user = userEvent.setup()
    renderBar(bulk)

    await user.click(screen.getByRole('button', { name: 'Add to album' }))
    await user.type(await screen.findByLabelText('Add to albums'), 'tr')

    // The typed query surfaces the album, and its listbox is a fixed overlay —
    // so the picker's modal body cannot clip it out of reach on desktop.
    expect(await screen.findByRole('option', { name: /Trip/ })).toBeInTheDocument()
    expect(screen.getByRole('listbox', { name: 'Add to albums' })).toHaveStyle({
      position: 'fixed',
    })
  })

  it('shows the options error state, not an endless spinner, on a load failure', async () => {
    const bulk = makeBulk()
    // A genuine (non-abort) rejection must surface the error state.
    albumsMock.mockRejectedValue(new ApiError(500, 'boom'))
    labelsMock.mockResolvedValue([])
    const user = userEvent.setup()
    renderBar(bulk)

    await user.click(screen.getByRole('button', { name: 'Add to album' }))

    expect(
      await screen.findByText('Could not load the list. Please try again.'),
    ).toBeInTheDocument()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('retries the option load after an error', async () => {
    const bulk = makeBulk()
    albumsMock.mockRejectedValueOnce(new ApiError(500, 'boom'))
    albumsMock.mockResolvedValueOnce([{ ...album('al1', 'Trip'), photo_count: 3 }])
    labelsMock.mockResolvedValue([])
    const user = userEvent.setup()
    renderBar(bulk)

    await user.click(screen.getByRole('button', { name: 'Add to album' }))
    await user.click(await screen.findByRole('button', { name: 'Try again' }))

    // The second attempt succeeds and the picker renders its options.
    expect(await screen.findByLabelText('Add to albums')).toBeInTheDocument()
    expect(albumsMock).toHaveBeenCalledTimes(2)
  })
})

/**
 * Points `window.matchMedia` at a fixed phone/desktop answer. The shared test
 * setup stubs a non-matching (desktop) `matchMedia`; a phone-width test overrides
 * it so the bar takes its collapsed branch.
 */
function mockViewport(narrow: boolean): void {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: narrow,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
}

describe('BatchActionBar on a narrow (phone) screen', () => {
  afterEach(() => {
    // Restore the shared desktop default so later tests never inherit a phone.
    mockViewport(false)
  })

  it('keeps the primary actions inline and folds the rest into an overflow menu', async () => {
    mockViewport(true)
    const user = userEvent.setup()
    renderBar(makeBulk())

    // Clear, the live count and the two most-common actions stay directly
    // reachable — no wrapping into a tall multi-row block.
    expect(screen.getByRole('button', { name: 'Clear selection' })).toBeInTheDocument()
    expect(screen.getByText('2 selected')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Add to album' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Labels' })).toBeInTheDocument()

    // The secondary actions are collapsed away until the overflow menu is opened,
    // so the bar stays a single compact row.
    expect(screen.queryByRole('button', { name: 'Archive' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Favorite' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'More actions' }))

    // Opening the menu reveals the rest, so nothing is lost on a phone.
    expect(await screen.findByRole('button', { name: 'Favorite' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Archive' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Select all' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'More edits' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Download ZIP' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Stack selected' })).toBeInTheDocument()
  })

  it('still applies a batch action chosen from the overflow menu', async () => {
    const bulk = makeBulk()
    bulkMock.mockResolvedValue(result(2))
    mockViewport(true)
    const user = userEvent.setup()
    renderBar(bulk)

    await user.click(screen.getByRole('button', { name: 'More actions' }))
    await user.click(await screen.findByRole('button', { name: 'Favorite' }))

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['a', 'b'], { set_favorite: true })
    })
    expect(bulk.finish).toHaveBeenCalledTimes(1)
  })
})

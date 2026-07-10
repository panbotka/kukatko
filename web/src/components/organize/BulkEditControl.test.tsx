import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../../auth/AuthContext'
import { useBulkEdit } from '../../hooks/useBulkEdit'
import i18n from '../../i18n'
import { ApiError } from '../../services/auth'

import { BulkEditControl } from './BulkEditControl'

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

const onEdited = vi.fn()

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

/**
 * The smallest page that wires bulk editing: a selection driven by plain buttons
 * plus the control under test. It stands in for the library/album/label grids,
 * whose tiles do nothing more than call `toggle` with a photo UID.
 */
function Harness() {
  const bulk = useBulkEdit({ onEdited })
  return (
    <>
      <span data-testid="count">{bulk.selection.count}</span>
      <button
        type="button"
        onClick={() => {
          bulk.selection.toggle('ph1')
        }}
      >
        toggle ph1
      </button>
      <button
        type="button"
        onClick={() => {
          bulk.selection.toggle('ph2')
        }}
      >
        toggle ph2
      </button>
      <BulkEditControl bulk={bulk} />
    </>
  )
}

function renderControl(canWrite = true) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <Harness />
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/** Selects `ph1` and `ph2`, then opens the dialog and chooses one operation. */
async function selectTwoAndOpen(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: 'toggle ph1' }))
  await user.click(screen.getByRole('button', { name: 'toggle ph2' }))
  await user.click(screen.getByRole('button', { name: 'Bulk edit' }))
  await user.selectOptions(await screen.findByLabelText('Archive'), 'archive')
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  bulkMock.mockReset()
  albumsMock.mockReset()
  labelsMock.mockReset()
  onEdited.mockReset()
  albumsMock.mockResolvedValue([])
  labelsMock.mockResolvedValue([])
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('BulkEditControl', () => {
  it('is hidden entirely from a viewer', () => {
    renderControl(false)
    expect(screen.queryByRole('button', { name: 'Bulk edit' })).not.toBeInTheDocument()
  })

  it('is disabled while nothing is selected and enabled once a photo is picked', async () => {
    const user = userEvent.setup()
    renderControl()

    expect(screen.getByRole('button', { name: 'Bulk edit' })).toBeDisabled()

    await user.click(screen.getByRole('button', { name: 'toggle ph1' }))
    expect(screen.getByRole('button', { name: 'Bulk edit' })).toBeEnabled()

    // Deselecting the last photo disables it again.
    await user.click(screen.getByRole('button', { name: 'toggle ph1' }))
    expect(screen.getByRole('button', { name: 'Bulk edit' })).toBeDisabled()
  })

  it('submits exactly the selected photos and clears the selection on success', async () => {
    bulkMock.mockResolvedValue({
      results: [],
      counts: { total: 2, updated: 2, skipped: 0, errored: 0 },
    })
    const user = userEvent.setup()
    renderControl()

    await selectTwoAndOpen(user)
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['ph1', 'ph2'], { archive: true })
    })

    await user.click(await screen.findByRole('button', { name: 'Done' }))

    // Selection mode stays on, but the applied batch is gone: no stale UID can
    // be carried into the next action, and the list is refreshed.
    expect(screen.getByTestId('count')).toHaveTextContent('0')
    expect(screen.getByRole('button', { name: 'Bulk edit' })).toBeDisabled()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(onEdited).toHaveBeenCalledTimes(1)
  })

  it("keeps the selection and shows the server's message when the apply fails", async () => {
    bulkMock.mockRejectedValue(new ApiError(400, 'archive and unarchive are mutually exclusive'))
    const user = userEvent.setup()
    renderControl()

    await selectTwoAndOpen(user)
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    expect(
      await screen.findByText('archive and unarchive are mutually exclusive'),
    ).toBeInTheDocument()
    expect(screen.getByTestId('count')).toHaveTextContent('2')
    expect(onEdited).not.toHaveBeenCalled()

    // Dismissing the failed dialog still leaves the selection ready for a retry.
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.getByTestId('count')).toHaveTextContent('2')
    expect(screen.getByRole('button', { name: 'Bulk edit' })).toBeEnabled()
  })

  it('falls back to a generic message when the failure carries none', async () => {
    bulkMock.mockRejectedValue(new TypeError('Failed to fetch'))
    const user = userEvent.setup()
    renderControl()

    await selectTwoAndOpen(user)
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    expect(await screen.findByText('The bulk edit failed. Please try again.')).toBeInTheDocument()
    expect(screen.getByTestId('count')).toHaveTextContent('2')
  })
})

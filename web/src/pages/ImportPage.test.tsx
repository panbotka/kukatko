import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { ApiError } from '../services/auth'
import {
  type ImportRun,
  type ImportRunsResponse,
  type ImportSource,
  type JobStats,
  type RunStatus,
} from '../services/import'

import { ImportPage } from './ImportPage'

vi.mock('../services/import', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/import')>()
  return {
    ...actual,
    fetchImportRuns: vi.fn(),
    fetchJobStats: vi.fn(),
    startImport: vi.fn(),
  }
})

const { fetchImportRuns, fetchJobStats, startImport } = await import('../services/import')
const runsMock = vi.mocked(fetchImportRuns)
const statsMock = vi.mocked(fetchJobStats)
const startMock = vi.mocked(startImport)

function run(
  id: number,
  source: ImportSource,
  status: RunStatus,
  overrides: Partial<ImportRun> = {},
): ImportRun {
  return {
    id,
    source,
    started_at: '2026-06-01T10:00:00Z',
    finished_at: status === 'running' ? null : '2026-06-01T10:30:00Z',
    status,
    high_watermark: null,
    counts: { imported: 5, updated: 1, skipped: 2, failed: 0 },
    last_error: '',
    ...overrides,
  }
}

function runsResponse(runs: ImportRun[]): ImportRunsResponse {
  return { runs, limit: 50, offset: 0, sources: { photoprism: true, photosorter: true } }
}

const emptyStats: JobStats = { by_state: {}, by_type: {}, total: 0 }

function auth(isAdmin: boolean): AuthContextValue {
  const role = isAdmin ? 'admin' : 'viewer'
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'U', role },
    role,
    downloadToken: null,
    canWrite: isAdmin,
    isAdmin,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

function renderPage(isAdmin = true) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(isAdmin)}>
        <MemoryRouter>
          <ImportPage />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  runsMock.mockReset()
  statsMock.mockReset()
  startMock.mockReset()
  statsMock.mockResolvedValue(emptyStats)
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('ImportPage', () => {
  it('renders the run-history table from polled status', async () => {
    runsMock.mockResolvedValue(
      runsResponse([
        run(2, 'photoprism', 'done'),
        run(1, 'photosorter', 'failed', { last_error: 'connection refused' }),
      ]),
    )
    renderPage()

    expect(await screen.findByText('Run history')).toBeInTheDocument()
    // Status badges from the two runs.
    expect(screen.getAllByText('Done').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Failed').length).toBeGreaterThan(0)
    // The failed run's error message shows in the table.
    expect(screen.getByText('connection refused')).toBeInTheDocument()
  })

  it('renders live progress and counts for an in-progress run', async () => {
    runsMock.mockResolvedValue(
      runsResponse([
        run(3, 'photoprism', 'running', {
          counts: { imported: 7, updated: 2, skipped: 1, failed: 0 },
        }),
      ]),
    )
    renderPage()

    // The "in progress" badge marks the running source section.
    expect(await screen.findByText('In progress')).toBeInTheDocument()
    // Counts render from the polled run status.
    expect(screen.getAllByText('New: 7').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Updated: 2').length).toBeGreaterThan(0)
  })

  it('starts an import: calls the API and reflects in-progress', async () => {
    runsMock
      .mockResolvedValueOnce(runsResponse([]))
      .mockResolvedValue(runsResponse([run(4, 'photoprism', 'running')]))
    startMock.mockResolvedValue({ job_id: 4, status: 'queued' })
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Import & migration')
    // Two sections, two start buttons; the first is PhotoPrism.
    const startButtons = screen.getAllByRole('button', { name: 'Start import' })
    await user.click(startButtons[0])

    await waitFor(() => {
      expect(startMock).toHaveBeenCalledWith('photoprism')
    })
    // After the refresh, the running run flips the section to in-progress.
    expect(await screen.findByText('In progress')).toBeInTheDocument()
  })

  it('shows a conflict notice when an import is already running', async () => {
    runsMock.mockResolvedValue(runsResponse([run(5, 'photoprism', 'done')]))
    startMock.mockRejectedValue(new ApiError(409, 'already in progress'))
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Import & migration')
    const startButtons = screen.getAllByRole('button', { name: 'Start import' })
    await user.click(startButtons[0])

    expect(await screen.findByText('An import is already in progress.')).toBeInTheDocument()
  })

  it('denies access to non-admins', async () => {
    renderPage(false)
    expect(
      await screen.findByText('This page is available to administrators only.'),
    ).toBeInTheDocument()
    expect(screen.queryByText('Import & migration')).not.toBeInTheDocument()
    expect(runsMock).not.toHaveBeenCalled()
  })
})

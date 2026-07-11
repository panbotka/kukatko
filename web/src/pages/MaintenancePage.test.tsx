import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type JobStats } from '../services/import'
import { type RepairResult, type ScanReport } from '../services/maintenance'

import { MaintenancePage } from './MaintenancePage'

vi.mock('../services/maintenance', () => ({
  fetchMaintenanceScan: vi.fn(),
  runMaintenanceRepair: vi.fn(),
}))

vi.mock('../services/import', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/import')>()
  return { ...actual, fetchJobStats: vi.fn() }
})

const { fetchMaintenanceScan, runMaintenanceRepair } = await import('../services/maintenance')
const { fetchJobStats } = await import('../services/import')
const scanMock = vi.mocked(fetchMaintenanceScan)
const repairMock = vi.mocked(runMaintenanceRepair)
const statsMock = vi.mocked(fetchJobStats)

/** Builds a scan report, defaulting every finding to empty unless overridden. */
function report(overrides: Partial<ScanReport> = {}): ScanReport {
  const empty = { count: 0, samples: [] as string[] }
  return {
    photos: 10,
    files_in_db: 11,
    originals_on_disk: 12,
    missing_originals: empty,
    orphan_files: empty,
    missing_thumbnails: empty,
    missing_embeddings: empty,
    missing_faces: empty,
    missing_phashes: empty,
    ...overrides,
  }
}

/** Builds a repair result, defaulting every count to zero unless overridden. */
function repairResult(overrides: Partial<RepairResult> = {}): RepairResult {
  return {
    thumbnails_enqueued: 0,
    embeddings_enqueued: 0,
    faces_enqueued: 0,
    phashes_enqueued: 0,
    orphans_imported: 0,
    orphans_skipped: 0,
    orphans_failed: 0,
    ...overrides,
  }
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
          <MaintenancePage />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  scanMock.mockReset()
  repairMock.mockReset()
  statsMock.mockReset()
  statsMock.mockResolvedValue(emptyStats)
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('MaintenancePage', () => {
  it('denies access to non-admins', () => {
    renderPage(false)
    expect(screen.getByText('This page is available to administrators only.')).toBeInTheDocument()
    expect(screen.queryByText('Library maintenance')).not.toBeInTheDocument()
    expect(scanMock).not.toHaveBeenCalled()
  })

  it('runs a scan and renders the findings table with counts and samples', async () => {
    scanMock.mockResolvedValue(
      report({
        missing_thumbnails: { count: 3, samples: ['ph1', 'ph2'] },
        orphan_files: { count: 1, samples: ['2026/06/x.jpg'] },
      }),
    )
    const user = userEvent.setup()
    renderPage()

    await user.click(screen.getByRole('button', { name: 'Run scan' }))

    expect(await screen.findByText('Missing thumbnails')).toBeInTheDocument()
    // The outstanding count badge and a sample id both render.
    expect(screen.getByText('3')).toBeInTheDocument()
    expect(screen.getByText('ph1, ph2')).toBeInTheDocument()
    expect(screen.getByText('2026/06/x.jpg')).toBeInTheDocument()
    // The totals summary and its drift hint render.
    expect(screen.getByText(/10 photos/)).toBeInTheDocument()
    expect(screen.getByText(/should roughly match/)).toBeInTheDocument()
    // Each finding carries an inline plain-language explanation of what it means.
    expect(screen.getByText(/has no generated thumbnail/)).toBeInTheDocument()
    expect(screen.getByText(/belongs to no catalogue photo/)).toBeInTheDocument()
  })

  it('reports a clean library when the scan finds no problems', async () => {
    scanMock.mockResolvedValue(report())
    const user = userEvent.setup()
    renderPage()

    await user.click(screen.getByRole('button', { name: 'Run scan' }))

    expect(await screen.findByText('The library is consistent.')).toBeInTheDocument()
  })

  it('disables the repair button until a repair is selected, then runs it', async () => {
    repairMock.mockResolvedValue(repairResult({ thumbnails_enqueued: 4 }))
    const user = userEvent.setup()
    renderPage()

    const runButton = screen.getByRole('button', { name: 'Run repairs' })
    expect(runButton).toBeDisabled()

    await user.click(screen.getByLabelText('Regenerate missing thumbnails'))
    expect(runButton).toBeEnabled()

    await user.click(runButton)
    await waitFor(() => {
      expect(repairMock).toHaveBeenCalledWith({ thumbnails: true })
    })
    // The result summary reflects the enqueued thumbnail count.
    expect(await screen.findByText(/thumbnails 4/)).toBeInTheDocument()
  })

  it('shows an error when the repair fails', async () => {
    repairMock.mockRejectedValue(new Error('boom'))
    const user = userEvent.setup()
    renderPage()

    await user.click(screen.getByLabelText('Backfill missing embeddings'))
    await user.click(screen.getByRole('button', { name: 'Run repairs' }))

    expect(await screen.findByText('The repair failed.')).toBeInTheDocument()
  })

  it('polls and renders the background job-queue stats with a legend', async () => {
    statsMock.mockResolvedValue({ by_state: { queued: 7, running: 2 }, by_type: {}, total: 9 })
    renderPage()

    expect(await screen.findByText('Total: 9')).toBeInTheDocument()
    expect(screen.getByText('Queued: 7')).toBeInTheDocument()
    // The queue is introduced and every state is explained in plain language,
    // including what "Dead" means and that it needs a manual requeue.
    expect(screen.getByText(/background queue that runs the repairs/)).toBeInTheDocument()
    expect(screen.getByText(/failed even after all attempts were used up/)).toBeInTheDocument()
  })
})

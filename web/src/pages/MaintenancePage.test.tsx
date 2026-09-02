import { render, screen, waitFor, within } from '@testing-library/react'
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
  purgeAuditLog: vi.fn(),
  // The nameless-subject card lives on this page too; its own tests drive it.
  fetchNamelessSubjects: vi.fn(),
  detachNamelessSubjects: vi.fn(),
  restoreNamelessSubjects: vi.fn(),
}))

vi.mock('../services/import', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/import')>()
  return { ...actual, fetchJobStats: vi.fn() }
})

const { fetchMaintenanceScan, runMaintenanceRepair, purgeAuditLog } =
  await import('../services/maintenance')
const { fetchJobStats } = await import('../services/import')
const scanMock = vi.mocked(fetchMaintenanceScan)
const repairMock = vi.mocked(runMaintenanceRepair)
const purgeMock = vi.mocked(purgeAuditLog)
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

function auth(opts: { isMaintainer?: boolean; role?: string } = {}): AuthContextValue {
  const { isMaintainer = false } = opts
  const role = opts.role ?? (isMaintainer ? 'maintainer' : 'viewer')
  const isAdmin = role === 'admin' || role === 'maintainer'
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'U', role },
    role,
    downloadToken: null,
    canWrite: isAdmin,
    isAdmin,
    isMaintainer,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

/** The shared setup stubs a non-matching (desktop) `matchMedia`; restore it after. */
const realMatchMedia = window.matchMedia

/**
 * Points `window.matchMedia` at a fixed phone/desktop answer, so
 * `useIsNarrowViewport` — and through it the scan result's table/card choice —
 * takes the branch under test.
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

function renderPage(value: AuthContextValue = auth({ isMaintainer: true })) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={value}>
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
  purgeMock.mockReset()
  statsMock.mockReset()
  statsMock.mockResolvedValue(emptyStats)
})

afterEach(() => {
  window.matchMedia = realMatchMedia
})

describe('MaintenancePage', () => {
  it('denies access to non-maintainers, including a plain admin', () => {
    // Maintenance is operations: hidden from a viewer and from an admin alike.
    for (const value of [auth(), auth({ role: 'admin' })]) {
      const { unmount } = renderPage(value)
      expect(
        screen.getByText('This page is available to system maintainers only.'),
      ).toBeInTheDocument()
      expect(screen.queryByText('Library maintenance')).not.toBeInTheDocument()
      // The destructive audit-log purge is hidden along with the rest of the page.
      expect(screen.queryByText('Purge audit log')).not.toBeInTheDocument()
      unmount()
    }
    expect(scanMock).not.toHaveBeenCalled()
    expect(purgeMock).not.toHaveBeenCalled()
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

    await user.click(screen.getByRole('button', { name: 'Run the check' }))

    expect(await screen.findByText('Photos with no thumbnail')).toBeInTheDocument()
    // The outstanding count badge renders; the raw ids and paths of the samples
    // do not — they wait behind the row's technical-details disclosure.
    expect(screen.getByText('3')).toBeInTheDocument()
    expect(screen.queryByText('ph1, ph2')).toBeNull()
    expect(screen.queryByText('2026/06/x.jpg')).toBeNull()
    const disclosures = screen.getAllByRole('button', { name: 'Technical details' })
    expect(disclosures).toHaveLength(2)
    for (const disclosure of disclosures) {
      await user.click(disclosure)
    }
    expect(screen.getByText('ph1, ph2')).toBeInTheDocument()
    expect(screen.getByText('2026/06/x.jpg')).toBeInTheDocument()
    // The totals summary and its drift hint render.
    expect(screen.getByText(/10 photos/)).toBeInTheDocument()
    expect(screen.getByText(/should roughly agree/)).toBeInTheDocument()
    // Each finding carries an inline plain-language explanation of what it means.
    expect(screen.getByText(/no small copy for the grid/)).toBeInTheDocument()
    expect(screen.getByText(/belongs to no photo in the catalogue/)).toBeInTheDocument()

    // A wide viewport keeps the compact table, headerless: its first column names
    // the problem, so there is nothing for a header row to add.
    const table = screen.getByRole('table')
    expect(within(table).queryAllByRole('columnheader')).toHaveLength(0)
    expect(within(table).getAllByRole('row')).toHaveLength(6)
  })

  it('reflows each finding into a stacked card on a phone', async () => {
    mockViewport(true)
    scanMock.mockResolvedValue(
      report({ missing_thumbnails: { count: 3, samples: ['ph1', 'ph2'] } }),
    )
    const user = userEvent.setup()
    renderPage()

    await user.click(screen.getByRole('button', { name: 'Run the check' }))

    expect(await screen.findByText('Photos with no thumbnail')).toBeInTheDocument()
    // Nothing to drag sideways: the three-column table is gone entirely.
    expect(screen.queryByRole('table')).toBeNull()

    const cards = screen.getAllByRole('listitem')
    expect(cards).toHaveLength(6)
    // The headerless table's columns still carry their labels onto the card —
    // a card has no header row to read the values across from.
    const thumbnails = cards[2]
    expect(within(thumbnails).getByText('What drifted')).toBeInTheDocument()
    expect(within(thumbnails).getByText('How many')).toBeInTheDocument()
    expect(within(thumbnails).getByText('Examples')).toBeInTheDocument()
    // Count, samples and the plain-language explanation all survive the reflow.
    expect(within(thumbnails).getByText('Photos with no thumbnail')).toBeInTheDocument()
    expect(within(thumbnails).getByText('3')).toBeInTheDocument()
    expect(within(thumbnails).queryByText('ph1, ph2')).toBeNull()
    await user.click(within(thumbnails).getByRole('button', { name: 'Technical details' }))
    expect(within(thumbnails).getByText('ph1, ph2')).toBeInTheDocument()
    expect(within(thumbnails).getByText(/no small copy for the grid/)).toBeInTheDocument()
  })

  it('reports a clean library when the scan finds no problems', async () => {
    scanMock.mockResolvedValue(report())
    const user = userEvent.setup()
    renderPage()

    await user.click(screen.getByRole('button', { name: 'Run the check' }))

    expect(
      await screen.findByText('The catalogue and the files agree; there is nothing to repair.'),
    ).toBeInTheDocument()
  })

  it('disables the repair button until a repair is selected, then runs it', async () => {
    repairMock.mockResolvedValue(repairResult({ thumbnails_enqueued: 4 }))
    const user = userEvent.setup()
    renderPage()

    const runButton = screen.getByRole('button', { name: 'Start filling in' })
    expect(runButton).toBeDisabled()

    await user.click(screen.getByLabelText('Generate the missing thumbnails'))
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

    await user.click(
      screen.getByLabelText('Work out what is in the photos that are still missing it'),
    )
    await user.click(screen.getByRole('button', { name: 'Start filling in' }))

    expect(await screen.findByText('It could not be started.')).toBeInTheDocument()
  })

  it('purges the audit log only after a confirmation step and shows the count', async () => {
    purgeMock.mockResolvedValue({
      deleted: 42,
      older_than_days: 365,
      cutoff: '2025-07-19T00:00:00Z',
    })
    const user = userEvent.setup()
    renderPage()

    // Clicking Purge asks for confirmation first — the service is not called yet.
    await user.click(screen.getByRole('button', { name: 'Purge audit log' }))
    expect(purgeMock).not.toHaveBeenCalled()
    expect(
      screen.getByText(/permanently deletes every audit entry older than 365 days/),
    ).toBeInTheDocument()

    // Confirming runs the purge with the default retention (1 year = 365 days).
    await user.click(screen.getByRole('button', { name: 'Yes, delete them' }))
    await waitFor(() => {
      expect(purgeMock).toHaveBeenCalledWith(365)
    })
    expect(await screen.findByText('Deleted 42 audit entries.')).toBeInTheDocument()
  })

  it('purges with a custom retention window entered in days', async () => {
    purgeMock.mockResolvedValue({ deleted: 3, older_than_days: 30, cutoff: '2025-06-19T00:00:00Z' })
    const user = userEvent.setup()
    renderPage()

    await user.selectOptions(screen.getByLabelText('Delete entries older than'), 'custom')
    const daysInput = screen.getByLabelText('Days to keep')
    await user.clear(daysInput)
    await user.type(daysInput, '30')

    await user.click(screen.getByRole('button', { name: 'Purge audit log' }))
    await user.click(screen.getByRole('button', { name: 'Yes, delete them' }))
    await waitFor(() => {
      expect(purgeMock).toHaveBeenCalledWith(30)
    })
    expect(await screen.findByText('Deleted 3 audit entries.')).toBeInTheDocument()
  })

  it('lets the maintainer cancel the purge without deleting anything', async () => {
    const user = userEvent.setup()
    renderPage()

    await user.click(screen.getByRole('button', { name: 'Purge audit log' }))
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(purgeMock).not.toHaveBeenCalled()
    // The confirmation prompt is gone and the Purge button is back.
    expect(screen.queryByRole('button', { name: 'Yes, delete them' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Purge audit log' })).toBeInTheDocument()
  })

  it('shows an error when the purge fails', async () => {
    purgeMock.mockRejectedValue(new Error('boom'))
    const user = userEvent.setup()
    renderPage()

    await user.click(screen.getByRole('button', { name: 'Purge audit log' }))
    await user.click(screen.getByRole('button', { name: 'Yes, delete them' }))
    expect(await screen.findByText('The purge failed.')).toBeInTheDocument()
  })

  it('polls and renders the background job-queue stats with a legend', async () => {
    statsMock.mockResolvedValue({ by_state: { queued: 7, running: 2 }, by_type: {}, total: 9 })
    renderPage()

    expect(await screen.findByText('Total: 9')).toBeInTheDocument()
    expect(screen.getByText('Waiting: 7')).toBeInTheDocument()
    // The queue is introduced and every state is explained in plain language,
    // including what the permanently-failed count means and that it needs a
    // manual retry — with no "dead job" anywhere in the copy.
    expect(screen.getByText(/works through in the background/)).toBeInTheDocument()
    expect(screen.getByText(/went wrong even after several attempts/)).toBeInTheDocument()
  })
})

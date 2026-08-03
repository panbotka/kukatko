import { render, screen, waitFor, within } from '@testing-library/react'
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
  type JobStats,
  type RunSource,
  type RunStatus,
  type VerifyReport,
} from '../services/import'

import { ImportPage } from './ImportPage'

vi.mock('../services/import', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/import')>()
  return {
    ...actual,
    fetchImportRuns: vi.fn(),
    fetchJobStats: vi.fn(),
    fetchImportFailures: vi.fn(),
    fetchVerifyReport: vi.fn(),
    startImport: vi.fn(),
  }
})

const { fetchImportRuns, fetchJobStats, fetchImportFailures, fetchVerifyReport, startImport } =
  await import('../services/import')
const runsMock = vi.mocked(fetchImportRuns)
const statsMock = vi.mocked(fetchJobStats)
const failuresMock = vi.mocked(fetchImportFailures)
const verifyMock = vi.mocked(fetchVerifyReport)
const startMock = vi.mocked(startImport)

function run(
  id: number,
  source: RunSource,
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

/**
 * A clean completeness report whose album section still carries the albums of the
 * types the import deliberately skips — the production shape after the verifier
 * stopped demanding PhotoPrism's auto-generated monthly albums.
 */
function cleanVerifyReport(): VerifyReport {
  return {
    photoprism: {
      source_total: 20670,
      source_reported_total: 20670,
      listing_shortfall: 0,
      source_by_type: { image: 20670 },
      imported_count: 20670,
      deduplicated_count: 0,
      missing_count: 0,
      missing_uids: [],
      surplus_count: 0,
      surplus_uids: [],
      file_gap_count: 0,
      file_gaps: [],
    },
    vectors: {
      not_configured: true,
      source_total_photos: 0,
      source_photos_with_embeddings: 0,
      source_photos_with_faces: 0,
      source_total_faces: 0,
      catalog_embeddings: 0,
      catalog_face_photos: 0,
      catalog_faces: 0,
      embeddings_source_coverage: 1,
      faces_source_coverage: 1,
      embeddings_missing_for_imported_photos: 0,
      embeddings_missing_uids: [],
      faces_missing_for_imported_photos: 0,
      faces_missing_uids: [],
    },
    structure: {
      albums: {
        source_count: 198,
        catalog_count: 198,
        missing_count: 0,
        missing: [],
        surplus_count: 0,
        surplus: [],
        skipped_types: ['month'],
        skipped_by_design_count: 560,
      },
      labels: {
        source_count: 12,
        catalog_count: 12,
        missing_count: 0,
        missing: [],
        surplus_count: 0,
        surplus: [],
      },
      subjects: {
        source_count: 30,
        catalog_count: 30,
        missing_count: 0,
        missing: [],
        surplus_count: 0,
        surplus: [],
      },
    },
    complete: true,
  }
}

function auth(
  opts: { isMaintainer?: boolean; canImport?: boolean; role?: string } = {},
): AuthContextValue {
  const { isMaintainer = false } = opts
  // Import is an operations capability: it tracks the maintainer role.
  const canImport = opts.canImport ?? isMaintainer
  const role = opts.role ?? (isMaintainer ? 'maintainer' : 'viewer')
  const isAdmin = role === 'admin' || role === 'maintainer'
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'U', role },
    role,
    downloadToken: null,
    canWrite: isAdmin || canImport,
    isAdmin,
    isMaintainer: canImport,
    canImport,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

/** The shared setup stubs a non-matching (desktop) `matchMedia`; restore it after. */
const realMatchMedia = window.matchMedia

/**
 * Points `window.matchMedia` at a fixed phone/desktop answer, so
 * `useIsNarrowViewport` — and through it the run history's table/card choice —
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
  failuresMock.mockReset()
  verifyMock.mockReset()
  startMock.mockReset()
  statsMock.mockResolvedValue(emptyStats)
  failuresMock.mockResolvedValue({ failures: [], limit: 100, offset: 0 })
})

afterEach(() => {
  // Only the viewport stub is undone here. `vi.restoreAllMocks()` would empty the
  // module mocks while the tree is still mounted, and RTL's `cleanup()` afterEach
  // would then flush the page's pending effects into a mock with no
  // implementation (see the note in SystemStatusPage.test.tsx); `restoreMocks:
  // true` in vite.config.ts restores them before the next test anyway.
  window.matchMedia = realMatchMedia
})

describe('ImportPage', () => {
  it('renders a CLI folder import in the history, next to the triggerable sources', async () => {
    runsMock.mockResolvedValue(runsResponse([run(3, 'folder', 'done')]))
    renderPage()

    // `kukatko import dir` has no start button, but its run shows up in the history.
    expect(await screen.findByText('Folder on disk')).toBeInTheDocument()
  })

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

    // A wide viewport keeps the familiar six-column table.
    const table = screen.getByRole('table')
    expect(
      within(table)
        .getAllByRole('columnheader')
        .map((th) => th.textContent),
    ).toEqual(['Source', 'Started', 'Finished', 'Status', 'Counts', 'Last error'])
    // The header row plus one row per run.
    expect(within(table).getAllByRole('row')).toHaveLength(3)
  })

  it('reflows each run into a stacked card on a phone', async () => {
    mockViewport(true)
    runsMock.mockResolvedValue(
      runsResponse([
        run(2, 'photoprism', 'done'),
        run(1, 'photosorter', 'failed', { last_error: 'connection refused' }),
      ]),
    )
    renderPage()

    expect(await screen.findByText('Run history')).toBeInTheDocument()
    // Nothing to drag sideways: the six-column table is gone entirely.
    expect(screen.queryByRole('table')).toBeNull()

    const cards = screen.getAllByRole('listitem')
    expect(cards).toHaveLength(2)
    // Every column travels with its label, so a value is still readable alone.
    const first = cards[0]
    for (const label of ['Source', 'Started', 'Finished', 'Status', 'Counts', 'Last error']) {
      expect(within(first).getByText(label)).toBeInTheDocument()
    }
    expect(within(first).getByText('PhotoPrism')).toBeInTheDocument()
    expect(within(first).getByText('Done')).toBeInTheDocument()
    expect(within(first).getByText('New: 5')).toBeInTheDocument()
    // The per-run status detail survives the reflow: the failure keeps its error.
    expect(within(cards[1]).getByText('connection refused')).toBeInTheDocument()
    expect(within(cards[1]).getByText('Failed')).toBeInTheDocument()
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

  it('starts an import: confirms the first run in the dialog, calls the API, reflects in-progress', async () => {
    runsMock
      .mockResolvedValueOnce(runsResponse([]))
      .mockResolvedValue(runsResponse([run(4, 'photoprism', 'running')]))
    startMock.mockResolvedValue({ job_id: 4, status: 'queued' })
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Import & migration')
    // Two sections, two start buttons; the first is PhotoPrism.
    const startButtons = screen.getAllByRole('button', { name: 'Start import' })
    await user.click(startButtons[0])

    // A first run (nothing completed yet) is confirmed through the shared dialog,
    // whose confirm button carries the same action as the control that opened it.
    const dialog = await screen.findByRole('dialog')
    expect(startMock).not.toHaveBeenCalled()
    await user.click(within(dialog).getByRole('button', { name: 'Start import' }))

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

  it('reports albums of a deliberately skipped type as skipped, not missing', async () => {
    runsMock.mockResolvedValue(runsResponse([run(2, 'photoprism', 'done')]))
    verifyMock.mockResolvedValue(cleanVerifyReport())
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Completeness check')
    await user.click(screen.getByRole('button', { name: 'Verify completeness' }))

    // The report is clean even though the source still serves 560 monthly albums…
    expect(
      await screen.findByText('The import is complete — nothing is missing.'),
    ).toBeInTheDocument()
    // …which are accounted for as skipped by design, with the type named.
    expect(screen.getByText(/skipped: 560/)).toHaveTextContent('month')
  })

  it('cannot be read as finished vectors when the catalogue is a subset of the source', async () => {
    // docs/READINESS_AUDIT.md §2.3: 280 of 20 670 photos imported, 50 embeddings
    // held against a source of 20 092. Every imported photo has its vectors, so
    // the per-photo gap is legitimately 0 — and that lone zero used to read as a
    // finished vector migration.
    const report = cleanVerifyReport()
    report.complete = false
    report.photoprism = { ...report.photoprism, imported_count: 280, missing_count: 20390 }
    report.vectors = {
      not_configured: false,
      source_total_photos: 20670,
      source_photos_with_embeddings: 20092,
      source_photos_with_faces: 8000,
      source_total_faces: 15000,
      catalog_embeddings: 50,
      catalog_face_photos: 20,
      catalog_faces: 30,
      embeddings_source_coverage: 0.0025,
      faces_source_coverage: 0.002,
      embeddings_missing_for_imported_photos: 0,
      embeddings_missing_uids: [],
      faces_missing_for_imported_photos: 0,
      faces_missing_uids: [],
    }
    runsMock.mockResolvedValue(runsResponse([run(2, 'photoprism', 'done')]))
    verifyMock.mockResolvedValue(report)
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Completeness check')
    await user.click(screen.getByRole('button', { name: 'Verify completeness' }))
    await screen.findByText('The import is not complete — some items are missing (see below).')

    // The vector rows carry the source coverage that contradicts the zero gap…
    const embeddings = screen.getByRole('row', { name: /^Embeddings/ })
    expect(embeddings).toHaveTextContent('0.3%')
    expect(screen.getByRole('row', { name: /^Faces/ })).toHaveTextContent('0.2%')
    // …and the scope of that zero is spelled out rather than left to be guessed.
    expect(screen.getByText(/counts only photos that are already in the catalogue/)).toBeVisible()
  })

  it('says the counts describe a window when the source listing came back short', async () => {
    // The production report: "source=20660 kukatko=20647 deduplicated=13
    // missing=0 => COMPLETE" while PhotoPrism held 20 677 pictures. The 17
    // absentees were never listed, so nothing could classify them as missing —
    // and a reader looking at "missing 0" had no way to tell.
    const report = cleanVerifyReport()
    report.complete = false
    report.photoprism = {
      ...report.photoprism,
      source_total: 20660,
      source_reported_total: 20677,
      listing_shortfall: 17,
      imported_count: 20647,
      deduplicated_count: 13,
      missing_count: 0,
    }
    runsMock.mockResolvedValue(runsResponse([run(2, 'photoprism', 'done')]))
    verifyMock.mockResolvedValue(report)
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Completeness check')
    await user.click(screen.getByRole('button', { name: 'Verify completeness' }))

    await screen.findByText('The import is not complete — some items are missing (see below).')
    expect(screen.getByText(/never reached the comparison/)).toBeVisible()
    expect(screen.getByText(/20677/)).toBeVisible()
  })

  it('names the catalogue photos the source listing no longer returns', async () => {
    const report = cleanVerifyReport()
    report.photoprism = {
      ...report.photoprism,
      surplus_count: 1,
      surplus_uids: ['pteek3u9kw8oxi7y'],
    }
    runsMock.mockResolvedValue(runsResponse([run(2, 'photoprism', 'done')]))
    verifyMock.mockResolvedValue(report)
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Completeness check')
    await user.click(screen.getByRole('button', { name: 'Verify completeness' }))

    // Reported, never enforced: a photo deleted in PhotoPrism after import leaves
    // exactly this trace and keeps the report complete.
    await screen.findByText('The import is complete — nothing is missing.')
    expect(screen.getByText('pteek3u9kw8oxi7y')).toBeVisible()
  })

  it('denies access to users without import permission', async () => {
    renderPage(auth())
    expect(
      await screen.findByText('This page is available to system maintainers only.'),
    ).toBeInTheDocument()
    expect(screen.queryByText('Import & migration')).not.toBeInTheDocument()
    expect(runsMock).not.toHaveBeenCalled()
  })

  it('denies a plain admin: import is operations, which needs maintainer', async () => {
    renderPage(auth({ role: 'admin' }))
    expect(
      await screen.findByText('This page is available to system maintainers only.'),
    ).toBeInTheDocument()
    expect(screen.queryByText('Import & migration')).not.toBeInTheDocument()
    expect(runsMock).not.toHaveBeenCalled()
  })

  it('lets a maintainer use the page', async () => {
    runsMock.mockResolvedValue(runsResponse([run(2, 'photoprism', 'done')]))
    renderPage(auth({ isMaintainer: true }))

    expect(await screen.findByText('Run history')).toBeInTheDocument()
    expect(runsMock).toHaveBeenCalled()
  })
})

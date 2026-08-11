import { render, screen, within } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import {
  type ImportFailure,
  type ImportRun,
  type ImportRunsResponse,
  type JobStats,
  type RunSource,
  type RunStatus,
} from '../services/import'

import { ImportPage } from './ImportPage'

vi.mock('../services/import', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/import')>()
  return {
    ...actual,
    fetchImportRuns: vi.fn(),
    fetchJobStats: vi.fn(),
    fetchImportFailures: vi.fn(),
  }
})

const { fetchImportRuns, fetchJobStats, fetchImportFailures } = await import('../services/import')
const runsMock = vi.mocked(fetchImportRuns)
const statsMock = vi.mocked(fetchJobStats)
const failuresMock = vi.mocked(fetchImportFailures)

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
  return { runs, limit: 50, offset: 0 }
}

const emptyStats: JobStats = { by_state: {}, by_type: {}, total: 0 }

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
  it('renders a CLI folder import in the history', async () => {
    runsMock.mockResolvedValue(runsResponse([run(3, 'folder', 'done')]))
    renderPage()

    // `kukatko import dir` is driven from the CLI; its run shows up in the history.
    expect(await screen.findByText('Folder on disk')).toBeInTheDocument()
  })

  it('offers nothing to start: the migration is over and the importers are gone', async () => {
    runsMock.mockResolvedValue(runsResponse([run(2, 'photoprism', 'done')]))
    renderPage()

    await screen.findByText('Run history')
    expect(screen.queryByRole('button', { name: /start/i })).toBeNull()
    expect(screen.queryByText('Completeness check')).toBeNull()
  })

  it('keeps the finished migration runs readable in the history', async () => {
    runsMock.mockResolvedValue(
      runsResponse([
        run(3, 'photosorter_feeds', 'done'),
        run(2, 'photoprism', 'done'),
        run(1, 'photosorter', 'failed', { last_error: 'connection refused' }),
      ]),
    )
    renderPage()

    expect(await screen.findByText('Run history')).toBeInTheDocument()
    // Every retired source still has a label — a raw i18n key in the provenance
    // record would be worse than useless.
    expect(screen.getByText('Legacy catalogue')).toBeInTheDocument()
    expect(screen.getByText('Legacy vector layer')).toBeInTheDocument()
    expect(screen.getByText('Legacy vector layer (feeds)')).toBeInTheDocument()
    expect(screen.getByText('connection refused')).toBeInTheDocument()

    // A wide viewport keeps the familiar six-column table.
    const table = screen.getByRole('table')
    expect(
      within(table)
        .getAllByRole('columnheader')
        .map((th) => th.textContent),
    ).toEqual(['Source', 'Started', 'Finished', 'Status', 'Counts', 'Last error'])
    // The header row plus one row per run.
    expect(within(table).getAllByRole('row')).toHaveLength(4)
  })

  it('reflows each run into a stacked card on a phone', async () => {
    mockViewport(true)
    runsMock.mockResolvedValue(
      runsResponse([
        run(2, 'folder', 'done'),
        run(1, 'folder', 'failed', { last_error: 'connection refused' }),
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
    expect(within(first).getByText('Folder on disk')).toBeInTheDocument()
    expect(within(first).getByText('Done')).toBeInTheDocument()
    expect(within(first).getByText('New: 5')).toBeInTheDocument()
    // The per-run status detail survives the reflow: the failure keeps its error.
    expect(within(cards[1]).getByText('connection refused')).toBeInTheDocument()
    expect(within(cards[1]).getByText('Failed')).toBeInTheDocument()
  })

  it('lists the recorded per-photo failures of a folder import', async () => {
    const failure: ImportFailure = {
      id: 1,
      run_id: 9,
      source: 'folder',
      stage: 'photo',
      photo_uid: '',
      source_ref: '/srv/incoming/beach.jpg',
      detail: '',
      error: 'decode failed',
      created_at: '2026-06-01T10:05:00Z',
      resolved_at: null,
    }
    runsMock.mockResolvedValue(runsResponse([run(9, 'folder', 'partial')]))
    failuresMock.mockResolvedValue({ failures: [failure], limit: 100, offset: 0 })
    renderPage()

    expect(await screen.findByText('/srv/incoming/beach.jpg')).toBeInTheDocument()
    expect(screen.getByText('decode failed')).toBeInTheDocument()
  })

  it('denies access to users without import permission', async () => {
    renderPage(auth())
    expect(
      await screen.findByText('This page is available to system maintainers only.'),
    ).toBeInTheDocument()
    expect(screen.queryByText('Run history')).not.toBeInTheDocument()
    expect(runsMock).not.toHaveBeenCalled()
  })

  it('denies a plain admin: import is operations, which needs maintainer', async () => {
    renderPage(auth({ role: 'admin' }))
    expect(
      await screen.findByText('This page is available to system maintainers only.'),
    ).toBeInTheDocument()
    expect(screen.queryByText('Run history')).not.toBeInTheDocument()
    expect(runsMock).not.toHaveBeenCalled()
  })

  it('lets a maintainer use the page', async () => {
    runsMock.mockResolvedValue(runsResponse([run(2, 'folder', 'done')]))
    renderPage(auth({ isMaintainer: true }))

    expect(await screen.findByText('Run history')).toBeInTheDocument()
    expect(runsMock).toHaveBeenCalled()
  })
})

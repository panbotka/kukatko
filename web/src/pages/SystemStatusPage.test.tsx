import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import type { LibraryStats, SystemStatus } from '../services/system'

import { SystemStatusPage } from './SystemStatusPage'

// Mock the system service module so the page's data and actions are controlled.
vi.mock('../services/system', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/system')>()
  return {
    ...actual,
    fetchSystemStatus: vi.fn(),
    fetchLibraryStats: vi.fn(),
    requeueDeadLetterJobs: vi.fn(),
    triggerBackup: vi.fn(),
  }
})

const { fetchSystemStatus, fetchLibraryStats, requeueDeadLetterJobs, triggerBackup } =
  await import('../services/system')
const fetchMock = vi.mocked(fetchSystemStatus)
const statsMock = vi.mocked(fetchLibraryStats)
const requeueMock = vi.mocked(requeueDeadLetterJobs)
const backupMock = vi.mocked(triggerBackup)

/** The library counts the dashboard's Library section renders. */
function libraryStats(overrides: Partial<LibraryStats> = {}): LibraryStats {
  return {
    photos: 1234,
    videos: 5,
    photos_live: 1230,
    photos_archived: 4,
    photos_with_embedding: 1200,
    photos_with_faces: 800,
    photos_without_embedding: 34,
    photos_without_faces: 434,
    photos_with_gps: 900,
    embeddings: 1200,
    faces: 3000,
    faces_assigned: 2100,
    subjects: 7,
    subjects_person: 6,
    subjects_pet: 1,
    subjects_other: 0,
    markers: 90,
    markers_assigned: 80,
    markers_unassigned: 10,
    albums: 3,
    labels: 4,
    ...overrides,
  }
}

// status builds a full snapshot, with overrides merged shallowly per section.
function status(overrides: Partial<SystemStatus> = {}): SystemStatus {
  return {
    version: { version: '1.2.3', commit: 'abc1234' },
    database: { reachable: true },
    embeddings: { online: false, url: 'http://box:8000' },
    jobs: {
      by_state: { queued: 4, running: 1, failed: 2 },
      by_type: { image_embed: 3 },
      total: 7,
      dead_letter: 2,
      pending_embeddings: 5,
    },
    backup: { configured: true, running: false, last_finished_at: '2026-06-01T10:00:00Z' },
    imports: {
      folder: {
        id: 1,
        source: 'folder',
        started_at: '2026-06-01T09:00:00Z',
        finished_at: '2026-06-01T09:30:00Z',
        status: 'done',
        high_watermark: null,
        counts: { imported: 9, updated: 0, skipped: 0, failed: 0 },
        last_error: '',
      },
    },
    storage: {
      originals_bytes: 1048576,
      cache_bytes: 524288,
      free_bytes: 2147483648,
      total_bytes: 4294967296,
    },
    maps: { configured: true, state: 'ok', degraded: false },
    geocode: {
      configured: true,
      budget_enabled: true,
      limit: 1000,
      spent: 120,
      remaining: 880,
      window_seconds: 86400,
      resets_at: '2026-06-02T09:00:00Z',
    },
    ...overrides,
  }
}

// auth builds an AuthContext value for the given role. System status is an
// operations capability, so the dashboard is gated on `isMaintainer`.
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

// renderPage renders the dashboard within auth + i18n + router providers.
function renderPage(value: AuthContextValue = auth({ isMaintainer: true })) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={value}>
        <MemoryRouter>
          <SystemStatusPage />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
  statsMock.mockReset()
  requeueMock.mockReset()
  backupMock.mockReset()
  fetchMock.mockResolvedValue(status())
  statsMock.mockResolvedValue(libraryStats())
  requeueMock.mockResolvedValue(2)
  backupMock.mockResolvedValue(undefined)
})

// Deliberately no `afterEach(vi.restoreAllMocks)`: `restoreMocks: true` in
// vite.config.ts already restores every mock BEFORE each test, and beforeEach
// re-stubs them. Restoring here as well emptied the module mocks while the tree
// was still mounted — RTL's own `cleanup()` afterEach then unmounted it, React
// flushed the pending passive effects, and the page's data effect called a mock
// with no implementation: `fetchLibraryStats(...)` returned undefined and the
// hook's `.then` threw. That failed a varying test of this file whenever the
// full suite ran busy enough for the effects to flush that late.

describe('SystemStatusPage', () => {
  it('denies access to non-maintainers (viewer and plain admin) and never fetches', async () => {
    // System status is operations: an admin is governance-only, so it is denied too.
    for (const value of [auth(), auth({ role: 'admin' })]) {
      const { unmount } = renderPage(value)
      expect(
        await screen.findByText('This page is available to system maintainers only.'),
      ).toBeInTheDocument()
      unmount()
    }
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('renders each status section from the polled snapshot', async () => {
    renderPage()

    // Each card heading renders.
    expect(await screen.findByText('Database')).toBeInTheDocument()
    expect(screen.getByText('Embeddings (box)')).toBeInTheDocument()
    expect(screen.getByText('Job queue')).toBeInTheDocument()
    expect(screen.getByText('Backup')).toBeInTheDocument()
    expect(screen.getByText('Imports')).toBeInTheDocument()
    expect(screen.getByText('Storage')).toBeInTheDocument()

    // Section values: reachable DB, offline box, dead-letter count, storage size.
    expect(screen.getByText('Reachable')).toBeInTheDocument()
    expect(screen.getByText('Offline')).toBeInTheDocument()
    expect(screen.getByText('Dead: 2')).toBeInTheDocument()
    expect(screen.getByText('1.0 MB')).toBeInTheDocument()
    expect(screen.getByText('1.2.3')).toBeInTheDocument()
  })

  it('explains the job-queue states in plain language, including box-pending', async () => {
    renderPage()

    // The queue is introduced and the tricky states are explained without hover.
    expect(
      await screen.findByText(/background job queue that processes repairs/),
    ).toBeInTheDocument()
    expect(screen.getByText(/failed even after all attempts were used up/)).toBeInTheDocument()
    // The extra box-pending state (jobs waiting for the AI box) is explained here.
    expect(screen.getByText(/waiting in the queue for the box/)).toBeInTheDocument()
  })

  it('shows the offline-box queued embeddings hint', async () => {
    renderPage()
    expect(
      await screen.findByText(
        'Box is offline → 5 embedding jobs are queued and will resume once the box is back online.',
      ),
    ).toBeInTheDocument()
  })

  it('requeues dead-letter jobs via the quick action', async () => {
    const user = userEvent.setup()
    renderPage()

    const button = await screen.findByRole('button', { name: 'Requeue dead-letter jobs' })
    await user.click(button)

    await waitFor(() => {
      expect(requeueMock).toHaveBeenCalledTimes(1)
    })
    expect(await screen.findByText('Requeued 2 jobs.')).toBeInTheDocument()
  })

  it('disables the requeue action when there are no dead-letter jobs', async () => {
    fetchMock.mockResolvedValue(
      status({
        jobs: {
          by_state: { queued: 1 },
          by_type: {},
          total: 1,
          dead_letter: 0,
          pending_embeddings: 0,
        },
      }),
    )
    renderPage()
    const button = await screen.findByRole('button', { name: 'Requeue dead-letter jobs' })
    expect(button).toBeDisabled()
  })

  it('triggers a backup via the quick action', async () => {
    const user = userEvent.setup()
    renderPage()

    const button = await screen.findByRole('button', { name: 'Trigger backup' })
    await user.click(button)

    await waitFor(() => {
      expect(backupMock).toHaveBeenCalledTimes(1)
    })
    expect(await screen.findByText('Backup started in the background.')).toBeInTheDocument()
  })

  it('links the import and maintenance quick actions to their flows', async () => {
    renderPage()
    expect(await screen.findByRole('link', { name: 'Import history' })).toHaveAttribute(
      'href',
      '/import',
    )
    expect(screen.getByRole('link', { name: 'Maintenance scan' })).toHaveAttribute(
      'href',
      '/maintenance',
    )
  })

  it('renders the library counts from the shared stats endpoint', async () => {
    renderPage()

    expect(await screen.findByRole('heading', { name: 'Library' })).toBeInTheDocument()
    expect(await screen.findByTestId('stat-headline-photos')).toHaveTextContent('1,234')
    expect(screen.getByTestId('stat-content-pending')).toHaveTextContent('34')
    // One data source: the dashboard reads the same endpoint as the stats page.
    expect(statsMock).toHaveBeenCalledTimes(1)
  })

  it('degrades the library section alone when the counts fail', async () => {
    statsMock.mockRejectedValue(new Error('boom'))
    renderPage()

    expect(await screen.findByText('Failed to load the library statistics.')).toBeInTheDocument()
    expect(screen.queryByTestId('library-stats')).not.toBeInTheDocument()
    // The operational cards still render from the healthy status snapshot.
    expect(screen.getByText('Reachable')).toBeInTheDocument()
  })

  it('shows an error state when the snapshot fails to load', async () => {
    fetchMock.mockRejectedValue(new Error('boom'))
    renderPage()
    expect(await screen.findByText('Failed to load the system status.')).toBeInTheDocument()
  })

  it('reports a rejected mapy.com key as a degraded map backend', async () => {
    fetchMock.mockResolvedValue(
      status({
        maps: {
          configured: true,
          state: 'key_rejected',
          degraded: true,
          detail: 'tile: mapy: upstream rejected the API key (status 403)',
          checked_at: '2026-06-01T10:00:00Z',
        },
      }),
    )
    renderPage()

    expect(await screen.findByText('Key rejected')).toBeInTheDocument()
    expect(screen.getByText(/rejecting the API key/)).toBeInTheDocument()
  })

  it('reports a healthy map backend without alarming the admin', async () => {
    renderPage()

    expect(await screen.findByText('Healthy')).toBeInTheDocument()
    expect(screen.queryByText(/rejecting the API key/)).not.toBeInTheDocument()
  })

  it('reports maps as not configured when no mapy.com key is set', async () => {
    fetchMock.mockResolvedValue(
      status({ maps: { configured: false, state: 'unknown', degraded: false } }),
    )
    renderPage()

    expect(await screen.findByText('Not configured')).toBeInTheDocument()
  })

  it('shows the geocode credit spend against its budget', async () => {
    renderPage()

    expect(await screen.findByText('120 / 1000')).toBeInTheDocument()
    expect(screen.getByText(/Geocoding credits this window/)).toBeInTheDocument()
    expect(screen.getByText(/Budget refills/)).toBeInTheDocument()
  })

  it('flags an exhausted geocode budget and when it refills', async () => {
    fetchMock.mockResolvedValue(
      status({
        geocode: {
          configured: true,
          budget_enabled: true,
          limit: 1000,
          spent: 1000,
          remaining: 0,
          window_seconds: 86400,
          resets_at: '2026-06-02T09:00:00Z',
        },
      }),
    )
    renderPage()

    expect(await screen.findByText('1000 / 1000')).toBeInTheDocument()
    expect(screen.getByText(/Budget spent, refills/)).toBeInTheDocument()
  })

  it('hides the credit line when no budget caps the geocode spend', async () => {
    fetchMock.mockResolvedValue(
      status({
        geocode: {
          configured: true,
          budget_enabled: false,
          limit: 0,
          spent: 0,
          remaining: 0,
          window_seconds: 86400,
        },
      }),
    )
    renderPage()

    expect(await screen.findByText('Healthy')).toBeInTheDocument()
    expect(screen.queryByText(/Geocoding credits/)).not.toBeInTheDocument()
  })
})

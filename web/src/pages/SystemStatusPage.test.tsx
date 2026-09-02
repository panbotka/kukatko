import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import type { SystemStatus } from '../services/system'

import { SystemStatusPage } from './SystemStatusPage'

// Mock the system service module so the page's data and actions are controlled.
vi.mock('../services/system', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/system')>()
  return {
    ...actual,
    fetchSystemStatus: vi.fn(),
    requeueDeadLetterJobs: vi.fn(),
    triggerBackup: vi.fn(),
  }
})

const { fetchSystemStatus, requeueDeadLetterJobs, triggerBackup } =
  await import('../services/system')
const fetchMock = vi.mocked(fetchSystemStatus)
const requeueMock = vi.mocked(requeueDeadLetterJobs)
const backupMock = vi.mocked(triggerBackup)

// status builds a full snapshot, with overrides merged shallowly per section.
function status(overrides: Partial<SystemStatus> = {}): SystemStatus {
  return {
    version: { version: '1.2.3', commit: 'abc1234' },
    database: { reachable: true },
    embeddings: { online: false, url: 'http://box:8000' },
    jobs: {
      by_state: { queued: 4, running: 1, failed: 2, dead: 2 },
      by_type: { image_embed: 7, ocr: 2 },
      by_type_state: {
        image_embed: { queued: 4, running: 1, done: 2 },
        ocr: { failed: 2, dead: 2 },
      },
      total: 11,
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
    library: {
      photos: 20310,
      videos: 145,
      trashed: 12,
      hidden: 3,
      private: 1,
      uploads: { day: 4, week: 40, month: 120, year: 5000 },
      albums: 88,
      labels: 42,
      people: 61,
      faces: 30000,
      embeddings: 20000,
      library_bytes: 82678120448,
      trash_bytes: 52428800,
      derived_bytes: 524288,
    },
    remaining: {
      faces_unassigned: 900,
      clusters: 7,
      photos_without_taken_at: 12,
      photos_without_gps: 8000,
      photos_without_place: 8100,
      photos_without_ocr: 300,
      duplicate_markers: 2,
      duplicates: {
        configured: true,
        available: true,
        groups: 14,
        computed_at: '2026-06-01T09:55:00Z',
      },
    },
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

/**
 * A snapshot of a freshly installed instance: nothing imported, nothing queued,
 * no backlog. It is the state the dashboard is most likely to be misread in — a
 * zero must read as "nothing yet", not as a problem.
 */
function emptyStatus(): SystemStatus {
  return status({
    jobs: {
      by_state: {},
      by_type: {},
      by_type_state: {},
      total: 0,
      dead_letter: 0,
      pending_embeddings: 0,
    },
    library: {
      photos: 0,
      videos: 0,
      trashed: 0,
      hidden: 0,
      private: 0,
      uploads: { day: 0, week: 0, month: 0, year: 0 },
      albums: 0,
      labels: 0,
      people: 0,
      faces: 0,
      embeddings: 0,
      library_bytes: 0,
      trash_bytes: 0,
      derived_bytes: 0,
    },
    remaining: {
      faces_unassigned: 0,
      clusters: 0,
      photos_without_taken_at: 0,
      photos_without_gps: 0,
      photos_without_place: 0,
      photos_without_ocr: 0,
      duplicate_markers: 0,
      duplicates: { configured: false, available: false, groups: 0 },
    },
  })
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
  requeueMock.mockReset()
  backupMock.mockReset()
  fetchMock.mockResolvedValue(status())
  requeueMock.mockResolvedValue(2)
  backupMock.mockResolvedValue(undefined)
})

// Deliberately no `afterEach(vi.restoreAllMocks)`: `restoreMocks: true` in
// vite.config.ts already restores every mock BEFORE each test, and beforeEach
// re-stubs them. Restoring here as well emptied the module mocks while the tree
// was still mounted — RTL's own `cleanup()` afterEach then unmounted it, React
// flushed the pending passive effects, and the page's data effect called a mock
// with no implementation: the fetch returned undefined and the page's `.then`
// threw. That failed a varying test of this file whenever the full suite ran
// busy enough for the effects to flush that late.

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

  it('renders each health card from the polled snapshot', async () => {
    renderPage()

    // Each card heading renders.
    expect(await screen.findByText('Database')).toBeInTheDocument()
    expect(screen.getByText('Content and face recognition')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Background work' })).toBeInTheDocument()
    expect(screen.getByText('Backup')).toBeInTheDocument()
    expect(screen.getByText('Imports')).toBeInTheDocument()
    // The disk card says whose disk it is: the library's own size lives in the
    // Library section and the two used to be confused for each other.
    expect(screen.getByText('Server disk')).toBeInTheDocument()
    expect(screen.getByText(/almost none of them/)).toBeInTheDocument()

    // Section values: reachable DB, unavailable recognition, disk size, version.
    expect(screen.getByText('Reachable')).toBeInTheDocument()
    expect(screen.getByText('Unavailable')).toBeInTheDocument()
    expect(screen.getByText('1.0 MB')).toBeInTheDocument()
    expect(screen.getByText('1.2.3')).toBeInTheDocument()
  })

  it('reports the age of the last backup and the last import', async () => {
    renderPage()

    // Both are 2026-06-01 in the fixture, so both read as "X years ago" against a
    // real clock; what matters is that an age is reported at all, next to the stamp.
    const ages = await screen.findAllByText(/^When: /)
    expect(ages).toHaveLength(2)
  })

  it('explains the queue states in plain language, recognition-service wait included', async () => {
    renderPage()

    // The queue is introduced and the tricky states are explained without hover.
    expect(
      await screen.findByText(/works through in the background, by kind of work/),
    ).toBeInTheDocument()
    expect(screen.getByText(/went wrong even after several attempts/)).toBeInTheDocument()
    // The extra pending state (work waiting for the recognition service) too.
    expect(screen.getByText(/waiting for the service to come back/)).toBeInTheDocument()
  })

  it('breaks the queue down by type and state instead of one lifetime total', async () => {
    renderPage()

    // One row per job type, each split across the states its work is actually in.
    expect(await screen.findByTestId('job-image_embed-queued')).toHaveTextContent('4')
    expect(screen.getByTestId('job-image_embed-done')).toHaveTextContent('2')
    expect(screen.getByTestId('job-ocr-dead')).toHaveTextContent('2')
    // The lifetime tally is still there, but explicitly labelled as history.
    expect(screen.getByText(/counts everything Kukátko has ever worked on/)).toBeInTheDocument()
  })

  it('requeues the dead letter of one job type from its row', async () => {
    const user = userEvent.setup()
    requeueMock.mockResolvedValue(2)
    renderPage()

    // Only the type with a dead letter offers the action.
    const row = await screen.findByTestId('job-row-ocr')
    await user.click(within(row).getByRole('button', { name: 'Retry' }))

    await waitFor(() => {
      expect(requeueMock).toHaveBeenCalledWith('ocr')
    })
    expect(await screen.findByText('2 jobs went back into the queue.')).toBeInTheDocument()
    expect(within(screen.getByTestId('job-row-image_embed')).queryByRole('button')).toBeNull()
  })

  it('shows the queued-work hint while recognition is unavailable', async () => {
    renderPage()
    expect(
      await screen.findByText(
        'The service is unavailable → 5 photos are waiting in the queue. Kukátko catches up as soon as it is back.',
      ),
    ).toBeInTheDocument()
  })

  it('requeues the whole dead letter via the quick action', async () => {
    const user = userEvent.setup()
    renderPage()

    const button = await screen.findByRole('button', { name: 'Retry the permanently failed' })
    await user.click(button)

    await waitFor(() => {
      expect(requeueMock).toHaveBeenCalledWith(undefined)
    })
    expect(await screen.findByText('2 jobs went back into the queue.')).toBeInTheDocument()
  })

  it('disables the requeue action when there are no dead-letter jobs', async () => {
    fetchMock.mockResolvedValue(
      status({
        jobs: {
          by_state: { queued: 1 },
          by_type: { thumbnail: 1 },
          by_type_state: { thumbnail: { queued: 1 } },
          total: 1,
          dead_letter: 0,
          pending_embeddings: 0,
        },
      }),
    )
    renderPage()
    const button = await screen.findByRole('button', { name: 'Retry the permanently failed' })
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
    expect(screen.getByRole('link', { name: 'Library maintenance' })).toHaveAttribute(
      'href',
      '/maintenance',
    )
  })

  it('leads with the library, from the one snapshot it already fetched', async () => {
    renderPage()

    expect(await screen.findByRole('heading', { name: 'Library' })).toBeInTheDocument()
    expect(screen.getByTestId('tile-photos')).toHaveTextContent('20,310')
    expect(screen.getByTestId('tile-videos')).toHaveTextContent('145')
    expect(screen.getByTestId('tile-trashed')).toHaveTextContent('12')
    expect(screen.getByTestId('tile-hidden')).toHaveTextContent('3')
    expect(screen.getByTestId('tile-private')).toHaveTextContent('1')
    expect(screen.getByTestId('tile-people')).toHaveTextContent('61')
    // No second fetch: the dashboard is one snapshot, so nothing can drift.
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('links every library number that has a view behind it', async () => {
    renderPage()

    expect(await screen.findByRole('link', { name: 'Photos in the library' })).toHaveAttribute(
      'href',
      '/',
    )
    expect(screen.getByRole('link', { name: 'of which videos' })).toHaveAttribute(
      'href',
      '/?q=type%3Avideo',
    )
    expect(screen.getByRole('link', { name: 'In the trash' })).toHaveAttribute('href', '/trash')
    expect(screen.getByRole('link', { name: 'Hidden' })).toHaveAttribute('href', '/?q=hidden%3Ayes')
    // A number with nowhere to go is plain text, not a link that lands elsewhere.
    expect(screen.queryByRole('link', { name: 'Detected faces' })).toBeNull()
  })

  it('reports the uploads and what the library weighs per the catalogue', async () => {
    renderPage()

    expect(await screen.findByTestId('uploads-day')).toHaveTextContent('4')
    expect(screen.getByTestId('uploads-year')).toHaveTextContent('5,000')
    // The catalogue's own arithmetic, which is the number that is meaningful when
    // the originals live in an object store and the server's disk holds none.
    expect(screen.getByTestId('catalogue-storage-library')).toHaveTextContent('77.0 GB')
    expect(screen.getByTestId('catalogue-storage-trash')).toHaveTextContent('50.0 MB')
    expect(screen.getByTestId('catalogue-storage-derived')).toHaveTextContent('512.0 KB')
  })

  it('lists the remaining work and links it to where it is done', async () => {
    renderPage()

    expect(await screen.findByRole('heading', { name: 'Remaining work' })).toBeInTheDocument()
    expect(screen.getByTestId('tile-faces-unassigned')).toHaveTextContent('900')
    expect(screen.getByTestId('tile-clusters')).toHaveTextContent('7')
    expect(screen.getByTestId('tile-without-gps')).toHaveTextContent('8,000')
    expect(screen.getByTestId('tile-without-ocr')).toHaveTextContent('300')
    expect(screen.getByTestId('tile-duplicates')).toHaveTextContent('14')
    expect(screen.getByRole('link', { name: 'Groups of faces to name' })).toHaveAttribute(
      'href',
      '/people/clusters',
    )
    expect(screen.getByRole('link', { name: 'Duplicate markers' })).toHaveAttribute(
      'href',
      '/duplicate-markers',
    )
    expect(screen.getByRole('link', { name: 'Without coordinates' })).toHaveAttribute(
      'href',
      '/?q=geo%3Ano',
    )
  })

  it('says the duplicate scan has no answer yet instead of showing a zero', async () => {
    fetchMock.mockResolvedValue(
      status({
        remaining: {
          ...status().remaining,
          duplicates: { configured: true, available: false, groups: 0 },
        },
      }),
    )
    renderPage()

    expect(await screen.findByText('Scanning in the background…')).toBeInTheDocument()
    expect(screen.getByTestId('tile-duplicates')).toHaveTextContent('—')
  })

  it('renders a fresh, empty instance without pretending anything is wrong', async () => {
    fetchMock.mockResolvedValue(emptyStatus())
    renderPage()

    expect(await screen.findByTestId('tile-photos')).toHaveTextContent('0')
    expect(screen.getByTestId('catalogue-storage-library')).toHaveTextContent('0 B')
    expect(
      screen.getByText('Nothing to do — there is no background work waiting.'),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Retry the permanently failed' })).toBeDisabled()
    // A zeroed backlog is the goal, not a warning: nothing is highlighted.
    expect(screen.getByTestId('tile-faces-unassigned')).not.toHaveClass('text-warning')
  })

  it('puts the announcement box after the dashboard, not before it', async () => {
    renderPage()

    await screen.findByRole('heading', { name: 'Library' })
    const headings = screen.getAllByRole('heading').map((heading) => heading.textContent)
    expect(headings.indexOf('Library')).toBeLessThan(headings.indexOf('Announcement'))
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
    expect(screen.getByText(/rejecting the access key/)).toBeInTheDocument()
  })

  it('reports a healthy map backend without alarming the admin', async () => {
    renderPage()

    expect(await screen.findByText('Healthy')).toBeInTheDocument()
    expect(screen.queryByText(/rejecting the access key/)).not.toBeInTheDocument()
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

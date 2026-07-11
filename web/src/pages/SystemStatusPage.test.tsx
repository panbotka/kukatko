import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

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
      by_state: { queued: 4, running: 1, failed: 2 },
      by_type: { image_embed: 3 },
      total: 7,
      dead_letter: 2,
      pending_embeddings: 5,
    },
    backup: { configured: true, running: false, last_finished_at: '2026-06-01T10:00:00Z' },
    imports: {
      photoprism: {
        id: 1,
        source: 'photoprism',
        started_at: '2026-06-01T09:00:00Z',
        finished_at: '2026-06-01T09:30:00Z',
        status: 'done',
        high_watermark: null,
        counts: { imported: 9, updated: 0, skipped: 0, failed: 0 },
        last_error: '',
      },
      photosorter: null,
    },
    storage: {
      originals_bytes: 1048576,
      cache_bytes: 524288,
      free_bytes: 2147483648,
      total_bytes: 4294967296,
    },
    ...overrides,
  }
}

// auth builds an AuthContext value with the given admin flag.
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

// renderPage renders the dashboard within auth + i18n + router providers.
function renderPage(isAdmin = true) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(isAdmin)}>
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

afterEach(() => {
  vi.restoreAllMocks()
})

describe('SystemStatusPage', () => {
  it('denies access to non-admins and never fetches', async () => {
    renderPage(false)
    expect(
      await screen.findByText('This page is available to administrators only.'),
    ).toBeInTheDocument()
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
    expect(await screen.findByRole('link', { name: 'Trigger import' })).toHaveAttribute(
      'href',
      '/import',
    )
    expect(screen.getByRole('link', { name: 'Maintenance scan' })).toHaveAttribute(
      'href',
      '/maintenance',
    )
  })

  it('shows an error state when the snapshot fails to load', async () => {
    fetchMock.mockRejectedValue(new Error('boom'))
    renderPage()
    expect(await screen.findByText('Failed to load the system status.')).toBeInTheDocument()
  })
})

import { act, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { notifyTaskChanged } from '../lib/taskChanges'
import { type TaskSummary } from '../services/tasks'

import { useTaskSummary } from './TaskSummaryContext'
import { REFRESH_INTERVAL_MS, TaskSummaryProvider } from './TaskSummaryProvider'

vi.mock('../services/tasks', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/tasks')>()
  return { ...actual, fetchTaskSummary: vi.fn() }
})

const { fetchTaskSummary } = await import('../services/tasks')
const fetchMock = vi.mocked(fetchTaskSummary)

function counts(waiting: number): TaskSummary {
  return {
    by_state: { question: waiting, working: 0, review: 0, done: 0, rejected: 0 },
    open: waiting,
    waiting_on_me: waiting,
    answered: 0,
  }
}

function auth(status: AuthContextValue['status']): AuthContextValue {
  return {
    status,
    user: status === 'authenticated' ? { uid: 'u1', username: 'u', role: 'viewer' } : null,
  } as unknown as AuthContextValue
}

/** Prints the count the provider holds, or "none". */
function Probe() {
  const { summary } = useTaskSummary()
  return <output>{summary === null ? 'none' : String(summary.waiting_on_me)}</output>
}

function renderProvider(status: AuthContextValue['status'] = 'authenticated') {
  return render(
    <AuthContext.Provider value={auth(status)}>
      <TaskSummaryProvider>
        <Probe />
      </TaskSummaryProvider>
    </AuthContext.Provider>,
  )
}

beforeEach(() => {
  fetchMock.mockReset()
  fetchMock.mockResolvedValue(counts(3))
})

afterEach(() => {
  vi.useRealTimers()
})

describe('TaskSummaryProvider', () => {
  it('fetches once on mount for a signed-in reader', async () => {
    renderProvider()

    expect(await screen.findByText('3')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('never asks for an anonymous visitor', () => {
    renderProvider('unauthenticated')

    expect(screen.getByText('none')).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('refreshes at once after a task write from this browser', async () => {
    renderProvider()
    await screen.findByText('3')

    fetchMock.mockResolvedValue(counts(2))
    act(() => {
      notifyTaskChanged()
    })

    expect(await screen.findByText('2')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('hides the count when a fetch fails', async () => {
    renderProvider()
    await screen.findByText('3')

    fetchMock.mockRejectedValue(new Error('offline'))
    act(() => {
      notifyTaskChanged()
    })

    expect(await screen.findByText('none')).toBeInTheDocument()
  })

  it('polls once a minute while the app is open', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    renderProvider()
    await screen.findByText('3')

    fetchMock.mockResolvedValue(counts(4))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(REFRESH_INTERVAL_MS)
    })

    await waitFor(() => {
      expect(screen.getByText('4')).toBeInTheDocument()
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})

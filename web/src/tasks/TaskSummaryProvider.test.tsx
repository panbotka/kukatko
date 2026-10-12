import { act, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { notifyTaskChanged } from '../lib/taskChanges'
import { ProjectContext, type ProjectContextValue } from '../project/ProjectContext'
import { type ProjectMembership } from '../services/auth'
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

/** The project context a provider sees, working in the project `slug`. */
function inProject(slug: string): ProjectContextValue {
  const project: ProjectMembership = {
    uid: `pr-${slug}`,
    slug,
    name: slug,
    active: true,
    role: 'viewer',
    approved: true,
  }
  return { project, projects: [project], status: 'ready', inAddress: true, switchProject: vi.fn() }
}

/** Mounts the provider inside the project `slug`; `rerender` with another to switch. */
function providerIn(slug: string) {
  return (
    <AuthContext.Provider value={auth('authenticated')}>
      <ProjectContext.Provider value={inProject(slug)}>
        <TaskSummaryProvider>
          <Probe />
        </TaskSummaryProvider>
      </ProjectContext.Provider>
    </AuthContext.Provider>
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

  it("drops the old project's counts and fetches the new project's on a switch", async () => {
    const { rerender } = render(providerIn('v'))
    expect(await screen.findByText('3')).toBeInTheDocument()

    let resolve: (summary: TaskSummary) => void = () => undefined
    fetchMock.mockImplementation(
      () =>
        new Promise<TaskSummary>((done) => {
          resolve = done
        }),
    )
    rerender(providerIn('jiny'))

    // The other project's counts never stand in for this one's while they load.
    expect(screen.getByText('none')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(2)
    act(() => {
      resolve(counts(7))
    })
    expect(await screen.findByText('7')).toBeInTheDocument()
  })

  it("aborts the old project's request when the project changes mid-flight", () => {
    fetchMock.mockImplementation(() => new Promise<TaskSummary>(() => undefined))
    const { rerender } = render(providerIn('v'))
    const firstSignal = fetchMock.mock.calls[0][0]

    rerender(providerIn('jiny'))

    expect(firstSignal?.aborted).toBe(true)
    expect(fetchMock).toHaveBeenCalledTimes(2)
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

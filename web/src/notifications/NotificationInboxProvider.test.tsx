import { act, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { ProjectContext, type ProjectContextValue } from '../project/ProjectContext'
import { type ProjectMembership } from '../services/auth'

import { useNotificationInbox } from './NotificationInboxContext'
import { NotificationInboxProvider, REFRESH_INTERVAL_MS } from './NotificationInboxProvider'

vi.mock('../services/notifications', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/notifications')>()
  return { ...actual, fetchUnreadNotificationCount: vi.fn() }
})

const { fetchUnreadNotificationCount } = await import('../services/notifications')
const fetchMock = vi.mocked(fetchUnreadNotificationCount)

function auth(status: AuthContextValue['status']): AuthContextValue {
  return {
    status,
    user: status === 'authenticated' ? { uid: 'u1', username: 'u', role: 'viewer' } : null,
  } as unknown as AuthContextValue
}

/** Prints the count the provider holds, or "none", and offers its refresh. */
function Probe() {
  const { unread, refresh } = useNotificationInbox()
  return (
    <>
      <output>{unread === null ? 'none' : String(unread)}</output>
      <button type="button" onClick={refresh}>
        refresh
      </button>
    </>
  )
}

function renderProvider(status: AuthContextValue['status'] = 'authenticated') {
  return render(
    <AuthContext.Provider value={auth(status)}>
      <NotificationInboxProvider>
        <Probe />
      </NotificationInboxProvider>
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
  return { project, projects: [project], status: 'ready', switchProject: vi.fn() }
}

/** Mounts the provider inside the project `slug`; `rerender` with another to switch. */
function providerIn(slug: string) {
  return (
    <AuthContext.Provider value={auth('authenticated')}>
      <ProjectContext.Provider value={inProject(slug)}>
        <NotificationInboxProvider>
          <Probe />
        </NotificationInboxProvider>
      </ProjectContext.Provider>
    </AuthContext.Provider>
  )
}

beforeEach(() => {
  fetchMock.mockReset()
  fetchMock.mockResolvedValue(3)
})

afterEach(() => {
  vi.useRealTimers()
})

describe('NotificationInboxProvider', () => {
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

  it('refreshes when asked and when the window regains focus', async () => {
    renderProvider()
    await screen.findByText('3')

    fetchMock.mockResolvedValue(1)
    act(() => {
      screen.getByRole('button', { name: 'refresh' }).click()
    })
    expect(await screen.findByText('1')).toBeInTheDocument()

    fetchMock.mockResolvedValue(0)
    act(() => {
      window.dispatchEvent(new Event('focus'))
    })
    expect(await screen.findByText('0')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('hides the count when a fetch fails', async () => {
    renderProvider()
    await screen.findByText('3')

    fetchMock.mockRejectedValue(new Error('offline'))
    act(() => {
      window.dispatchEvent(new Event('focus'))
    })

    expect(await screen.findByText('none')).toBeInTheDocument()
  })

  it("drops the old project's count and fetches the new project's on a switch", async () => {
    const { rerender } = render(providerIn('v'))
    expect(await screen.findByText('3')).toBeInTheDocument()

    let resolve: (count: number) => void = () => undefined
    fetchMock.mockImplementation(
      () =>
        new Promise<number>((done) => {
          resolve = done
        }),
    )
    rerender(providerIn('jiny'))

    // The other project's count never stands in for this one's while it loads.
    expect(screen.getByText('none')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(2)
    act(() => {
      resolve(5)
    })
    expect(await screen.findByText('5')).toBeInTheDocument()
  })

  it("aborts the old project's request when the project changes mid-flight", () => {
    fetchMock.mockImplementation(() => new Promise<number>(() => undefined))
    const { rerender } = render(providerIn('v'))
    const firstSignal = fetchMock.mock.calls[0][0]

    rerender(providerIn('jiny'))

    expect(firstSignal?.aborted).toBe(true)
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('polls once a minute while the tab is visible', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    renderProvider()
    await screen.findByText('3')

    fetchMock.mockResolvedValue(4)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(REFRESH_INTERVAL_MS)
    })

    await waitFor(() => {
      expect(screen.getByText('4')).toBeInTheDocument()
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})

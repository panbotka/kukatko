import { act, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'

import { CapabilitiesProvider } from './CapabilitiesProvider'
import { useCapabilities } from './CapabilitiesContext'

vi.mock('../services/capabilities', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/capabilities')>()
  return { ...actual, fetchCapabilities: vi.fn() }
})

const { fetchCapabilities } = await import('../services/capabilities')
const capsMock = vi.mocked(fetchCapabilities)

/** Reads the context and renders the two flags as stable, assertable strings. */
function Probe() {
  const caps = useCapabilities()
  return (
    <>
      <span data-testid="semantic">{String(caps.semantic_search)}</span>
      <span data-testid="known">{String(caps.known)}</span>
    </>
  )
}

/** The slice of the auth context the provider reads: only the status. */
function auth(status: AuthContextValue['status']): AuthContextValue {
  return { status } as unknown as AuthContextValue
}

/** Renders a Probe inside the provider under test, signed in unless told otherwise. */
function renderProvider(status: AuthContextValue['status'] = 'authenticated') {
  const tree = (current: AuthContextValue['status']) => (
    <AuthContext.Provider value={auth(current)}>
      <CapabilitiesProvider>
        <Probe />
      </CapabilitiesProvider>
    </AuthContext.Provider>
  )
  const view = render(tree(status))
  const rerenderAs = (next: AuthContextValue['status']) => {
    view.rerender(tree(next))
  }
  return { ...view, rerenderAs }
}

/** Flushes the microtasks that settle the mocked fetch and its state update. */
async function flush() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })
}

beforeEach(() => {
  vi.useFakeTimers()
  capsMock.mockReset()
  capsMock.mockResolvedValue({ semantic_search: true, passkeys: false, video_streaming: false })
  Object.defineProperty(document, 'hidden', { configurable: true, get: () => false })
})

afterEach(() => {
  vi.runOnlyPendingTimers()
  vi.useRealTimers()
})

describe('CapabilitiesProvider', () => {
  it('starts with features off and turns them on after the first fetch', async () => {
    renderProvider()
    // Before the fetch settles the safe default (all off) is exposed, and it is
    // marked unknown so nothing reads that blank as a reported absence.
    expect(screen.getByTestId('semantic')).toHaveTextContent('false')
    expect(screen.getByTestId('known')).toHaveTextContent('false')

    await flush()
    expect(capsMock).toHaveBeenCalledTimes(1)
    expect(screen.getByTestId('semantic')).toHaveTextContent('true')
    expect(screen.getByTestId('known')).toHaveTextContent('true')
  })

  it('stays unknown while every fetch fails, so nothing reports a false outage', async () => {
    // The regression this guards: an expired session answers 401, the fetch
    // rejects on every attempt, and the untouched all-off default used to be
    // indistinguishable from an instance that had answered "semantic search is
    // unavailable" — which put an outage notice on a working feature.
    capsMock.mockRejectedValue(new Error('unauthorized'))
    renderProvider()
    await flush()

    expect(screen.getByTestId('semantic')).toHaveTextContent('false')
    expect(screen.getByTestId('known')).toHaveTextContent('false')
  })

  it('refreshes on the poll interval, reflecting the box going offline', async () => {
    renderProvider()
    await flush()
    expect(screen.getByTestId('semantic')).toHaveTextContent('true')

    // The box drops off: the next refresh reports semantic search unavailable.
    capsMock.mockResolvedValue({ semantic_search: false, passkeys: false, video_streaming: false })
    act(() => {
      vi.advanceTimersByTime(60_000)
    })
    await flush()
    expect(capsMock).toHaveBeenCalledTimes(2)
    expect(screen.getByTestId('semantic')).toHaveTextContent('false')
  })

  it('keeps the last known flags when a refresh fails', async () => {
    renderProvider()
    await flush()
    expect(screen.getByTestId('semantic')).toHaveTextContent('true')

    capsMock.mockRejectedValue(new Error('offline'))
    act(() => {
      vi.advanceTimersByTime(60_000)
    })
    await flush()
    // A failed probe must not wipe the last good state back to the default, nor
    // un-learn it: the instance has already said what it can do.
    expect(screen.getByTestId('semantic')).toHaveTextContent('true')
    expect(screen.getByTestId('known')).toHaveTextContent('true')
  })

  it('fetches nothing until the session is authenticated, then starts', async () => {
    // The route is project-scoped: with no session there is no project to ask.
    const { rerenderAs } = renderProvider('loading')
    await flush()
    rerenderAs('unauthenticated')
    await flush()
    expect(capsMock).not.toHaveBeenCalled()
    expect(screen.getByTestId('known')).toHaveTextContent('false')

    rerenderAs('authenticated')
    await flush()
    expect(capsMock).toHaveBeenCalledTimes(1)
    expect(screen.getByTestId('semantic')).toHaveTextContent('true')
  })
})

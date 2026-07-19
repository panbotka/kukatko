import { act, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { CapabilitiesProvider } from './CapabilitiesProvider'
import { useCapabilities } from './CapabilitiesContext'

vi.mock('../services/capabilities', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/capabilities')>()
  return { ...actual, fetchCapabilities: vi.fn() }
})

const { fetchCapabilities } = await import('../services/capabilities')
const capsMock = vi.mocked(fetchCapabilities)

/** Reads the context and renders semantic_search as a stable, assertable string. */
function Probe() {
  const caps = useCapabilities()
  return <span data-testid="semantic">{String(caps.semantic_search)}</span>
}

/** Renders a Probe inside the provider under test. */
function renderProvider() {
  return render(
    <CapabilitiesProvider>
      <Probe />
    </CapabilitiesProvider>,
  )
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
  capsMock.mockResolvedValue({ semantic_search: true })
  Object.defineProperty(document, 'hidden', { configurable: true, get: () => false })
})

afterEach(() => {
  vi.runOnlyPendingTimers()
  vi.useRealTimers()
})

describe('CapabilitiesProvider', () => {
  it('starts with features off and turns them on after the first fetch', async () => {
    renderProvider()
    // Before the fetch settles the safe default (all off) is exposed.
    expect(screen.getByTestId('semantic')).toHaveTextContent('false')

    await flush()
    expect(capsMock).toHaveBeenCalledTimes(1)
    expect(screen.getByTestId('semantic')).toHaveTextContent('true')
  })

  it('refreshes on the poll interval, reflecting the box going offline', async () => {
    renderProvider()
    await flush()
    expect(screen.getByTestId('semantic')).toHaveTextContent('true')

    // The box drops off: the next refresh reports semantic search unavailable.
    capsMock.mockResolvedValue({ semantic_search: false })
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
    // A failed probe must not wipe the last good state back to the default.
    expect(screen.getByTestId('semantic')).toHaveTextContent('true')
  })
})

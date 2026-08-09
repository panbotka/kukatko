import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { type SearchHistoryEntry } from '../services/searchHistory'

import { useRecordSearch, useSearchHistory } from './useSearchHistory'

vi.mock('../services/searchHistory', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/searchHistory')>()
  return {
    ...actual,
    fetchSearchHistory: vi.fn(),
    recordSearch: vi.fn(),
    clearSearchHistory: vi.fn(),
  }
})

const { fetchSearchHistory, recordSearch, clearSearchHistory } =
  await import('../services/searchHistory')
const fetchMock = vi.mocked(fetchSearchHistory)
const recordMock = vi.mocked(recordSearch)
const clearMock = vi.mocked(clearSearchHistory)

/** One history entry; only the query text matters here. */
function entry(query: string): SearchHistoryEntry {
  return { query, searched_at: '2026-08-09T12:00:00Z' }
}

beforeEach(() => {
  fetchMock.mockResolvedValue([])
  recordMock.mockResolvedValue()
  clearMock.mockResolvedValue()
})

describe('useSearchHistory', () => {
  it('fetches nothing until the dropdown could be seen', () => {
    renderHook(() => useSearchHistory(false))
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('loads on activation and refetches on the next one, so another device shows up', async () => {
    fetchMock.mockResolvedValue([entry('svatba')])
    const { result, rerender } = renderHook(({ active }) => useSearchHistory(active), {
      initialProps: { active: true },
    })

    await waitFor(() => {
      expect(result.current.entries).toHaveLength(1)
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    // Deactivating fetches nothing; reactivating re-reads the server's list.
    rerender({ active: false })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    fetchMock.mockResolvedValue([entry('svatba'), entry('hory')])
    rerender({ active: true })
    await waitFor(() => {
      expect(result.current.entries).toHaveLength(2)
    })
  })

  it('empties the list on clear before the request resolves', async () => {
    fetchMock.mockResolvedValue([entry('svatba')])
    // Replaced by the pending promise's resolver before anything can call it.
    let resolveClear: () => void = () => undefined
    clearMock.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          resolveClear = resolve
        }),
    )
    const { result } = renderHook(() => useSearchHistory(true))
    await waitFor(() => {
      expect(result.current.entries).toHaveLength(1)
    })

    act(() => {
      result.current.clear()
    })
    expect(result.current.entries).toEqual([])
    expect(clearMock).toHaveBeenCalledTimes(1)
    await act(() => {
      resolveClear()
      return Promise.resolve()
    })
    expect(result.current.entries).toEqual([])
  })

  it('leaves the list empty when the load fails, instead of surfacing an error', async () => {
    fetchMock.mockRejectedValue(new Error('offline'))
    const { result } = renderHook(() => useSearchHistory(true))
    await waitFor(() => {
      expect(result.current.loading).toBe(false)
    })
    expect(result.current.entries).toEqual([])
  })
})

describe('useRecordSearch', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('records a query only once it has stopped changing', () => {
    const { rerender } = renderHook(
      ({ query }: { query: string }) => {
        useRecordSearch(query, 500)
      },
      { initialProps: { query: 'sv' } },
    )

    // Every prefix of a slowly typed word is a real search on the page, so the
    // delay is what keeps `sv` and `sva` out of the history.
    act(() => {
      vi.advanceTimersByTime(300)
    })
    rerender({ query: 'sva' })
    act(() => {
      vi.advanceTimersByTime(300)
    })
    rerender({ query: 'svatba' })
    expect(recordMock).not.toHaveBeenCalled()

    act(() => {
      vi.advanceTimersByTime(500)
    })
    expect(recordMock).toHaveBeenCalledTimes(1)
    expect(recordMock.mock.calls[0][0]).toBe('svatba')
  })

  it('records nothing for a blank query', () => {
    renderHook(() => {
      useRecordSearch('   ', 500)
    })
    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(recordMock).not.toHaveBeenCalled()
  })

  it('does not record the same query twice', () => {
    const { rerender } = renderHook(
      ({ query }: { query: string }) => {
        useRecordSearch(query, 500)
      },
      { initialProps: { query: 'svatba' } },
    )
    act(() => {
      vi.advanceTimersByTime(500)
    })
    expect(recordMock).toHaveBeenCalledTimes(1)

    // A trailing space is the same search, and coming back to it (Back, a shared
    // link) must not append it again.
    rerender({ query: 'svatba ' })
    act(() => {
      vi.advanceTimersByTime(500)
    })
    expect(recordMock).toHaveBeenCalledTimes(1)
  })
})

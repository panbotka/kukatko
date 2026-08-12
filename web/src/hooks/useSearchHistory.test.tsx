import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

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
  it('records nothing on its own, however the query moves', () => {
    const { rerender } = renderHook(
      ({ query }: { query: string }) => {
        useRecordSearch()
        return query
      },
      { initialProps: { query: 'sv' } },
    )

    // Typing runs a search on every pause, and none of those is a submit: no
    // timer may turn `sv`, `sva` or even a settled `svatba` into an entry.
    rerender({ query: 'sva' })
    rerender({ query: 'svatba' })
    expect(recordMock).not.toHaveBeenCalled()
  })

  it('records the query it is called with, once', () => {
    const { result } = renderHook(() => useRecordSearch())

    act(() => {
      result.current('svatba')
    })

    expect(recordMock).toHaveBeenCalledTimes(1)
    expect(recordMock.mock.calls[0][0]).toBe('svatba')
  })

  it('records nothing for a blank query', () => {
    const { result } = renderHook(() => useRecordSearch())

    act(() => {
      result.current('   ')
    })

    expect(recordMock).not.toHaveBeenCalled()
  })

  it('does not record the same query twice in a row', () => {
    const { result } = renderHook(() => useRecordSearch())

    act(() => {
      result.current('svatba')
    })
    // A trailing space is the same search, and leaning on Enter must not append
    // it again.
    act(() => {
      result.current('svatba ')
    })

    expect(recordMock).toHaveBeenCalledTimes(1)
  })

  it('records the query again when the reader has searched for something else since', () => {
    const { result } = renderHook(() => useRecordSearch())

    act(() => {
      result.current('svatba')
      result.current('hory')
      result.current('svatba')
    })

    // Re-running an older search is what moves it back to the front of the ring.
    expect(recordMock.mock.calls.map(([query]) => query)).toEqual(['svatba', 'hory', 'svatba'])
  })

  it('keeps a failed record from being the last word on that query', async () => {
    recordMock.mockRejectedValueOnce(new Error('offline'))
    const { result } = renderHook(() => useRecordSearch())

    act(() => {
      result.current('svatba')
    })
    await waitFor(() => {
      expect(recordMock).toHaveBeenCalledTimes(1)
    })

    // The failure is swallowed, but not remembered as recorded: submitting the
    // same query again retries it.
    act(() => {
      result.current('svatba')
    })
    expect(recordMock).toHaveBeenCalledTimes(2)
  })
})

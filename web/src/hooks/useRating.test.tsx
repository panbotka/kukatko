import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useRating } from './useRating'

// Only the network call is faked; the hook's optimistic logic runs for real.
vi.mock('../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/photos')>()
  return { ...actual, ratePhoto: vi.fn() }
})

const { ratePhoto } = await import('../services/photos')
const rateMock = vi.mocked(ratePhoto)

beforeEach(() => {
  rateMock.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useRating', () => {
  it('sets the rating optimistically and calls the API', async () => {
    rateMock.mockResolvedValue(undefined)
    const { result } = renderHook(() => useRating('ph1', 0, 'none'))

    act(() => {
      result.current.setRating(4)
    })

    // Optimistic update is immediate (before the request resolves).
    expect(result.current.rating).toBe(4)
    expect(rateMock).toHaveBeenCalledWith('ph1', { rating: 4 })
    await waitFor(() => {
      expect(result.current.pending).toBe(false)
    })
    expect(result.current.rating).toBe(4)
  })

  it('rolls back the rating when the request fails', async () => {
    rateMock.mockRejectedValue(new Error('boom'))
    const { result } = renderHook(() => useRating('ph1', 2, 'none'))

    act(() => {
      result.current.setRating(5)
    })
    expect(result.current.rating).toBe(5)

    await waitFor(() => {
      expect(result.current.rating).toBe(2)
    })
  })

  it('sets the flag optimistically and rolls back on failure', async () => {
    rateMock.mockRejectedValue(new Error('boom'))
    const { result } = renderHook(() => useRating('ph1', 0, 'none'))

    act(() => {
      result.current.setFlag('reject')
    })
    expect(result.current.flag).toBe('reject')
    expect(rateMock).toHaveBeenCalledWith('ph1', { flag: 'reject' })

    await waitFor(() => {
      expect(result.current.flag).toBe('none')
    })
  })

  it('ignores a no-op set to the current value', () => {
    rateMock.mockResolvedValue(undefined)
    const { result } = renderHook(() => useRating('ph1', 3, 'pick'))

    act(() => {
      result.current.setRating(3)
      result.current.setFlag('pick')
    })
    expect(rateMock).not.toHaveBeenCalled()
  })
})

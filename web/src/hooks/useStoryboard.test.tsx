import { renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import * as photosService from '../services/photos'

import { useStoryboard } from './useStoryboard'

vi.mock('../services/photos', async () => {
  const actual = await vi.importActual<typeof photosService>('../services/photos')
  return { ...actual, fetchStoryboard: vi.fn() }
})

const fetchStoryboard = vi.mocked(photosService.fetchStoryboard)

const ready: photosService.Storyboard = {
  status: 'ready',
  columns: 10,
  rows: 1,
  count: 10,
  tile_width: 160,
  tile_height: 90,
  interval_ms: 2000,
}

beforeEach(() => {
  fetchStoryboard.mockResolvedValue({ status: 'unavailable' })
})

afterEach(() => {
  vi.useRealTimers()
})

describe('useStoryboard', () => {
  it('asks for nothing while disabled', () => {
    renderHook(() => useStoryboard('ph1', false))
    expect(fetchStoryboard).not.toHaveBeenCalled()
  })

  it('returns the grid once the sprite is ready', async () => {
    fetchStoryboard.mockResolvedValue(ready)
    const { result } = renderHook(() => useStoryboard('ph1', true))
    await waitFor(() => {
      expect(result.current).toEqual(ready)
    })
  })

  it('stays null — and stops asking — when the photo can never have one', async () => {
    const { result } = renderHook(() => useStoryboard('ph1', true))
    await waitFor(() => {
      expect(fetchStoryboard).toHaveBeenCalledTimes(1)
    })
    expect(result.current).toBeNull()
  })

  it('stays null when the request fails, so the player just has no preview', async () => {
    fetchStoryboard.mockRejectedValue(new Error('boom'))
    const { result } = renderHook(() => useStoryboard('ph1', true))
    await waitFor(() => {
      expect(fetchStoryboard).toHaveBeenCalled()
    })
    expect(result.current).toBeNull()
  })

  it('re-asks while the sprite is still being generated, then settles on ready', async () => {
    vi.useFakeTimers()
    fetchStoryboard.mockResolvedValueOnce({ status: 'pending' }).mockResolvedValue(ready)
    const { result } = renderHook(() => useStoryboard('ph1', true))

    await vi.waitFor(() => {
      expect(fetchStoryboard).toHaveBeenCalledTimes(1)
    })
    await vi.advanceTimersByTimeAsync(5000)
    await vi.waitFor(() => {
      expect(result.current).toEqual(ready)
    })
  })

  it('gives up after a bounded number of polls rather than asking forever', async () => {
    vi.useFakeTimers()
    fetchStoryboard.mockResolvedValue({ status: 'pending' })
    renderHook(() => useStoryboard('ph1', true))

    await vi.advanceTimersByTimeAsync(5000 * 20)
    // The first ask plus four polls, and then it stops.
    expect(fetchStoryboard).toHaveBeenCalledTimes(5)
  })

  it('drops the previous clip’s grid the moment the photo changes', async () => {
    fetchStoryboard.mockResolvedValue(ready)
    const { result, rerender } = renderHook(({ uid }) => useStoryboard(uid, true), {
      initialProps: { uid: 'ph1' },
    })
    await waitFor(() => {
      expect(result.current).toEqual(ready)
    })

    fetchStoryboard.mockResolvedValue({ status: 'pending' })
    rerender({ uid: 'ph2' })
    expect(result.current).toBeNull()
  })
})

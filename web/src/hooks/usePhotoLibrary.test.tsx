import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { type Photo, type PhotoListParams, type PhotoListResponse } from '../services/photos'

import { usePhotoLibrary } from './usePhotoLibrary'

// Mock the data service: the hook is the unit under test, the network is not.
vi.mock('../services/photos', () => ({
  fetchPhotos: vi.fn(),
}))

const { fetchPhotos } = await import('../services/photos')
const fetchMock = vi.mocked(fetchPhotos)

/** Builds a minimal photo with the given uid. */
function photo(uid: string): Photo {
  return {
    uid,
    file_hash: uid,
    file_name: `${uid}.jpg`,
    file_size: 1,
    file_mime: 'image/jpeg',
    file_width: 1,
    file_height: 1,
    taken_at_source: 'exif',
    title: '',
    description: '',
    camera_make: '',
    camera_model: '',
    lens_model: '',
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

function page(photos: Photo[], total: number, nextOffset: number | null): PhotoListResponse {
  return { photos, total, limit: 100, offset: 0, next_offset: nextOffset }
}

beforeEach(() => {
  fetchMock.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('usePhotoLibrary', () => {
  it('loads the first page and exposes the photos and total', async () => {
    fetchMock.mockResolvedValue(page([photo('a'), photo('b')], 2, null))

    const { result } = renderHook(() => usePhotoLibrary({ sort: 'newest' }))

    expect(result.current.status).toBe('loading')
    await waitFor(() => {
      expect(result.current.status).toBe('ready')
    })
    expect(result.current.photos.map((p) => p.uid)).toEqual(['a', 'b'])
    expect(result.current.total).toBe(2)
    expect(result.current.hasMore).toBe(false)
  })

  it('reports ready with no photos for an empty result', async () => {
    fetchMock.mockResolvedValue(page([], 0, null))

    const { result } = renderHook(() => usePhotoLibrary({ sort: 'newest' }))

    await waitFor(() => {
      expect(result.current.status).toBe('ready')
    })
    expect(result.current.photos).toEqual([])
    expect(result.current.total).toBe(0)
  })

  it('surfaces an error and recovers on retry', async () => {
    fetchMock.mockRejectedValueOnce(new Error('boom'))

    const { result } = renderHook(() => usePhotoLibrary({ sort: 'newest' }))

    await waitFor(() => {
      expect(result.current.status).toBe('error')
    })

    fetchMock.mockResolvedValueOnce(page([photo('a')], 1, null))
    act(() => {
      result.current.retry()
    })

    await waitFor(() => {
      expect(result.current.status).toBe('ready')
    })
    expect(result.current.photos.map((p) => p.uid)).toEqual(['a'])
  })

  it('appends the next page and requests it at the server-provided offset', async () => {
    fetchMock.mockResolvedValueOnce(page([photo('a')], 3, 1))

    const { result } = renderHook(() => usePhotoLibrary({ sort: 'newest' }))
    await waitFor(() => {
      expect(result.current.status).toBe('ready')
    })
    expect(result.current.hasMore).toBe(true)

    fetchMock.mockResolvedValueOnce(page([photo('b'), photo('c')], 3, null))
    act(() => {
      result.current.loadMore()
    })

    await waitFor(() => {
      expect(result.current.photos).toHaveLength(3)
    })
    expect(result.current.photos.map((p) => p.uid)).toEqual(['a', 'b', 'c'])
    expect(result.current.hasMore).toBe(false)

    // The second call must request the next page at the offset the server gave.
    const secondCall = fetchMock.mock.calls[1][0]
    expect(secondCall.offset).toBe(1)
  })

  it('reloads from the first page when the params change', async () => {
    fetchMock.mockResolvedValue(page([photo('a')], 1, null))

    const { result, rerender } = renderHook((props: PhotoListParams) => usePhotoLibrary(props), {
      initialProps: { sort: 'newest' },
    })
    await waitFor(() => {
      expect(result.current.status).toBe('ready')
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    fetchMock.mockResolvedValue(page([photo('z')], 1, null))
    rerender({ sort: 'oldest' })

    await waitFor(() => {
      expect(result.current.photos.map((p) => p.uid)).toEqual(['z'])
    })
    const calls = fetchMock.mock.calls
    const lastCall = calls[calls.length - 1][0]
    expect(lastCall.sort).toBe('oldest')
    expect(lastCall.offset).toBe(0)
  })
})

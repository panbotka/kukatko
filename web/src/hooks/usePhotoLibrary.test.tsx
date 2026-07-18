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
    thumb_url: `/api/v1/photos/${uid}/thumb/tile_500`,
    download_url: `/api/v1/photos/${uid}/download?original=true`,
    title: '',
    description: '',
    camera_make: '',
    camera_model: '',
    lens_model: '',
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

  it('shows the loading skeleton while the query change is in flight', async () => {
    fetchMock.mockResolvedValue(page([photo('a')], 1, null))

    const { result, rerender } = renderHook((props: PhotoListParams) => usePhotoLibrary(props), {
      initialProps: { sort: 'newest' },
    })
    await waitFor(() => {
      expect(result.current.status).toBe('ready')
    })

    // A genuine query change must blank to the skeleton (status 'loading', the
    // previous query's photos cleared) — this is the behavior the reload fix must
    // NOT regress.
    fetchMock.mockResolvedValue(page([photo('z')], 1, null))
    rerender({ sort: 'oldest' })
    expect(result.current.status).toBe('loading')
    expect(result.current.photos).toEqual([])

    await waitFor(() => {
      expect(result.current.status).toBe('ready')
    })
  })

  describe('background reload (reloadKey)', () => {
    it('does not blank the grid to a skeleton on a reload-key bump', async () => {
      fetchMock.mockResolvedValue(page([photo('a'), photo('b')], 2, null))

      const { result, rerender } = renderHook(
        (props: { reloadKey: string }) =>
          usePhotoLibrary({ sort: 'newest' }, { reloadKey: props.reloadKey }),
        { initialProps: { reloadKey: '0' } },
      )
      await waitFor(() => {
        expect(result.current.status).toBe('ready')
      })
      expect(result.current.photos.map((p) => p.uid)).toEqual(['a', 'b'])

      // A batch archive removed 'a'; the reload should reflect it in the
      // background — without ever dropping to 'loading' or clearing the list.
      fetchMock.mockResolvedValue(page([photo('b')], 1, null))
      rerender({ reloadKey: '1' })

      // Synchronously after the bump the current photos stay mounted and the
      // status is still 'ready' — the grid is never swapped for the skeleton.
      expect(result.current.status).toBe('ready')
      expect(result.current.photos.map((p) => p.uid)).toEqual(['a', 'b'])
      expect(result.current.reloading).toBe(true)

      await waitFor(() => {
        expect(result.current.photos.map((p) => p.uid)).toEqual(['b'])
      })
      expect(result.current.status).toBe('ready')
      expect(result.current.reloading).toBe(false)
    })

    it('refetches the first page on a reload-key bump', async () => {
      fetchMock.mockResolvedValue(page([photo('a')], 1, null))

      const { result, rerender } = renderHook(
        (props: { reloadKey: string }) =>
          usePhotoLibrary({ sort: 'newest' }, { reloadKey: props.reloadKey }),
        { initialProps: { reloadKey: '0' } },
      )
      await waitFor(() => {
        expect(result.current.status).toBe('ready')
      })
      expect(fetchMock).toHaveBeenCalledTimes(1)

      rerender({ reloadKey: '1' })
      await waitFor(() => {
        expect(fetchMock).toHaveBeenCalledTimes(2)
      })
      // The refetch is the first page, in the background — not a load-more.
      const lastCall = fetchMock.mock.calls[fetchMock.mock.calls.length - 1][0]
      expect(lastCall.offset).toBe(0)
    })

    it('reflects a batch favorite in place after a reload-key bump', async () => {
      fetchMock.mockResolvedValue(page([{ ...photo('a'), is_favorite: false }], 1, null))

      const { result, rerender } = renderHook(
        (props: { reloadKey: string }) =>
          usePhotoLibrary({ sort: 'newest' }, { reloadKey: props.reloadKey }),
        { initialProps: { reloadKey: '0' } },
      )
      await waitFor(() => {
        expect(result.current.status).toBe('ready')
      })
      expect(result.current.photos[0].is_favorite).toBe(false)

      fetchMock.mockResolvedValue(page([{ ...photo('a'), is_favorite: true }], 1, null))
      rerender({ reloadKey: '1' })
      // No skeleton flash while the favorite state refreshes.
      expect(result.current.status).toBe('ready')

      await waitFor(() => {
        expect(result.current.photos[0].is_favorite).toBe(true)
      })
      expect(result.current.status).toBe('ready')
    })

    it('keeps the current list when a background reload fails', async () => {
      fetchMock.mockResolvedValue(page([photo('a'), photo('b')], 2, null))

      const { result, rerender } = renderHook(
        (props: { reloadKey: string }) =>
          usePhotoLibrary({ sort: 'newest' }, { reloadKey: props.reloadKey }),
        { initialProps: { reloadKey: '0' } },
      )
      await waitFor(() => {
        expect(result.current.status).toBe('ready')
      })

      // A failed background refresh must not blank the grid to the error state:
      // the already-loaded photos stay visible.
      fetchMock.mockRejectedValueOnce(new Error('boom'))
      rerender({ reloadKey: '1' })

      await waitFor(() => {
        expect(result.current.reloading).toBe(false)
      })
      expect(result.current.status).toBe('ready')
      expect(result.current.photos.map((p) => p.uid)).toEqual(['a', 'b'])
    })
  })
})

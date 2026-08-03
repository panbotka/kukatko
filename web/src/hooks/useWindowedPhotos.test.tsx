import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { type Photo, type PhotoListParams, type PhotoListResponse } from '../services/photos'

import { PAGE_SIZE } from './usePaginatedPhotos'
import { WINDOW_MAX_ATTEMPTS, WINDOW_MAX_PAGES, useWindowedPhotos } from './useWindowedPhotos'

// Mock the data service: the hook is the unit under test, the network is not.
vi.mock('../services/photos', () => ({
  fetchPhotos: vi.fn(),
}))

const { fetchPhotos } = await import('../services/photos')
const fetchMock = vi.mocked(fetchPhotos)

/** The production library's order of magnitude: 20 889 photos, ~209 pages. */
const HUGE = 20889

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

/**
 * A page of a library of `total` photos: one photo per page, named after its own
 * offset, which is enough to tell which page a slot came from.
 */
function pageAt(total: number, offset: number): PhotoListResponse {
  return {
    photos: [photo(`p${String(offset)}`)],
    total,
    limit: PAGE_SIZE,
    offset,
    next_offset: offset + PAGE_SIZE < total ? offset + PAGE_SIZE : null,
  }
}

/** Answers every page request from a library of `total` photos. */
function servePagesOf(total: number): void {
  fetchMock.mockImplementation((params) => Promise.resolve(pageAt(total, params.offset ?? 0)))
}

/** The offsets asked for so far, in ascending order and without duplicates. */
function offsetsRequested(): number[] {
  return [...new Set(fetchMock.mock.calls.map((call) => call[0].offset ?? 0))].sort((a, b) => a - b)
}

const PARAMS: PhotoListParams = { sort: 'newest' }

beforeEach(() => {
  fetchMock.mockReset()
})

describe('useWindowedPhotos', () => {
  it('sizes the list to the whole result and fills only the first page', async () => {
    servePagesOf(HUGE)

    const { result } = renderHook(() => useWindowedPhotos(PARAMS))

    expect(result.current.status).toBe('loading')
    await waitFor(() => {
      expect(result.current.status).toBe('ready')
    })
    expect(result.current.total).toBe(HUGE)
    // Every photo has a slot from the first response on — that is what makes an
    // index an absolute position and a jump possible without loading the middle.
    expect(result.current.photos).toHaveLength(HUGE)
    expect(result.current.photos[0]?.uid).toBe('p0')
    expect(result.current.photos[HUGE - 1]).toBeUndefined()
  })

  it('costs the same to reach the oldest photo as the newest', async () => {
    servePagesOf(HUGE)
    const { result } = renderHook(() => useWindowedPhotos(PARAMS))
    await waitFor(() => {
      expect(result.current.status).toBe('ready')
    })

    act(() => {
      result.current.ensureRange(HUGE - 30, HUGE - 1)
    })
    await waitFor(() => {
      expect(result.current.photos[20800]?.uid).toBe('p20800')
    })

    // The page under the viewport plus the prefetch either side of it (the one
    // after it does not exist) — two requests to reach the end of a 209-page
    // library, whatever the reader was looking at before.
    expect(offsetsRequested()).toEqual([0, 20700, 20800])
  })

  it('never asks twice for a page it already holds', async () => {
    servePagesOf(HUGE)
    const { result } = renderHook(() => useWindowedPhotos(PARAMS))
    await waitFor(() => {
      expect(result.current.status).toBe('ready')
    })

    for (const start of [0, 10, 20, 30]) {
      act(() => {
        result.current.ensureRange(start, start + 30)
      })
    }
    await waitFor(() => {
      expect(result.current.photos[PAGE_SIZE]?.uid).toBe(`p${String(PAGE_SIZE)}`)
    })
    expect(offsetsRequested()).toEqual([0, PAGE_SIZE])
  })

  it('drops the pages the reader has travelled away from', async () => {
    servePagesOf(HUGE)
    const { result } = renderHook(() => useWindowedPhotos(PARAMS))
    await waitFor(() => {
      expect(result.current.status).toBe('ready')
    })

    // Walk far enough to load more pages than the window is allowed to keep.
    for (let page = 0; page < WINDOW_MAX_PAGES + 6; page++) {
      act(() => {
        result.current.ensureRange(page * PAGE_SIZE, page * PAGE_SIZE + 30)
      })
      await waitFor(() => {
        expect(result.current.photos[page * PAGE_SIZE]?.uid).toBe(`p${String(page * PAGE_SIZE)}`)
      })
    }

    const loaded = result.current.photos.filter((p) => p !== undefined).length
    expect(loaded).toBeLessThanOrEqual(WINDOW_MAX_PAGES)
    // What was dropped is the far end of the walk, not what is under the reader.
    expect(result.current.photos[0]).toBeUndefined()
  })

  it('abandons the pages a jump has already travelled past', async () => {
    const signals: (AbortSignal | undefined)[] = []
    fetchMock.mockImplementation((params, signal) => {
      signals.push(signal)
      return new Promise((resolve) => {
        setTimeout(() => {
          resolve(pageAt(HUGE, params.offset ?? 0))
        }, 20)
      })
    })
    const { result } = renderHook(() => useWindowedPhotos(PARAMS))
    await waitFor(() => {
      expect(result.current.status).toBe('ready')
    })

    // A jump sweeps the visible range through intermediate positions before it
    // settles; a response for a position nobody is looking at any more is
    // bandwidth taken from the one they are.
    act(() => {
      result.current.ensureRange(5000, 5030)
    })
    const midway = signals.length
    act(() => {
      result.current.ensureRange(HUGE - 30, HUGE - 1)
    })
    expect(signals.slice(0, midway).some((signal) => signal?.aborted === true)).toBe(true)
    await waitFor(() => {
      expect(result.current.photos[20800]?.uid).toBe('p20800')
    })
  })

  it('reports a failed first load as an error and reloads it on retry', async () => {
    fetchMock.mockRejectedValueOnce(new Error('offline'))
    const { result } = renderHook(() => useWindowedPhotos(PARAMS))

    await waitFor(() => {
      expect(result.current.status).toBe('error')
    })

    servePagesOf(250)
    act(() => {
      result.current.retry()
    })
    await waitFor(() => {
      expect(result.current.status).toBe('ready')
    })
    expect(result.current.total).toBe(250)
  })

  it('retries a failed page while scrolling, and gives up after a few tries', async () => {
    fetchMock.mockImplementation((params) => {
      const offset = params.offset ?? 0
      return offset === PAGE_SIZE
        ? Promise.reject(new Error('offline'))
        : Promise.resolve(pageAt(1000, offset))
    })
    const { result } = renderHook(() => useWindowedPhotos(PARAMS))
    await waitFor(() => {
      expect(result.current.status).toBe('ready')
    })

    // The first load already reaches for the second page (the prefetch) and it
    // fails; each further range change tries it again, until the cap.
    for (let attempt = 0; attempt < WINDOW_MAX_ATTEMPTS + 2; attempt++) {
      act(() => {
        result.current.ensureRange(0, 30)
      })
      await waitFor(() => {
        expect(result.current.status).toBe('ready')
      })
    }
    await waitFor(() => {
      expect(result.current.moreError).toBe(true)
    })
    const attempts = fetchMock.mock.calls.filter(
      (call) => (call[0].offset ?? 0) === PAGE_SIZE,
    ).length
    expect(attempts).toBe(WINDOW_MAX_ATTEMPTS)
    // The rest of the library is unaffected: a hole is one page, not the list.
    expect(result.current.photos[0]?.uid).toBe('p0')
  })

  it('refetches exactly the loaded pages when the reload key changes', async () => {
    servePagesOf(1000)
    const { result, rerender } = renderHook(
      ({ reloadKey }: { reloadKey: string }) => useWindowedPhotos(PARAMS, { reloadKey }),
      { initialProps: { reloadKey: '' } },
    )
    await waitFor(() => {
      expect(result.current.status).toBe('ready')
    })
    act(() => {
      result.current.ensureRange(500, 530)
    })
    await waitFor(() => {
      expect(result.current.photos[500]?.uid).toBe('p500')
    })

    const before = fetchMock.mock.calls.length
    rerender({ reloadKey: 'edited' })
    await waitFor(() => {
      expect(fetchMock.mock.calls.length).toBeGreaterThan(before)
    })
    // The photos stay on screen throughout: a bulk edit refreshes in place.
    expect(result.current.status).toBe('ready')
    expect(result.current.photos[500]?.uid).toBe('p500')
  })

  it('resets to the first page when the query changes', async () => {
    servePagesOf(1000)
    const { result, rerender } = renderHook(
      ({ params }: { params: PhotoListParams }) => useWindowedPhotos(params),
      { initialProps: { params: PARAMS } },
    )
    await waitFor(() => {
      expect(result.current.status).toBe('ready')
    })
    act(() => {
      result.current.ensureRange(500, 530)
    })
    await waitFor(() => {
      expect(result.current.photos[500]?.uid).toBe('p500')
    })

    servePagesOf(7)
    rerender({ params: { sort: 'newest', camera: 'Canon' } })
    await waitFor(() => {
      expect(result.current.total).toBe(7)
    })
    expect(result.current.photos).toHaveLength(7)
    expect(result.current.photos[0]?.uid).toBe('p0')
  })

  it('does not reset for a fresh params object holding the same query', async () => {
    servePagesOf(1000)
    const { result, rerender } = renderHook(
      ({ params }: { params: PhotoListParams }) => useWindowedPhotos(params),
      { initialProps: { params: { sort: 'newest' } } },
    )
    await waitFor(() => {
      expect(result.current.status).toBe('ready')
    })
    const before = fetchMock.mock.calls.length

    rerender({ params: { sort: 'newest' } })
    expect(fetchMock.mock.calls.length).toBe(before)
  })
})

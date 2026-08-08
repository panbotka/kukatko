import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { type PhotoListParams, type YearsResponse } from '../services/photos'

import { useLibraryFacets } from './useLibraryFacets'

// Only the network is faked; the hook's request bookkeeping is what is tested.
vi.mock('../services/photos', () => ({ fetchPhotoYears: vi.fn() }))
vi.mock('../services/organize', () => ({ fetchAlbums: vi.fn(), fetchLabels: vi.fn() }))
vi.mock('../services/people', () => ({ fetchSubjects: vi.fn() }))

const { fetchPhotoYears } = await import('../services/photos')
const { fetchAlbums, fetchLabels } = await import('../services/organize')
const { fetchSubjects } = await import('../services/people')

const yearsMock = vi.mocked(fetchPhotoYears)
const albumsMock = vi.mocked(fetchAlbums)
const labelsMock = vi.mocked(fetchLabels)
const subjectsMock = vi.mocked(fetchSubjects)

/** A years response holding the single given year. */
function years(year: number, count: number): YearsResponse {
  return { years: [{ year, count }], total: count }
}

/** Mounts the hook over a rerenderable `params` prop. */
function render(initial: PhotoListParams) {
  return renderHook((props: { params: PhotoListParams }) => useLibraryFacets(props.params), {
    initialProps: { params: initial },
  })
}

/** Lets every pending microtask and settled promise flush. */
async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

beforeEach(() => {
  yearsMock.mockReset()
  albumsMock.mockReset()
  labelsMock.mockReset()
  subjectsMock.mockReset()
  albumsMock.mockResolvedValue([])
  labelsMock.mockResolvedValue([])
  subjectsMock.mockResolvedValue([])
})

describe('useLibraryFacets', () => {
  it('loads the year counts for the current filters', async () => {
    yearsMock.mockResolvedValue(years(2026, 7))

    const { result } = render({ sort: 'newest', taken_after: '1960-01-01' })

    await waitFor(() => {
      expect(result.current.years).toEqual([{ year: 2026, count: 7 }])
    })
    // The period itself is stripped: a facet must not narrow its own options —
    // with the sixties picked, every other decade would read zero.
    expect(yearsMock.mock.calls[0][0].taken_after).toBe('')
    expect(yearsMock.mock.calls[0][0].taken_before).toBe('')
  })

  it('empties the year facet when its request fails', async () => {
    yearsMock.mockRejectedValue(new Error('boom'))

    const { result } = render({ sort: 'newest' })

    await flush()
    expect(result.current.years).toEqual([])
  })

  it('drops a stale year response that lands after a newer one', async () => {
    let settleFirst: (res: YearsResponse) => void = () => undefined
    yearsMock.mockImplementationOnce(
      () =>
        new Promise<YearsResponse>((resolve) => {
          settleFirst = resolve
        }),
    )
    yearsMock.mockResolvedValue(years(2026, 7))

    const { result, rerender } = render({ sort: 'newest' })
    rerender({ params: { sort: 'newest', label: 'lb_a' } })

    await waitFor(() => {
      expect(result.current.years).toEqual([{ year: 2026, count: 7 }])
    })

    // The first request answers only now. Aborting it was a no-op — the response
    // was already on the wire — so only the request id keeps its counts, which
    // belong to a filter the reader has left, out of the dropdown.
    await act(async () => {
      settleFirst(years(1999, 1))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(result.current.years).toEqual([{ year: 2026, count: 7 }])
  })

  it('does not let a stale failure empty the newer year counts', async () => {
    let failFirst: (err: Error) => void = () => undefined
    yearsMock.mockImplementationOnce(
      () =>
        new Promise<YearsResponse>((_resolve, reject) => {
          failFirst = reject
        }),
    )
    yearsMock.mockResolvedValue(years(2026, 7))

    const { result, rerender } = render({ sort: 'newest' })
    rerender({ params: { sort: 'newest', label: 'lb_a' } })

    await waitFor(() => {
      expect(result.current.years).toEqual([{ year: 2026, count: 7 }])
    })

    await act(async () => {
      failFirst(new Error('boom'))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(result.current.years).toEqual([{ year: 2026, count: 7 }])
  })
})

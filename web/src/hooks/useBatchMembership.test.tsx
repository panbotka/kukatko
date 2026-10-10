import { renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { type BulkMembershipSummary } from '../services/bulk'

import { MEMBERSHIP_MAX_UIDS, useBatchMembership } from './useBatchMembership'

// Only the network is faked; the hook's request bookkeeping is what is tested.
vi.mock('../services/bulk', () => ({ fetchBulkMembershipSummary: vi.fn() }))

const { fetchBulkMembershipSummary } = await import('../services/bulk')

const summaryMock = vi.mocked(fetchBulkMembershipSummary)

/** A membership answer with `filed` of `total` photos in the album "Panorama". */
function summary(total: number, filed: number): BulkMembershipSummary {
  return {
    total,
    filed,
    albums: filed > 0 ? [{ uid: 'al1', title: 'Panorama', photo_count: filed }] : [],
    labels: [],
  }
}

/** Mounts the hook over rerenderable `uids`/`enabled` props. */
function render(uids: string[], enabled: boolean) {
  return renderHook(
    (props: { uids: string[]; enabled: boolean }) => useBatchMembership(props.uids, props.enabled),
    { initialProps: { uids, enabled } },
  )
}

beforeEach(() => {
  summaryMock.mockReset()
})

describe('useBatchMembership', () => {
  it('asks once about the whole batch and reports the answer', async () => {
    summaryMock.mockResolvedValue(summary(2, 2))
    const { result } = render(['ph1', 'ph2'], true)

    await waitFor(() => {
      expect(result.current).toEqual({ status: 'ready', summary: summary(2, 2), partial: false })
    })
    expect(summaryMock).toHaveBeenCalledTimes(1)
    expect(summaryMock).toHaveBeenCalledWith(['ph1', 'ph2'], expect.any(AbortSignal))
  })

  it('asks nothing while disabled or over an empty batch', () => {
    const { result, rerender } = render(['ph1'], false)
    expect(result.current).toEqual({ status: 'idle' })
    rerender({ uids: [], enabled: true })
    expect(result.current).toEqual({ status: 'idle' })
    expect(summaryMock).not.toHaveBeenCalled()
  })

  it('does not ask again for a fresh array holding the same photos', async () => {
    summaryMock.mockResolvedValue(summary(1, 0))
    const { result, rerender } = render(['ph1'], true)
    await waitFor(() => {
      expect(result.current.status).toBe('ready')
    })
    rerender({ uids: ['ph1'], enabled: true })
    expect(summaryMock).toHaveBeenCalledTimes(1)
  })

  it('asks afresh when enabled again, so an album filed in between is seen', async () => {
    summaryMock.mockResolvedValueOnce(summary(1, 0)).mockResolvedValueOnce(summary(1, 1))
    const { result, rerender } = render(['ph1'], true)
    await waitFor(() => {
      expect(result.current).toEqual({ status: 'ready', summary: summary(1, 0), partial: false })
    })

    rerender({ uids: ['ph1'], enabled: false })
    expect(result.current).toEqual({ status: 'idle' })
    rerender({ uids: ['ph1'], enabled: true })

    await waitFor(() => {
      expect(result.current).toEqual({ status: 'ready', summary: summary(1, 1), partial: false })
    })
    expect(summaryMock).toHaveBeenCalledTimes(2)
  })

  it('reports a failed question as an error', async () => {
    summaryMock.mockRejectedValue(new Error('boom'))
    const { result } = render(['ph1'], true)
    await waitFor(() => {
      expect(result.current).toEqual({ status: 'error' })
    })
  })

  it('asks about the head of an oversized batch and marks the answer partial', async () => {
    summaryMock.mockResolvedValue(summary(MEMBERSHIP_MAX_UIDS, 0))
    const uids = Array.from({ length: MEMBERSHIP_MAX_UIDS + 5 }, (_, i) => `ph${i}`)
    const { result } = render(uids, true)

    await waitFor(() => {
      expect(result.current).toEqual({
        status: 'ready',
        summary: summary(MEMBERSHIP_MAX_UIDS, 0),
        partial: true,
      })
    })
    expect(summaryMock).toHaveBeenCalledWith(
      uids.slice(0, MEMBERSHIP_MAX_UIDS),
      expect.any(AbortSignal),
    )
  })
})

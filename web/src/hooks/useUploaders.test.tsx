import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { type PhotoListParams, type UploadersResponse } from '../services/photos'

import { useUploaders } from './useUploaders'

// Only the network is faked; the hook's request bookkeeping is what is tested.
vi.mock('../services/photos', () => ({ fetchPhotoUploaders: vi.fn() }))

const { fetchPhotoUploaders } = await import('../services/photos')

const uploadersMock = vi.mocked(fetchPhotoUploaders)

/** An uploader facet holding the single given contributor. */
function uploaders(uid: string, name: string, count: number): UploadersResponse {
  return { uploaders: [{ uid, name, count }] }
}

/** Mounts the hook over a rerenderable `params` prop. */
function render(initial: PhotoListParams) {
  return renderHook((props: { params: PhotoListParams }) => useUploaders(props.params), {
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
  uploadersMock.mockReset()
})

describe('useUploaders', () => {
  it('loads the contributors to the current view', async () => {
    uploadersMock.mockResolvedValue(uploaders('us_1', 'Tomáš', 7))

    const { result } = render({ sort: 'newest', album: 'al_1', uploader: 'us_2' })

    await waitFor(() => {
      expect(result.current).toEqual([{ uid: 'us_1', name: 'Tomáš', count: 7 }])
    })
    // The album scope rides along — that is the whole point of the facet — while
    // the uploader scope is stripped: a facet must not narrow its own options,
    // or the chosen contributor would be the only one left on offer.
    expect(uploadersMock.mock.calls[0][0].album).toBe('al_1')
    expect(uploadersMock.mock.calls[0][0].uploader).toBe('')
  })

  it('offers the same contributors whichever one is currently picked', async () => {
    uploadersMock.mockResolvedValue(uploaders('us_1', 'Tomáš', 7))

    const { result, rerender } = render({ sort: 'newest', uploader: '' })
    await flush()
    rerender({ params: { sort: 'newest', uploader: 'us_1' } })
    await flush()

    // Every request asks the same question, so picking a contributor cannot
    // hollow out the list they were picked from.
    for (const call of uploadersMock.mock.calls) {
      expect(call[0].uploader).toBe('')
    }
    expect(result.current).toEqual([{ uid: 'us_1', name: 'Tomáš', count: 7 }])
  })

  it('empties the facet when its request fails', async () => {
    uploadersMock.mockRejectedValue(new Error('boom'))

    const { result } = render({ sort: 'newest' })

    await flush()
    expect(result.current).toEqual([])
  })

  it('drops a stale response that lands after a newer one', async () => {
    let settleFirst: (res: UploadersResponse) => void = () => undefined
    uploadersMock.mockImplementationOnce(
      () =>
        new Promise<UploadersResponse>((resolve) => {
          settleFirst = resolve
        }),
    )
    uploadersMock.mockResolvedValue(uploaders('us_1', 'Tomáš', 7))

    const { result, rerender } = render({ sort: 'newest' })
    rerender({ params: { sort: 'newest', label: 'lb_a' } })

    await waitFor(() => {
      expect(result.current).toEqual([{ uid: 'us_1', name: 'Tomáš', count: 7 }])
    })

    // The first request answers only now. Aborting it was a no-op — the response
    // was already on the wire — so only the request id keeps counts belonging to
    // a view the reader has left out of the control.
    await act(async () => {
      settleFirst(uploaders('us_9', 'Stale', 1))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(result.current).toEqual([{ uid: 'us_1', name: 'Tomáš', count: 7 }])
  })
})

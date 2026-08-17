import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { type ShareManifestFile } from '../lib/photoShare'
import { ApiError } from '../services/auth'
import * as shareService from '../services/share'

import { usePhotoShare } from './usePhotoShare'

vi.mock('../services/share', () => ({
  fetchShareManifest: vi.fn(),
  fetchShareFile: vi.fn(),
}))

const fetchShareManifest = vi.mocked(shareService.fetchShareManifest)
const fetchShareFile = vi.mocked(shareService.fetchShareFile)

/** A manifest entry named after its index. */
function entry(index: number, size = 1024): ShareManifestFile {
  return { uid: `p${index}`, name: `p${index}.jpg`, mime: 'image/jpeg', size, preview: false }
}

/** The share sheet: records what it was handed, resolves by default. */
let share: ReturnType<typeof vi.fn>

/**
 * Makes this browser one that can share files, with a recording `navigator.share`.
 * jsdom has neither, so every test that exercises the sequence installs them.
 */
function stubShareSheet() {
  share = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'share', { value: share, configurable: true })
  Object.defineProperty(navigator, 'canShare', { value: () => true, configurable: true })
}

/** Removes the Web Share API again, as a desktop Firefox would have it. */
function stubNoShareSheet() {
  Reflect.deleteProperty(navigator, 'share')
  Reflect.deleteProperty(navigator, 'canShare')
}

beforeEach(() => {
  stubShareSheet()
  fetchShareManifest.mockResolvedValue([entry(1)])
  fetchShareFile.mockImplementation((file) =>
    Promise.resolve(new File([new Uint8Array([1, 2, 3])], file.name, { type: file.mime })),
  )
})

afterEach(() => {
  Reflect.deleteProperty(navigator, 'share')
  Reflect.deleteProperty(navigator, 'canShare')
})

describe('usePhotoShare', () => {
  it('reports the browser cannot share files, and never asks the backend', async () => {
    stubNoShareSheet()
    const { result } = renderHook(() => usePhotoShare(['p1']))

    expect(result.current.supported).toBe(false)

    act(() => {
      result.current.share()
    })
    await waitFor(() => {
      expect(result.current.status.kind).toBe('idle')
    })
    // A control that is never rendered cannot start a share, but even if it were,
    // nothing would be fetched.
    expect(fetchShareManifest).not.toHaveBeenCalled()
  })

  it('hands one batch to the share sheet as real Files with their own names', async () => {
    fetchShareManifest.mockResolvedValue([entry(1), entry(2)])
    const { result } = renderHook(() => usePhotoShare(['p1', 'p2']))

    act(() => {
      result.current.share()
    })
    await waitFor(() => {
      expect(share).toHaveBeenCalledTimes(1)
    })

    expect(fetchShareManifest).toHaveBeenCalledWith(['p1', 'p2'], expect.anything())
    const files = (share.mock.calls[0][0] as { files: File[] }).files
    expect(files.map((f) => f.name)).toEqual(['p1.jpg', 'p2.jpg'])
    expect(files.every((f) => f instanceof File)).toBe(true)
    expect(files[0].type).toBe('image/jpeg')
    await waitFor(() => {
      expect(result.current.status.kind).toBe('idle')
    })
    expect(result.current.error).toBeNull()
  })

  it('splits a large selection and waits for a tap between batches', async () => {
    // 25 files → two batches (20 + 5), because a share sheet cannot take them all.
    fetchShareManifest.mockResolvedValue(Array.from({ length: 25 }, (_v, i) => entry(i)))
    const { result } = renderHook(() => usePhotoShare(['p1']))

    act(() => {
      result.current.share()
    })
    await waitFor(() => {
      expect(result.current.status).toEqual({ kind: 'waiting', batch: 2, batches: 2 })
    })

    // The first handoff carried exactly one batch, not the whole selection, and the
    // second batch is not chained: it is waiting for its own gesture.
    expect(share).toHaveBeenCalledTimes(1)
    expect((share.mock.calls[0][0] as { files: File[] }).files).toHaveLength(20)
    // Only the first batch's files were ever fetched — the rest is not in memory.
    expect(fetchShareFile).toHaveBeenCalledTimes(20)

    act(() => {
      result.current.share()
    })
    await waitFor(() => {
      expect(share).toHaveBeenCalledTimes(2)
    })
    expect((share.mock.calls[1][0] as { files: File[] }).files).toHaveLength(5)
    // The manifest is asked once for the whole sequence, not once per batch.
    expect(fetchShareManifest).toHaveBeenCalledTimes(1)
    await waitFor(() => {
      expect(result.current.status.kind).toBe('idle')
    })
  })

  it('reports progress while a batch is being fetched', async () => {
    fetchShareManifest.mockResolvedValue([entry(1), entry(2), entry(3)])
    // Hold the second file so the intermediate progress is observable.
    let release: (file: File) => void = () => undefined
    fetchShareFile.mockImplementationOnce((file) =>
      Promise.resolve(new File([new Uint8Array([1])], file.name, { type: file.mime })),
    )
    fetchShareFile.mockImplementationOnce(
      () =>
        new Promise<File>((resolve) => {
          release = resolve
        }),
    )
    const { result } = renderHook(() => usePhotoShare(['p1', 'p2', 'p3']))

    act(() => {
      result.current.share()
    })
    await waitFor(() => {
      expect(result.current.status).toEqual({
        kind: 'fetching',
        batch: 1,
        batches: 1,
        done: 1,
        total: 3,
      })
    })
    expect(result.current.busy).toBe(true)

    act(() => {
      release(new File([new Uint8Array([2])], 'p2.jpg', { type: 'image/jpeg' }))
    })
    await waitFor(() => {
      expect(share).toHaveBeenCalledTimes(1)
    })
  })

  it('stops quietly when the reader closes the share sheet', async () => {
    fetchShareManifest.mockResolvedValue(Array.from({ length: 25 }, (_v, i) => entry(i)))
    share.mockRejectedValueOnce(new DOMException('cancelled', 'AbortError'))
    const { result } = renderHook(() => usePhotoShare(['p1']))

    act(() => {
      result.current.share()
    })
    await waitFor(() => {
      expect(result.current.status.kind).toBe('idle')
    })

    // Cancelling is a decision, not a failure: no message, and the sequence is over
    // rather than paused at batch 2.
    expect(result.current.error).toBeNull()
    expect(share).toHaveBeenCalledTimes(1)
  })

  it('names the photo it could not fetch and shares the rest of the batch', async () => {
    fetchShareManifest.mockResolvedValue([entry(1), entry(2)])
    fetchShareFile.mockRejectedValueOnce(new ApiError(404, 'original file not found'))
    const { result } = renderHook(() => usePhotoShare(['p1', 'p2']))

    act(() => {
      result.current.share()
    })
    await waitFor(() => {
      expect(share).toHaveBeenCalledTimes(1)
    })

    expect(result.current.error).toEqual({ kind: 'fetch', name: 'p1.jpg', count: 1 })
    const files = (share.mock.calls[0][0] as { files: File[] }).files
    expect(files.map((f) => f.name)).toEqual(['p2.jpg'])
  })

  it('keeps the remaining batches shareable when a whole batch fails to fetch', async () => {
    fetchShareManifest.mockResolvedValue(Array.from({ length: 25 }, (_v, i) => entry(i)))
    // Every file of the first batch fails; the second batch downloads normally.
    for (let i = 0; i < 20; i++) {
      fetchShareFile.mockRejectedValueOnce(new ApiError(500, 'boom'))
    }
    const { result } = renderHook(() => usePhotoShare(['p1']))

    act(() => {
      result.current.share()
    })
    await waitFor(() => {
      expect(result.current.status).toEqual({ kind: 'waiting', batch: 2, batches: 2 })
    })

    expect(share).not.toHaveBeenCalled()
    expect(result.current.error).toEqual({ kind: 'fetch', name: 'p0.jpg', count: 20 })

    act(() => {
      result.current.share()
    })
    await waitFor(() => {
      expect(share).toHaveBeenCalledTimes(1)
    })
    expect((share.mock.calls[0][0] as { files: File[] }).files).toHaveLength(5)
  })

  it('says a selection is over the cap rather than sharing part of it', async () => {
    fetchShareManifest.mockRejectedValue(new ApiError(413, 'too many photos'))
    const { result } = renderHook(() => usePhotoShare(['p1']))

    act(() => {
      result.current.share()
    })
    await waitFor(() => {
      expect(result.current.error).toEqual({ kind: 'tooMany' })
    })

    expect(share).not.toHaveBeenCalled()
    expect(result.current.status.kind).toBe('idle')
  })

  it('reports a backend that will not describe the selection', async () => {
    fetchShareManifest.mockRejectedValue(new ApiError(500, 'nope'))
    const { result } = renderHook(() => usePhotoShare(['p1']))

    act(() => {
      result.current.share()
    })
    await waitFor(() => {
      expect(result.current.error).toEqual({ kind: 'manifest' })
    })
    expect(result.current.busy).toBe(false)
  })

  it('stays on the same batch when the sheet itself refuses, so a tap retries it', async () => {
    fetchShareManifest.mockResolvedValue([entry(1)])
    share.mockRejectedValueOnce(new DOMException('not allowed', 'NotAllowedError'))
    const { result } = renderHook(() => usePhotoShare(['p1']))

    act(() => {
      result.current.share()
    })
    await waitFor(() => {
      expect(result.current.error).toEqual({ kind: 'sheet' })
    })
    expect(result.current.status).toEqual({ kind: 'waiting', batch: 1, batches: 1 })

    act(() => {
      result.current.share()
    })
    await waitFor(() => {
      expect(share).toHaveBeenCalledTimes(2)
    })
    expect(result.current.error).toBeNull()
  })

  it('ignores a second tap while a batch is being prepared', async () => {
    let release: (files: ShareManifestFile[]) => void = () => undefined
    fetchShareManifest.mockImplementation(
      () =>
        new Promise<ShareManifestFile[]>((resolve) => {
          release = resolve
        }),
    )
    const { result } = renderHook(() => usePhotoShare(['p1']))

    act(() => {
      result.current.share()
    })
    await waitFor(() => {
      expect(result.current.status.kind).toBe('manifest')
    })
    act(() => {
      result.current.share()
    })
    expect(fetchShareManifest).toHaveBeenCalledTimes(1)

    act(() => {
      release([entry(1)])
    })
    await waitFor(() => {
      expect(share).toHaveBeenCalledTimes(1)
    })
  })

  it('drops a stale message when the selection changes under it', async () => {
    fetchShareManifest.mockRejectedValue(new ApiError(500, 'nope'))
    const { result, rerender } = renderHook(({ uids }) => usePhotoShare(uids), {
      initialProps: { uids: ['p1'] },
    })

    act(() => {
      result.current.share()
    })
    await waitFor(() => {
      expect(result.current.error).toEqual({ kind: 'manifest' })
    })

    rerender({ uids: ['p1', 'p2'] })
    await waitFor(() => {
      expect(result.current.error).toBeNull()
    })
  })

  it('does nothing when the selection resolves to no files at all', async () => {
    fetchShareManifest.mockResolvedValue([])
    const { result } = renderHook(() => usePhotoShare(['p1']))

    act(() => {
      result.current.share()
    })
    await waitFor(() => {
      expect(fetchShareManifest).toHaveBeenCalled()
    })
    expect(share).not.toHaveBeenCalled()
    expect(result.current.status.kind).toBe('idle')
    expect(result.current.error).toBeNull()
  })
})

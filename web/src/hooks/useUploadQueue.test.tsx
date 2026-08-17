import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { type UploadFileOptions, type UploadFileResult } from '../services/upload'

import { MAX_CONCURRENT_UPLOADS, useUploadQueue } from './useUploadQueue'

// Mock the upload service: the queue is the unit under test, not the network.
vi.mock('../services/upload', () => ({
  uploadFile: vi.fn(),
  isAbortError: (error: unknown): boolean =>
    error instanceof DOMException && error.name === 'AbortError',
}))

const { uploadFile } = await import('../services/upload')
const uploadMock = vi.mocked(uploadFile)

/** One captured in-flight upload, resolvable/rejectable from the test. */
interface Pending {
  options: UploadFileOptions
  resolve: (result: UploadFileResult) => void
  reject: (error: unknown) => void
}

let pending: Pending[] = []

// A fixed lastModified makes the dedup key (name + size + mtime) deterministic
// across separate File instances with the same name.
function file(name: string): File {
  return new File(['data'], name, { type: 'image/jpeg', lastModified: 0 })
}

function result(outcome: UploadFileResult['outcome'], uid?: string): UploadFileResult {
  return { filename: 'x', status: 201, outcome, photo_uid: uid }
}

/** Resolves the n-th captured upload and flushes the resulting state updates. */
async function settle(index: number, value: UploadFileResult): Promise<void> {
  await act(async () => {
    pending[index].resolve(value)
    await Promise.resolve()
  })
}

beforeEach(() => {
  pending = []
  uploadMock.mockReset()
  uploadMock.mockImplementation(
    (_file: File, options: UploadFileOptions = {}) =>
      new Promise<UploadFileResult>((resolve, reject) => {
        pending.push({ options, resolve, reject })
      }),
  )
})

describe('useUploadQueue', () => {
  it('adds files as items and skips duplicates', () => {
    const { result: hook } = renderHook(() => useUploadQueue())

    act(() => {
      hook.current.addFiles([file('a.jpg'), file('b.jpg')])
    })
    expect(hook.current.items).toHaveLength(2)

    // Re-adding the same files (same name/size/mtime) is a no-op.
    act(() => {
      hook.current.addFiles([file('a.jpg')])
    })
    expect(hook.current.items).toHaveLength(2)
  })

  it('removes a file from the queue, aborting it if it is in flight', () => {
    const { result: hook } = renderHook(() => useUploadQueue())
    act(() => {
      hook.current.addFiles([file('a.jpg'), file('b.jpg')])
    })
    const id = hook.current.items[0].id
    act(() => {
      hook.current.removeItem(id)
    })
    expect(hook.current.items.map((i) => i.file.name)).toEqual(['b.jpg'])
  })

  it('starts uploading the moment files are added — there is no start step', () => {
    const { result: hook } = renderHook(() => useUploadQueue())
    act(() => {
      hook.current.addFiles([file('a.jpg')])
    })
    expect(uploadMock).toHaveBeenCalledTimes(1)
    expect(hook.current.items[0].status).toBe('uploading')
  })

  it('appends files to a running batch instead of restarting it', async () => {
    const { result: hook } = renderHook(() => useUploadQueue())
    act(() => {
      hook.current.addFiles([file('a.jpg'), file('b.jpg'), file('c.jpg')])
    })
    expect(uploadMock).toHaveBeenCalledTimes(MAX_CONCURRENT_UPLOADS)

    // Two more while the first three are in flight: nothing is aborted, they
    // simply queue up behind the cap.
    act(() => {
      hook.current.addFiles([file('d.jpg'), file('e.jpg')])
    })
    expect(uploadMock).toHaveBeenCalledTimes(MAX_CONCURRENT_UPLOADS)
    expect(hook.current.summary).toMatchObject({
      total: 5,
      uploading: MAX_CONCURRENT_UPLOADS,
      queued: 5 - MAX_CONCURRENT_UPLOADS,
    })

    // And a freed slot goes to a newcomer, not to a restarted file.
    await settle(0, result('created', 'ph1'))
    await waitFor(() => {
      expect(uploadMock).toHaveBeenCalledTimes(MAX_CONCURRENT_UPLOADS + 1)
    })
    expect(hook.current.items.map((i) => i.file.name)).toEqual([
      'a.jpg',
      'b.jpg',
      'c.jpg',
      'd.jpg',
      'e.jpg',
    ])
  })

  it('caps concurrent uploads and starts the next as each finishes', async () => {
    const { result: hook } = renderHook(() => useUploadQueue())
    act(() => {
      hook.current.addFiles([
        file('a.jpg'),
        file('b.jpg'),
        file('c.jpg'),
        file('d.jpg'),
        file('e.jpg'),
      ])
    })

    // Only the cap runs at once; the rest stay queued.
    expect(uploadMock).toHaveBeenCalledTimes(MAX_CONCURRENT_UPLOADS)
    expect(hook.current.summary.uploading).toBe(MAX_CONCURRENT_UPLOADS)
    expect(hook.current.summary.queued).toBe(5 - MAX_CONCURRENT_UPLOADS)

    // Finishing one frees a slot for the next queued file.
    await settle(0, result('created', 'ph1'))
    await waitFor(() => {
      expect(uploadMock).toHaveBeenCalledTimes(MAX_CONCURRENT_UPLOADS + 1)
    })
  })

  it('renders per-file progress from the upload callback', () => {
    const { result: hook } = renderHook(() => useUploadQueue())
    act(() => {
      hook.current.addFiles([file('a.jpg')])
    })
    act(() => {
      pending[0].options.onProgress?.(0.42)
    })
    expect(hook.current.items[0].progress).toBeCloseTo(0.42)
    expect(hook.current.items[0].status).toBe('uploading')
  })

  it('maps created / duplicate / error outcomes to statuses and counts', async () => {
    const { result: hook } = renderHook(() => useUploadQueue())
    act(() => {
      hook.current.addFiles([file('a.jpg'), file('b.jpg'), file('c.jpg')])
    })
    await settle(0, result('created', 'ph1'))
    await settle(1, result('duplicate', 'ph2'))
    await settle(2, { filename: 'c', status: 500, outcome: 'error', error: 'boom' })

    await waitFor(() => {
      expect(hook.current.isComplete).toBe(true)
    })
    expect(hook.current.summary).toMatchObject({ created: 1, duplicate: 1, error: 1 })
    expect(hook.current.createdUids).toEqual(['ph1'])
    // Assignment targets created *and* duplicate photos, but never failures.
    expect(hook.current.resolvedUids).toEqual(['ph1', 'ph2'])
    expect(hook.current.items[2].error).toBe('boom')
  })

  it('reports overall progress, weighting in-flight files by their partial fraction', async () => {
    const { result: hook } = renderHook(() => useUploadQueue())
    act(() => {
      hook.current.addFiles([file('a.jpg'), file('b.jpg')])
    })
    // Both uploading at 0% — nothing completed yet.
    expect(hook.current.progress).toBe(0)

    // One file half-sent contributes its fraction: (0.5 + 0) / 2 = 0.25.
    act(() => {
      pending[0].options.onProgress?.(0.5)
    })
    expect(hook.current.progress).toBeCloseTo(0.25)

    // Settling it counts the file as fully done: (1 + 0) / 2 = 0.5.
    await settle(0, result('created', 'ph1'))
    expect(hook.current.progress).toBeCloseTo(0.5)

    // A failed file still counts as done for the overall bar: (1 + 1) / 2 = 1.
    await settle(1, { filename: 'b', status: 500, outcome: 'error', error: 'boom' })
    await waitFor(() => {
      expect(hook.current.isComplete).toBe(true)
    })
    expect(hook.current.progress).toBe(1)
  })

  it('retries a failed file', async () => {
    const { result: hook } = renderHook(() => useUploadQueue())
    act(() => {
      hook.current.addFiles([file('a.jpg')])
    })
    await act(async () => {
      pending[0].reject(new Error('network'))
      await Promise.resolve()
    })
    await waitFor(() => {
      expect(hook.current.items[0].status).toBe('error')
    })

    const id = hook.current.items[0].id
    act(() => {
      hook.current.retry(id)
    })

    await waitFor(() => {
      expect(uploadMock).toHaveBeenCalledTimes(2)
    })
    expect(hook.current.items[0].status).toBe('uploading')
  })

  it('surfaces near-duplicate warnings without failing the file', async () => {
    const { result: hook } = renderHook(() => useUploadQueue())
    act(() => {
      hook.current.addFiles([file('a.jpg')])
    })
    await settle(0, {
      filename: 'a.jpg',
      status: 201,
      outcome: 'created',
      photo_uid: 'ph1',
      warnings: [{ code: 'near_duplicate', message: 'similar' }],
    })

    expect(hook.current.items[0].status).toBe('created')
    expect(hook.current.items[0].warnings).toEqual([{ code: 'near_duplicate', message: 'similar' }])
  })

  it('clears the queue', () => {
    const { result: hook } = renderHook(() => useUploadQueue())
    act(() => {
      hook.current.addFiles([file('a.jpg'), file('b.jpg')])
    })
    act(() => {
      hook.current.clear()
    })
    expect(hook.current.items).toHaveLength(0)
  })
})

import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

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

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useUploadQueue', () => {
  it('adds files as queued items and skips duplicates', () => {
    const { result: hook } = renderHook(() => useUploadQueue())

    act(() => {
      hook.current.addFiles([file('a.jpg'), file('b.jpg')])
    })
    expect(hook.current.items).toHaveLength(2)
    expect(hook.current.items.every((i) => i.status === 'queued')).toBe(true)

    // Re-adding the same files (same name/size/mtime) is a no-op.
    act(() => {
      hook.current.addFiles([file('a.jpg')])
    })
    expect(hook.current.items).toHaveLength(2)
  })

  it('removes a queued file before upload', () => {
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

  it('does not upload until start is called', () => {
    const { result: hook } = renderHook(() => useUploadQueue())
    act(() => {
      hook.current.addFiles([file('a.jpg')])
    })
    expect(uploadMock).not.toHaveBeenCalled()
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

    act(() => {
      hook.current.start()
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
      hook.current.start()
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
    act(() => {
      hook.current.start()
    })

    await settle(0, result('created', 'ph1'))
    await settle(1, result('duplicate', 'ph2'))
    await settle(2, { filename: 'c', status: 500, outcome: 'error', error: 'boom' })

    await waitFor(() => {
      expect(hook.current.isComplete).toBe(true)
    })
    expect(hook.current.summary).toMatchObject({ created: 1, duplicate: 1, error: 1 })
    expect(hook.current.createdUids).toEqual(['ph1'])
    expect(hook.current.items[2].error).toBe('boom')
  })

  it('retries a failed file', async () => {
    const { result: hook } = renderHook(() => useUploadQueue())
    act(() => {
      hook.current.addFiles([file('a.jpg')])
    })
    act(() => {
      hook.current.start()
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
    act(() => {
      hook.current.start()
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

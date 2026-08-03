import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { pendingValue } from '../lib/pendingCreate'
import { ApiError } from '../services/auth'
import { type BulkResult } from '../services/bulk'
import { type Album, type AlbumSummary, type Label, type LabelCount } from '../services/organize'

import { useUploadOrganize } from './useUploadOrganize'

vi.mock('../services/bulk', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/bulk')>()
  return { ...actual, bulkUpdatePhotos: vi.fn() }
})
vi.mock('../services/organize', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/organize')>()
  return {
    ...actual,
    fetchAlbums: vi.fn(),
    fetchLabels: vi.fn(),
    createAlbum: vi.fn(),
    createLabel: vi.fn(),
  }
})

const { bulkUpdatePhotos } = await import('../services/bulk')
const { fetchAlbums, fetchLabels, createAlbum, createLabel } = await import('../services/organize')
const bulkMock = vi.mocked(bulkUpdatePhotos)
const albumsMock = vi.mocked(fetchAlbums)
const labelsMock = vi.mocked(fetchLabels)
const createAlbumMock = vi.mocked(createAlbum)
const createLabelMock = vi.mocked(createLabel)

function albumSummary(uid: string, title: string): AlbumSummary {
  return {
    uid,
    slug: title.toLowerCase(),
    title,
    description: '',
    type: 'album',
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: 0,
  }
}

function labelCount(uid: string, name: string): LabelCount {
  return {
    uid,
    slug: name.toLowerCase(),
    name,
    priority: 0,
    review_enabled: true,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: 0,
  }
}

function album(uid: string, title: string): Album {
  return {
    uid,
    slug: title.toLowerCase(),
    title,
    description: '',
    type: 'album',
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

function label(uid: string, name: string): Label {
  return {
    uid,
    slug: name.toLowerCase(),
    name,
    priority: 0,
    review_enabled: true,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

function result(counts: Partial<BulkResult['counts']> = {}): BulkResult {
  return { results: [], counts: { total: 0, updated: 0, skipped: 0, errored: 0, ...counts } }
}

/** Renders the hook and waits for the album/label catalogs to load. */
async function renderReady() {
  const hook = renderHook(() => useUploadOrganize())
  await waitFor(() => {
    expect(hook.result.current.load.status).toBe('ready')
  })
  return hook
}

beforeEach(() => {
  bulkMock.mockReset()
  albumsMock.mockReset().mockResolvedValue([albumSummary('al1', 'Trip')])
  labelsMock.mockReset().mockResolvedValue([labelCount('lb1', 'Sunset')])
  createAlbumMock.mockReset()
  createLabelMock.mockReset()
})

describe('useUploadOrganize', () => {
  it('loads the album and label catalogs', async () => {
    const { result: hook } = await renderReady()
    expect(hook.current.load).toMatchObject({
      status: 'ready',
      albums: [expect.objectContaining({ uid: 'al1' })],
      labels: [expect.objectContaining({ uid: 'lb1' })],
    })
    expect(hook.current.hasSelection).toBe(false)
  })

  it('reports a load error without blocking (selection stays empty)', async () => {
    albumsMock.mockRejectedValue(new ApiError(500, 'boom'))
    const { result: hook } = renderHook(() => useUploadOrganize())
    await waitFor(() => {
      expect(hook.current.load.status).toBe('error')
    })
    expect(hook.current.hasSelection).toBe(false)
  })

  it('assigns the chosen album and label to every uid in one bulk call', async () => {
    bulkMock.mockResolvedValue(result({ total: 2, updated: 2 }))
    const { result: hook } = await renderReady()

    act(() => {
      hook.current.setAlbums(['al1'])
      hook.current.setLabels(['lb1'])
    })
    expect(hook.current.hasSelection).toBe(true)

    act(() => {
      hook.current.runAssign(['ph1', 'ph2'])
    })

    await waitFor(() => {
      expect(hook.current.assign.status).toBe('done')
    })
    expect(bulkMock).toHaveBeenCalledTimes(1)
    expect(bulkMock).toHaveBeenCalledWith(['ph1', 'ph2'], {
      add_to_albums: ['al1'],
      add_labels: ['lb1'],
    })
  })

  it('creates a pending album/label before assigning', async () => {
    createAlbumMock.mockResolvedValue(album('alNew', 'Holiday'))
    createLabelMock.mockResolvedValue(label('lbNew', 'Beach'))
    bulkMock.mockResolvedValue(result({ total: 1, updated: 1 }))
    const { result: hook } = await renderReady()

    act(() => {
      hook.current.setAlbums([pendingValue('Holiday')])
      hook.current.setLabels([pendingValue('Beach')])
    })
    act(() => {
      hook.current.runAssign(['ph1'])
    })

    await waitFor(() => {
      expect(hook.current.assign.status).toBe('done')
    })
    expect(createAlbumMock).toHaveBeenCalledWith({
      title: 'Holiday',
      description: '',
      private: false,
    })
    expect(createLabelMock).toHaveBeenCalledWith({ name: 'Beach', priority: 0 })
    expect(bulkMock).toHaveBeenCalledWith(['ph1'], {
      add_to_albums: ['alNew'],
      add_labels: ['lbNew'],
    })
  })

  it('makes no call when nothing is selected', async () => {
    const { result: hook } = await renderReady()
    act(() => {
      hook.current.runAssign(['ph1'])
    })
    // Nothing to do: the assignment never leaves idle and no request is made.
    await Promise.resolve()
    expect(hook.current.assign.status).toBe('idle')
    expect(bulkMock).not.toHaveBeenCalled()
  })

  it('surfaces a retryable error and re-sends on retry', async () => {
    bulkMock.mockRejectedValueOnce(new ApiError(413, 'too many photos'))
    const { result: hook } = await renderReady()

    act(() => {
      hook.current.setAlbums(['al1'])
    })
    act(() => {
      hook.current.runAssign(['ph1'])
    })
    await waitFor(() => {
      expect(hook.current.assign).toEqual({ status: 'error', message: 'too many photos' })
    })

    bulkMock.mockResolvedValueOnce(result({ total: 1, updated: 1 }))
    act(() => {
      hook.current.retryAssign()
    })
    await waitFor(() => {
      expect(hook.current.assign.status).toBe('done')
    })
    expect(bulkMock).toHaveBeenCalledTimes(2)
  })

  it('re-arms a finished assignment when the selection changes afterwards', async () => {
    bulkMock.mockResolvedValue(result({ total: 1, updated: 1 }))
    const { result: hook } = await renderReady()

    act(() => {
      hook.current.setAlbums(['al1'])
    })
    act(() => {
      hook.current.runAssign(['ph1'])
    })
    await waitFor(() => {
      expect(hook.current.assign.status).toBe('done')
    })

    // Picking another album once the batch is already assigned must not be
    // swallowed: the result goes back to idle so the caller assigns again.
    act(() => {
      hook.current.setAlbums(['al1', 'al2'])
    })
    expect(hook.current.assign.status).toBe('idle')

    act(() => {
      hook.current.runAssign(['ph1'])
    })
    await waitFor(() => {
      expect(hook.current.assign.status).toBe('done')
    })
    expect(bulkMock).toHaveBeenLastCalledWith(['ph1'], { add_to_albums: ['al1', 'al2'] })
  })

  it('re-arms a failed assignment when the selection changes afterwards', async () => {
    bulkMock.mockRejectedValueOnce(new ApiError(500, 'boom'))
    const { result: hook } = await renderReady()

    act(() => {
      hook.current.setAlbums(['al1'])
    })
    act(() => {
      hook.current.runAssign(['ph1'])
    })
    await waitFor(() => {
      expect(hook.current.assign.status).toBe('error')
    })

    act(() => {
      hook.current.setLabels(['lb1'])
    })
    expect(hook.current.assign.status).toBe('idle')
  })

  it('keeps a pending-create swap from re-arming the assignment it runs in', async () => {
    let release: (value: Album) => void = () => undefined
    createAlbumMock.mockImplementation(
      () =>
        new Promise<Album>((resolve) => {
          release = resolve
        }),
    )
    bulkMock.mockResolvedValue(result({ total: 1, updated: 1 }))
    const { result: hook } = await renderReady()

    act(() => {
      hook.current.setAlbums([pendingValue('Holiday')])
    })
    act(() => {
      hook.current.runAssign(['ph1'])
    })
    await waitFor(() => {
      expect(hook.current.assign.status).toBe('assigning')
    })

    // Creating the album rewrites the selection (marker → real UID) from inside
    // the run; that internal edit must not knock the state back to idle.
    await act(async () => {
      release(album('alNew', 'Holiday'))
      await Promise.resolve()
    })
    await waitFor(() => {
      expect(hook.current.assign.status).toBe('done')
    })
    expect(hook.current.albums).toEqual(['alNew'])
    expect(bulkMock).toHaveBeenCalledTimes(1)
  })

  it('resetAssign clears a finished result', async () => {
    bulkMock.mockResolvedValue(result({ total: 1, updated: 1 }))
    const { result: hook } = await renderReady()
    act(() => {
      hook.current.setAlbums(['al1'])
    })
    act(() => {
      hook.current.runAssign(['ph1'])
    })
    await waitFor(() => {
      expect(hook.current.assign.status).toBe('done')
    })
    act(() => {
      hook.current.resetAssign()
    })
    expect(hook.current.assign.status).toBe('idle')
  })
})

import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { type Bbox, type FaceView, type FacesResponse, type Suggestion } from '../services/people'
import { type Photo, type PhotoListResponse } from '../services/photos'

import { type AlbumFaceRow, useAlbumFaces, useAlbumFaceCount } from './useAlbumFaces'

// Only the two network calls the run makes are faked; the queue walk, the
// skipping, the dismissals and the same-subject rule all run for real.
vi.mock('../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/people')>()
  return { ...actual, fetchFaces: vi.fn(), assignFace: vi.fn() }
})
vi.mock('../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/photos')>()
  return { ...actual, fetchPhotos: vi.fn() }
})

const { fetchFaces, assignFace } = await import('../services/people')
const { fetchPhotos } = await import('../services/photos')
const facesMock = vi.mocked(fetchFaces)
const assignMock = vi.mocked(assignFace)
const photosMock = vi.mocked(fetchPhotos)

/** A catalogue row, only as far as the run reads it. */
function photo(uid: string): Photo {
  return {
    uid,
    file_hash: `h_${uid}`,
    file_name: `${uid}.jpg`,
    file_size: 1000,
    file_mime: 'image/jpeg',
    file_width: 1200,
    file_height: 800,
    taken_at_source: 'exif',
    media_type: 'image',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  } as Photo
}

/** A ranked candidate at the given confidence. */
function suggestion(name: string, confidence: number): Suggestion {
  return {
    subject_uid: `su_${name}`,
    subject_name: name,
    distance: 1 - confidence,
    confidence,
  }
}

/** An unnamed detection carrying the given suggestions. */
function face(faceIndex: number, suggestions: Suggestion[] = []): FaceView {
  return {
    face_index: faceIndex,
    // Boxes run down the photo, so reading order matches the array order.
    bbox: [0.1, 0.1 * (faceIndex + 1), 0.2, 0.2] as Bbox,
    det_score: 0.9,
    action: 'create_marker',
    suggestions,
  }
}

/** The faces of one photo. */
function facesOf(uid: string, faces: FaceView[]): FacesResponse {
  return { photo_uid: uid, width: 1200, height: 800, orientation: 1, faces }
}

/** Answers the queue search with one page holding exactly these photos. */
function queueOf(...uids: string[]): void {
  photosMock.mockImplementation((params) =>
    Promise.resolve({
      photos: (params.offset ?? 0) === 0 ? uids.map(photo) : [],
      total: uids.length,
      limit: params.limit ?? 200,
      offset: params.offset ?? 0,
      next_offset: null,
    } satisfies PhotoListResponse),
  )
}

/** Answers `GET /photos/{uid}/faces` from a table of photos. */
function facesFrom(table: Record<string, FaceView[]>): void {
  facesMock.mockImplementation((uid) =>
    // A photo missing from the table is one whose faces cannot be fetched.
    Object.hasOwn(table, uid)
      ? Promise.resolve(facesOf(uid, table[uid]))
      : Promise.reject(new Error(`no faces for ${uid}`)),
  )
}

/**
 * The identity a row's confirm button would apply. A row the test drives is
 * always one that offers somebody, so an empty one is a broken test, not a case.
 */
function offerOf(row: AlbumFaceRow): Suggestion {
  if (row.suggestion === null) {
    throw new Error(`row ${String(row.number)} offers nobody`)
  }
  return row.suggestion
}

/** Mounts the run and waits for it to settle on a photo (or on the end). */
async function renderRun() {
  const rendered = renderHook(() => useAlbumFaces('al1'))
  await waitFor(() => {
    expect(rendered.result.current.status).not.toBe('loading')
  })
  return rendered
}

beforeEach(() => {
  vi.clearAllMocks()
  assignMock.mockResolvedValue(undefined)
})

describe('useAlbumFaces', () => {
  it('builds the queue from the album and stops on its first photo', async () => {
    queueOf('p1', 'p2')
    facesFrom({
      p1: [face(0, [suggestion('Alice', 0.9)])],
      p2: [face(0, [suggestion('Bob', 0.8)])],
    })

    const { result } = await renderRun()

    expect(photosMock).toHaveBeenCalledWith(
      expect.objectContaining({ album: 'al1', q: 'face:new' }),
      expect.anything(),
    )
    expect(result.current.status).toBe('ready')
    expect(result.current.photo?.uid).toBe('p1')
    expect(result.current.total).toBe(2)
    expect(result.current.position).toBe(1)
    expect(result.current.rows.map((row) => row.suggestion?.subject_name)).toEqual(['Alice'])
  })

  it('skips a photo nobody can answer for without ever showing it', async () => {
    queueOf('p1', 'p2', 'p3')
    facesFrom({
      // Below the display floor: shown muted on the photo detail, never a button.
      p1: [face(0, [suggestion('Alice', 0.2)])],
      // A marker with no face row behind it: nameable only by hand.
      p2: [face(-1, [suggestion('Bob', 0.9)])],
      p3: [face(0, [suggestion('Cyril', 0.7)])],
    })

    const { result } = await renderRun()

    expect(result.current.photo?.uid).toBe('p3')
    expect(result.current.position).toBe(3)
  })

  it('ends the run when nothing in the album can be answered', async () => {
    queueOf('p1')
    facesFrom({ p1: [face(0, [suggestion('Alice', 0.2)])] })

    const { result } = await renderRun()

    expect(result.current.status).toBe('done')
    expect(result.current.photo).toBeNull()
  })

  it('confirms one face through the assign endpoint and leaves the rest', async () => {
    queueOf('p1')
    facesFrom({
      p1: [face(0, [suggestion('Alice', 0.9)]), face(1, [suggestion('Bob', 0.8)])],
    })

    const { result } = await renderRun()
    const [first] = result.current.rows
    await act(async () => {
      result.current.confirm(first.face, offerOf(first))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(assignMock).toHaveBeenCalledTimes(1)
    expect(assignMock).toHaveBeenCalledWith('p1', {
      action: 'create_marker',
      bbox: [0.1, 0.1, 0.2, 0.2],
      face_index: 0,
      subject_uid: 'su_Alice',
    })
    // The confirmed face is named and gone from the questions; the other stays.
    expect(result.current.rows.map((row) => row.face.face_index)).toEqual([1])
    expect(result.current.photo?.uid).toBe('p1')
  })

  it('names an existing marker in place rather than drawing a second one', async () => {
    queueOf('p1')
    facesFrom({
      p1: [{ ...face(0, [suggestion('Alice', 0.9)]), marker_uid: 'mk_1' }],
    })

    const { result } = await renderRun()
    const [row] = result.current.rows
    await act(async () => {
      result.current.confirm(row.face, offerOf(row))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(assignMock).toHaveBeenCalledWith('p1', {
      action: 'assign_person',
      marker_uid: 'mk_1',
      subject_uid: 'su_Alice',
    })
  })

  it('confirms every offer at once, one person only once', async () => {
    queueOf('p1', 'p2')
    facesFrom({
      p1: [
        face(0, [suggestion('Alice', 0.7)]),
        face(1, [suggestion('Alice', 0.95)]),
        face(2, [suggestion('Bob', 0.8)]),
      ],
      p2: [face(0, [suggestion('Cyril', 0.9)])],
    })

    const { result } = await renderRun()
    expect(result.current.batch.map((item) => item.face.face_index)).toEqual([1, 2])

    await act(async () => {
      result.current.confirmAll()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(assignMock).toHaveBeenCalledTimes(2)
    expect(assignMock.mock.calls.map((call) => call[1].subject_uid)).toEqual(['su_Alice', 'su_Bob'])
    // Face 0 still suggests Alice, who is now on the photo — it is left for a
    // human, and it is the only question standing, so the run stays put.
    expect(result.current.rows.map((row) => row.face.face_index)).toEqual([0])
    expect(result.current.photo?.uid).toBe('p1')
  })

  it('advances by itself once the photo has nothing left to ask', async () => {
    queueOf('p1', 'p2')
    facesFrom({
      p1: [face(0, [suggestion('Alice', 0.9)])],
      p2: [face(0, [suggestion('Bob', 0.8)])],
    })

    const { result } = await renderRun()
    const [row] = result.current.rows
    await act(async () => {
      result.current.confirm(row.face, offerOf(row))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    await waitFor(() => {
      expect(result.current.photo?.uid).toBe('p2')
    })
  })

  it('dismisses a row without writing anything, and moves on when it was the last', async () => {
    queueOf('p1', 'p2')
    facesFrom({
      p1: [face(0, [suggestion('Alice', 0.9)]), face(1, [suggestion('Bob', 0.8)])],
      p2: [face(0, [suggestion('Cyril', 0.8)])],
    })

    const { result } = await renderRun()
    act(() => {
      result.current.dismiss(0)
    })

    expect(assignMock).not.toHaveBeenCalled()
    expect(result.current.rows.map((row) => row.face.face_index)).toEqual([1])
    expect(result.current.photo?.uid).toBe('p1')

    act(() => {
      result.current.dismiss(1)
    })
    await waitFor(() => {
      expect(result.current.photo?.uid).toBe('p2')
    })
  })

  it('lists a face with no offered suggestion but offers nothing to press', async () => {
    queueOf('p1')
    facesFrom({
      p1: [face(0, [suggestion('Alice', 0.9)]), face(1, [suggestion('Bob', 0.2)])],
    })

    const { result } = await renderRun()

    expect(result.current.rows.map((row) => row.suggestion?.subject_name ?? null)).toEqual([
      'Alice',
      null,
    ])
    expect(result.current.batch).toHaveLength(1)
  })

  it('keeps the face unnamed and counts the failure when the server refuses', async () => {
    queueOf('p1', 'p2')
    facesFrom({
      p1: [face(0, [suggestion('Alice', 0.9)])],
      p2: [face(0, [suggestion('Bob', 0.8)])],
    })
    assignMock.mockRejectedValue(new Error('nope'))

    const { result } = await renderRun()
    const [row] = result.current.rows
    await act(async () => {
      result.current.confirm(row.face, offerOf(row))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(result.current.failed).toBe(1)
    // Still unnamed, still asked about, and the run has not moved on.
    expect(result.current.rows.map((row) => row.face.face_index)).toEqual([0])
    expect(result.current.photo?.uid).toBe('p1')
  })

  it('goes back to the photo it came from, even once that photo is finished', async () => {
    queueOf('p1', 'p2')
    facesFrom({
      p1: [face(0, [suggestion('Alice', 0.9)])],
      p2: [face(0, [suggestion('Bob', 0.8)])],
    })

    const { result } = await renderRun()
    expect(result.current.canGoBack).toBe(false)

    const [row] = result.current.rows
    await act(async () => {
      result.current.confirm(row.face, offerOf(row))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    await waitFor(() => {
      expect(result.current.photo?.uid).toBe('p2')
    })
    expect(result.current.canGoBack).toBe(true)

    act(() => {
      result.current.back()
    })
    await waitFor(() => {
      expect(result.current.photo?.uid).toBe('p1')
    })
    // Shown as it was left: the face is named, so there is nothing left to ask.
    expect(result.current.rows).toHaveLength(0)
    expect(result.current.canGoBack).toBe(false)
  })

  it('skips forward on demand and walks past a photo whose faces cannot be read', async () => {
    queueOf('p1', 'p2', 'p3')
    facesFrom({
      p1: [face(0, [suggestion('Alice', 0.9)])],
      // p2 missing on purpose: the request fails.
      p3: [face(0, [suggestion('Cyril', 0.8)])],
    })

    const { result } = await renderRun()
    act(() => {
      result.current.next()
    })

    await waitFor(() => {
      expect(result.current.photo?.uid).toBe('p3')
    })
    expect(result.current.position).toBe(3)
  })

  it('reports the queue it could not load rather than an empty album', async () => {
    photosMock.mockRejectedValue(new Error('boom'))

    const { result } = await renderRun()

    expect(result.current.status).toBe('error')
  })
})

describe('useAlbumFaceCount', () => {
  it('asks the same search for its total alone', async () => {
    photosMock.mockResolvedValue({
      photos: [],
      total: 12,
      limit: 1,
      offset: 0,
      next_offset: null,
    })

    const { result } = renderHook(() => useAlbumFaceCount('al1'))

    await waitFor(() => {
      expect(result.current).toBe(12)
    })
    expect(photosMock).toHaveBeenCalledWith(
      { album: 'al1', q: 'face:new', limit: 1 },
      expect.anything(),
    )
  })

  it('stays unknown when the count cannot be fetched', async () => {
    photosMock.mockRejectedValue(new Error('boom'))

    const { result } = renderHook(() => useAlbumFaceCount('al1'))

    await waitFor(() => {
      expect(photosMock).toHaveBeenCalled()
    })
    expect(result.current).toBeNull()
  })
})

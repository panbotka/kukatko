import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { type FacesResponse } from '../services/people'

import { useFaces } from './useFaces'

// Only the network calls are faked; the hook's selection/optimistic logic runs.
vi.mock('../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/people')>()
  return { ...actual, fetchFaces: vi.fn(), assignFace: vi.fn() }
})

const { fetchFaces, assignFace } = await import('../services/people')
const fetchMock = vi.mocked(fetchFaces)
const assignMock = vi.mocked(assignFace)

/** A faces response with one unnamed detection carrying one suggestion. */
function facesResponse(): FacesResponse {
  return {
    photo_uid: 'ph1',
    width: 1000,
    height: 800,
    orientation: 1,
    faces: [
      {
        face_index: 0,
        bbox: [0.1, 0.2, 0.3, 0.4],
        det_score: 0.9,
        action: 'create_marker',
        suggestions: [
          { subject_uid: 'su_a', subject_name: 'Alice', distance: 0.1, confidence: 0.9 },
        ],
      },
    ],
  }
}

/** The same photo with the face already assigned to a marker. */
function namedResponse(): FacesResponse {
  const base = facesResponse()
  return {
    ...base,
    faces: [
      { ...base.faces[0], marker_uid: 'mk_1', subject_name: 'Alice', action: 'assign_person' },
    ],
  }
}

/** Mounts the hook and waits for the initial fetch to settle. */
async function renderReady(response: FacesResponse = facesResponse()) {
  fetchMock.mockResolvedValue(response)
  const rendered = renderHook(() => useFaces('ph1'))
  await waitFor(() => {
    expect(rendered.result.current.status).toBe('ready')
  })
  return rendered
}

beforeEach(() => {
  fetchMock.mockReset()
  assignMock.mockReset()
  assignMock.mockResolvedValue(undefined)
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useFaces', () => {
  it('loads the detections for the photo', async () => {
    const { result } = await renderReady()

    expect(fetchMock).toHaveBeenCalledWith('ph1', expect.anything())
    expect(result.current.faces).toHaveLength(1)
    expect(result.current.selected).toBeNull()
  })

  it('reports an empty detection list without erroring', async () => {
    const { result } = await renderReady({ ...facesResponse(), faces: [] })

    expect(result.current.status).toBe('ready')
    expect(result.current.faces).toEqual([])
  })

  it('surfaces a failed fetch as an error state', async () => {
    fetchMock.mockRejectedValue(new Error('boom'))
    const { result } = renderHook(() => useFaces('ph1'))

    await waitFor(() => {
      expect(result.current.status).toBe('error')
    })
    expect(result.current.faces).toEqual([])
  })

  it('resolves the selected face from its index', async () => {
    const { result } = await renderReady()

    act(() => {
      result.current.select(0)
    })
    expect(result.current.selected?.face_index).toBe(0)

    act(() => {
      result.current.select(null)
    })
    expect(result.current.selected).toBeNull()
  })

  it('creates a marker when accepting a suggestion for an unmatched face', async () => {
    const { result } = await renderReady()
    const face = result.current.faces[0]

    // Hold the assignment in flight so the optimistic name is observable at all:
    // once it settles, the reconciling refetch replaces it with the server's answer.
    let settleAssign: () => void = () => undefined
    assignMock.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          settleAssign = resolve
        }),
    )
    fetchMock.mockResolvedValue(namedResponse())

    act(() => {
      result.current.acceptSuggestion(face, face.suggestions[0])
    })

    expect(assignMock).toHaveBeenCalledWith('ph1', {
      action: 'create_marker',
      bbox: [0.1, 0.2, 0.3, 0.4],
      face_index: 0,
      subject_uid: 'su_a',
    })
    expect(result.current.busy).toBe(true)
    expect(result.current.faces[0].subject_name).toBe('Alice')
    // Only the server can mint a marker, so its absence proves this is the
    // optimistic state and not the refetched one.
    expect(result.current.faces[0].marker_uid).toBeUndefined()

    settleAssign()

    await waitFor(() => {
      expect(result.current.busy).toBe(false)
    })
    expect(result.current.faces[0].subject_name).toBe('Alice')
    expect(result.current.faces[0].marker_uid).toBe('mk_1')
  })

  it('assigns a free-text name to an unmatched face', async () => {
    const { result } = await renderReady()

    act(() => {
      result.current.assignName(result.current.faces[0], 'Bob')
    })

    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith('ph1', {
        action: 'create_marker',
        bbox: [0.1, 0.2, 0.3, 0.4],
        face_index: 0,
        subject_name: 'Bob',
      })
    })
  })

  it('assigns an already-matched face in place via its marker', async () => {
    const { result } = await renderReady(namedResponse())

    act(() => {
      result.current.assignName(result.current.faces[0], 'Carol')
    })

    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith('ph1', {
        action: 'assign_person',
        marker_uid: 'mk_1',
        subject_name: 'Carol',
      })
    })
  })

  it('unassigns a named face and ignores unassign on an unmatched one', async () => {
    const { result } = await renderReady(namedResponse())

    act(() => {
      result.current.unassign(result.current.faces[0])
    })
    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith('ph1', {
        action: 'unassign_person',
        marker_uid: 'mk_1',
      })
    })

    assignMock.mockClear()
    act(() => {
      result.current.unassign({ ...result.current.faces[0], marker_uid: undefined })
    })
    expect(assignMock).not.toHaveBeenCalled()
  })

  it('flags a failed assignment and refetches to reconcile', async () => {
    const { result } = await renderReady()
    assignMock.mockRejectedValue(new Error('nope'))

    act(() => {
      result.current.assignName(result.current.faces[0], 'Bob')
    })

    await waitFor(() => {
      expect(result.current.actionError).toBe(true)
    })
    expect(result.current.busy).toBe(false)
    // One initial load plus the reconciling refetch.
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})

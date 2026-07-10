import { useCallback, useEffect, useState } from 'react'

import {
  type AssignRequest,
  assignFace,
  type FaceView,
  fetchFaces,
  type FacesResponse,
  type Suggestion,
} from '../services/people'

/** Fetch lifecycle of the faces detected on one photo. */
type State = { status: 'loading' } | { status: 'error' } | { status: 'ready'; data: FacesResponse }

/** What {@link useFaces} exposes: the detections plus the naming actions. */
export interface UseFacesResult {
  /** Whether the detections are still loading, failed, or are ready. */
  status: 'loading' | 'error' | 'ready'
  /** The detected faces; empty while loading, on error, or when none were found. */
  faces: FaceView[]
  /** The face whose naming panel is open, or null when none is selected. */
  selected: FaceView | null
  /** True while an assignment request is in flight. */
  busy: boolean
  /** True when the last assignment failed; the faces were refetched. */
  actionError: boolean
  /** Opens (or, with null, closes) the naming panel for a face. */
  select: (faceIndex: number | null) => void
  /** Names a face by accepting one of its ranked identity suggestions. */
  acceptSuggestion: (face: FaceView, suggestion: Suggestion) => void
  /** Names a face with free text (the subject is found or created server-side). */
  assignName: (face: FaceView, name: string) => void
  /** Clears a face's current assignment. */
  unassign: (face: FaceView) => void
}

/**
 * Builds the assignment request for naming `face` with the given identity (by
 * subject UID or free-text name). A face matched to an existing marker is
 * assigned in place; an unmatched detection creates a new marker from its bbox.
 */
function buildAssign(
  face: FaceView,
  who: Pick<AssignRequest, 'subject_uid' | 'subject_name'>,
): AssignRequest {
  if (face.marker_uid !== undefined && face.marker_uid !== '') {
    return { action: 'assign_person', marker_uid: face.marker_uid, ...who }
  }
  return { action: 'create_marker', bbox: face.bbox, face_index: face.face_index, ...who }
}

/**
 * Loads a photo's detected faces and owns the naming state machine: selection,
 * optimistic assignment, and the refetch that reconciles with the server. Split
 * out of the view so the photo detail can draw the boxes as an overlay on its one
 * image while rendering the naming panel elsewhere on the page.
 */
export function useFaces(photoUid: string): UseFacesResult {
  const [state, setState] = useState<State>({ status: 'loading' })
  const [selected, setSelected] = useState<number | null>(null)
  const [busy, setBusy] = useState(false)
  const [actionError, setActionError] = useState(false)

  const reload = useCallback(
    async (signal?: AbortSignal) => {
      const data = await fetchFaces(photoUid, signal)
      setState({ status: 'ready', data })
    },
    [photoUid],
  )

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    setSelected(null)
    reload(controller.signal).catch((err: unknown) => {
      if (err instanceof DOMException && err.name === 'AbortError') {
        return
      }
      setState({ status: 'error' })
    })
    return () => {
      controller.abort()
    }
  }, [reload])

  /** Optimistically updates the named face in place before the server confirms. */
  const applyOptimistic = useCallback((faceIndex: number, name: string | undefined) => {
    setState((prev) => {
      if (prev.status !== 'ready') {
        return prev
      }
      const faces = prev.data.faces.map((face) =>
        face.face_index === faceIndex ? { ...face, subject_name: name, suggestions: [] } : face,
      )
      return { status: 'ready', data: { ...prev.data, faces } }
    })
  }, [])

  const runAssign = useCallback(
    async (face: FaceView, req: AssignRequest, optimisticName: string | undefined) => {
      setBusy(true)
      setActionError(false)
      applyOptimistic(face.face_index, optimisticName)
      setSelected(null)
      try {
        await assignFace(photoUid, req)
        await reload()
      } catch {
        setActionError(true)
        await reload().catch(() => undefined)
      } finally {
        setBusy(false)
      }
    },
    [applyOptimistic, photoUid, reload],
  )

  const acceptSuggestion = useCallback(
    (face: FaceView, suggestion: Suggestion) => {
      void runAssign(
        face,
        buildAssign(face, { subject_uid: suggestion.subject_uid }),
        suggestion.subject_name,
      )
    },
    [runAssign],
  )

  const assignName = useCallback(
    (face: FaceView, name: string) => {
      void runAssign(face, buildAssign(face, { subject_name: name }), name)
    },
    [runAssign],
  )

  const unassign = useCallback(
    (face: FaceView) => {
      if (face.marker_uid === undefined || face.marker_uid === '') {
        return
      }
      void runAssign(face, { action: 'unassign_person', marker_uid: face.marker_uid }, undefined)
    },
    [runAssign],
  )

  const faces = state.status === 'ready' ? state.data.faces : []
  return {
    status: state.status,
    faces,
    selected: faces.find((face) => face.face_index === selected) ?? null,
    busy,
    actionError,
    select: setSelected,
    acceptSuggestion,
    assignName,
    unassign,
  }
}

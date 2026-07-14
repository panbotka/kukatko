import { useCallback, useEffect, useRef, useState } from 'react'

import { isNamed } from '../lib/faceState'
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
  /**
   * Names a face by accepting one of its ranked identity suggestions — or any
   * subject picked from the typeahead, which is the same thing minus the ranking.
   */
  acceptSuggestion: (
    face: FaceView,
    subject: Pick<Suggestion, 'subject_uid' | 'subject_name'>,
  ) => void
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
 * Returns the `face_index` of the first face nobody has named, or null when every
 * face is named. Ordered by array position, never by `face_index`: markers with no
 * detected face carry negative indexes.
 */
function firstUnnamed(faces: FaceView[]): number | null {
  return faces.find((face) => !isNamed(face))?.face_index ?? null
}

/**
 * Returns the `face_index` of the next face left to name after the one at
 * `afterIndex`, wrapping to the start of the list, or null when none remains. Used
 * to walk a group photo without going back to the mouse between people; the face
 * just named is skipped even if the optimistic patch has not landed yet.
 */
function nextUnnamed(faces: FaceView[], afterIndex: number): number | null {
  const at = faces.findIndex((face) => face.face_index === afterIndex)
  const ordered = at < 0 ? faces : [...faces.slice(at + 1), ...faces.slice(0, at)]
  return (
    ordered.find((face) => face.face_index !== afterIndex && !isNamed(face))?.face_index ?? null
  )
}

/**
 * Loads a photo's detected faces and owns the naming state machine: selection,
 * optimistic assignment, and the refetch that reconciles with the server. Split
 * out of the view so the photo detail can draw the boxes as an overlay on its one
 * image while rendering the naming panel elsewhere on the page.
 *
 * On load the first unnamed face is selected, and naming one advances to the next —
 * so a photo full of people is worked through from the keyboard alone.
 */
export function useFaces(photoUid: string): UseFacesResult {
  const [state, setState] = useState<State>({ status: 'loading' })
  const [selected, setSelected] = useState<number | null>(null)
  const [busy, setBusy] = useState(false)
  const [actionError, setActionError] = useState(false)

  const reload = useCallback(
    async (signal?: AbortSignal, autoSelect = false) => {
      const data = await fetchFaces(photoUid, signal)
      setState({ status: 'ready', data })
      if (autoSelect) {
        setSelected(firstUnnamed(data.faces))
      }
    },
    [photoUid],
  )

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    setSelected(null)
    // Only the initial load picks a face. The refetch that reconciles a mutation
    // must not, or naming the last face would drag the selection back to the top.
    reload(controller.signal, true).catch((err: unknown) => {
      if (err instanceof DOMException && err.name === 'AbortError') {
        return
      }
      setState({ status: 'error' })
    })
    return () => {
      controller.abort()
    }
  }, [reload])

  // The faces as of this render, for runAssign to compute the next target from
  // without capturing a stale list in its closure.
  const facesRef = useRef<FaceView[]>([])

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
    async (
      face: FaceView,
      req: AssignRequest,
      optimisticName: string | undefined,
      advance: boolean,
    ) => {
      setBusy(true)
      setActionError(false)
      applyOptimistic(face.face_index, optimisticName)
      // Naming a face moves on to the next one left to name. Unassigning keeps it
      // selected: it has just become unnamed, and the reason to unassign is almost
      // always to name it something else.
      setSelected(advance ? nextUnnamed(facesRef.current, face.face_index) : face.face_index)
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
    (face: FaceView, subject: Pick<Suggestion, 'subject_uid' | 'subject_name'>) => {
      void runAssign(
        face,
        buildAssign(face, { subject_uid: subject.subject_uid }),
        subject.subject_name,
        true,
      )
    },
    [runAssign],
  )

  const assignName = useCallback(
    (face: FaceView, name: string) => {
      void runAssign(face, buildAssign(face, { subject_name: name }), name, true)
    },
    [runAssign],
  )

  const unassign = useCallback(
    (face: FaceView) => {
      if (face.marker_uid === undefined || face.marker_uid === '') {
        return
      }
      void runAssign(
        face,
        { action: 'unassign_person', marker_uid: face.marker_uid },
        undefined,
        false,
      )
    },
    [runAssign],
  )

  const faces = state.status === 'ready' ? state.data.faces : []
  facesRef.current = faces
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

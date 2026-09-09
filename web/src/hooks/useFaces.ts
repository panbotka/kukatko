import { useCallback, useEffect, useRef, useState } from 'react'

import { displayFrame, type Frame, readingOrder } from '../lib/faceGeometry'
import { isNamed } from '../lib/faceState'
import { type FaceConfirmation } from '../lib/faceSuggestion'
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

/**
 * Live progress of a running "confirm all" over the faces panel's suggestions,
 * read by the button that started it. `failed` survives the run: it is how the
 * panel reports afterwards that some faces stayed unnamed.
 */
export interface FacesConfirmAllState {
  running: boolean
  current: number
  total: number
  failed: number
}

/** What {@link useFaces} exposes: the detections plus the naming actions. */
export interface UseFacesResult {
  /** Whether the detections are still loading, failed, or are ready. */
  status: 'loading' | 'error' | 'ready'
  /**
   * The detected faces in reading order (see {@link readingOrder}); empty while
   * loading, on error, or when none were found.
   */
  faces: FaceView[]
  /**
   * The photo's display frame (its pixel dimensions with the EXIF orientation
   * applied), null until the detections are ready. Face bboxes are normalised
   * against it, so anything cropping to one — the people chips — needs it to keep
   * the face's proportions.
   */
  frame: Frame | null
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
  /**
   * Names every face of a prepared batch, one request at a time, through the same
   * path a single confirmation takes. The batch is the caller's (see
   * `lib/faceSuggestion.bulkConfirmations`) — this hook only walks it.
   */
  confirmAll: (targets: FaceConfirmation[]) => void
  /** Stops a running batch before its next face; what is confirmed stays confirmed. */
  cancelConfirmAll: () => void
  /** Progress of the running batch, and the failure tally of the last one. */
  confirmAllState: FacesConfirmAllState
}

/** A batch that has not been started, or has been forgotten. */
const IDLE_CONFIRM_ALL: FacesConfirmAllState = {
  running: false,
  current: 0,
  total: 0,
  failed: 0,
}

/**
 * Builds the assignment request for naming `face` with the given identity (by
 * subject UID or free-text name). A face matched to an existing marker is
 * assigned in place; one with no marker creates a new marker from its bbox.
 *
 * That fork is the backend's own (`internal/facematch`) and stays here; the UI
 * shows neither of its two branches, because naming either face is the same one
 * click (see `lib/faceState`).
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
 * The detections are put into **reading order** (`readingOrder`) the moment they
 * land, so every consumer numbers and walks them the way the eye crosses the
 * photo: left to right, top to bottom. Detection order is the model's own and on a
 * group photo is effectively arbitrary.
 *
 * On load the first unnamed face is selected, and naming one advances to the next —
 * so a photo full of people is worked through from the keyboard alone.
 *
 * The photo detail pages prev/next without remounting the hook, so every response
 * is checked against the photo it was requested for before it is applied: a
 * reconcile still in flight for the photo just left never paints its faces (or its
 * marker uids, which would then be assigned against the wrong photo) over the one
 * now on screen.
 */
export function useFaces(photoUid: string): UseFacesResult {
  const [state, setState] = useState<State>({ status: 'loading' })
  const [selected, setSelected] = useState<number | null>(null)
  const [busy, setBusy] = useState(false)
  const [actionError, setActionError] = useState(false)
  const [confirmAllState, setConfirmAllState] = useState<FacesConfirmAllState>(IDLE_CONFIRM_ALL)

  // A batch runs outside React state so the loop can read its own liveness
  // between two awaits: `cancelBatch` is the stop button, `batchRunning` keeps a
  // second click from starting a parallel run over the same faces.
  const cancelBatch = useRef(false)
  const batchRunning = useRef(false)

  // The photo this render is showing, and a monotonic id of the newest request.
  // Together they decide whether a response is still wanted: the uid rejects work
  // left over from a previous photo (the mutation reconcile carries no abort
  // signal, and aborting it would surface as a failed assignment), the id drops a
  // slow response overtaken by a newer one for the same photo.
  const currentUidRef = useRef(photoUid)
  currentUidRef.current = photoUid
  const latestRequest = useRef(0)

  const reload = useCallback(
    async (signal?: AbortSignal, autoSelect = false) => {
      if (currentUidRef.current !== photoUid) {
        // A reconcile for a photo already left: drop it before it claims the
        // request id and invalidates the current photo's load.
        return
      }
      const requestId = latestRequest.current + 1
      latestRequest.current = requestId
      const data = await fetchFaces(photoUid, signal)
      if (latestRequest.current !== requestId || currentUidRef.current !== photoUid) {
        return
      }
      // Reading order, once, here — everything downstream numbers faces by their
      // array position (the boxes on the photo, the panel rows, the people chips)
      // and walks them in it, so the one order the detector happened to emit is
      // replaced at the single point they all read from.
      const ordered = { ...data, faces: readingOrder(data.faces) }
      setState({ status: 'ready', data: ordered })
      if (autoSelect) {
        setSelected(firstUnnamed(ordered.faces))
      }
    },
    [photoUid],
  )

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    setSelected(null)
    // A mutation still settling belongs to the photo just left; the new one starts
    // with a clean action state rather than inheriting its spinner or its error.
    setBusy(false)
    setActionError(false)
    // A batch belongs to the photo it was started on; the new one neither
    // continues it nor inherits its failure tally.
    cancelBatch.current = true
    batchRunning.current = false
    setConfirmAllState(IDLE_CONFIRM_ALL)
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
        if (currentUidRef.current !== photoUid) {
          // The reader has moved on; the failure belongs to the photo they left.
          return
        }
        setActionError(true)
        await reload().catch(() => undefined)
      } finally {
        if (currentUidRef.current === photoUid) {
          setBusy(false)
        }
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

  const confirmAll = useCallback(
    (targets: FaceConfirmation[]) => {
      if (batchRunning.current || targets.length === 0) {
        return
      }
      // The list as it stands before the batch, so the face selected at the end
      // is computed the way a single confirmation computes it: from what was
      // still unnamed when the click happened.
      const before = facesRef.current
      batchRunning.current = true
      cancelBatch.current = false
      setBusy(true)
      setActionError(false)
      setConfirmAllState({ running: true, current: 0, total: targets.length, failed: 0 })
      void (async () => {
        const confirmed = new Set<number>()
        let done = 0
        let failed = 0
        for (const target of targets) {
          if (cancelBatch.current || currentUidRef.current !== photoUid) {
            break
          }
          done += 1
          applyOptimistic(target.face.face_index, target.subject.subject_name)
          try {
            await assignFace(
              photoUid,
              buildAssign(target.face, { subject_uid: target.subject.subject_uid }),
            )
            confirmed.add(target.face.face_index)
          } catch {
            // The run goes on: one face the server refused is not a reason to
            // abandon the rest, and it simply stays unnamed for a second look.
            failed += 1
          }
          setConfirmAllState((state) => ({ ...state, current: done, failed }))
        }
        batchRunning.current = false
        if (currentUidRef.current !== photoUid) {
          return
        }
        // One reconcile at the end rather than one per face: it replaces every
        // optimistic name with what the server stored, so the faces that failed
        // come back unnamed by themselves.
        await reload().catch(() => undefined)
        setSelected(
          before.find((face) => !isNamed(face) && !confirmed.has(face.face_index))?.face_index ??
            null,
        )
        setBusy(false)
        setConfirmAllState((state) => ({ ...state, running: false }))
      })()
    },
    [applyOptimistic, photoUid, reload],
  )

  const cancelConfirmAll = useCallback(() => {
    cancelBatch.current = true
  }, [])

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
    frame:
      state.status === 'ready'
        ? displayFrame(state.data.width, state.data.height, state.data.orientation)
        : null,
    selected: faces.find((face) => face.face_index === selected) ?? null,
    busy,
    actionError,
    select: setSelected,
    acceptSuggestion,
    assignName,
    unassign,
    confirmAll,
    cancelConfirmAll,
    confirmAllState,
  }
}

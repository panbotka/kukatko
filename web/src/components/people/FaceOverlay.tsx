import { useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { faceBoxStyle } from '../../lib/faceGeometry'
import {
  type AssignRequest,
  assignFace,
  type FaceView,
  fetchFaces,
  type FacesResponse,
  type Suggestion,
} from '../../services/people'
import { thumbUrl } from '../../services/photos'

import { FaceAssignPanel } from './FaceAssignPanel'

/** Props for {@link FaceOverlay}. */
export interface FaceOverlayProps {
  /** Photo whose faces are drawn and named. */
  photoUid: string
  /** Thumbnail size used as the base image. Defaults to `fit_1280`. */
  imageSize?: string
}

/** Fetch lifecycle of the overlay. */
type State = { status: 'loading' } | { status: 'error' } | { status: 'ready'; data: FacesResponse }

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
 * Draws clickable face boxes over a photo from normalised bboxes and lets the
 * user name each face: accept a ranked suggestion with one tap or type a name.
 * Self-contained — it fetches the faces, applies each assignment optimistically,
 * and refetches to reconcile. It is the reusable building block a photo detail
 * page mounts over the image.
 */
export function FaceOverlay({ photoUid, imageSize = 'fit_1280' }: FaceOverlayProps) {
  const { t } = useTranslation()
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

  if (state.status === 'loading') {
    return (
      <div className="d-flex justify-content-center py-3">
        <Spinner animation="border" role="status" size="sm">
          <span className="visually-hidden">{t('faces.loading')}</span>
        </Spinner>
      </div>
    )
  }

  if (state.status === 'error') {
    return <p className="text-secondary small mb-0">{t('faces.error')}</p>
  }

  const { faces } = state.data
  const selectedFace = faces.find((face) => face.face_index === selected) ?? null

  return (
    <section aria-label={t('faces.title')}>
      <div className="position-relative d-inline-block w-100">
        <img
          src={thumbUrl(photoUid, imageSize)}
          alt={t('faces.title')}
          className="w-100 h-auto rounded"
          style={{ display: 'block' }}
        />
        {faces.map((face) => {
          const named = face.subject_name !== undefined && face.subject_name !== ''
          const label = named
            ? (face.subject_name ?? '')
            : t('faces.unnamed', { index: face.face_index + 1 })
          return (
            <button
              key={face.face_index}
              type="button"
              aria-label={label}
              title={label}
              aria-pressed={selected === face.face_index}
              onClick={() => {
                setSelected(face.face_index)
              }}
              className="position-absolute p-0 border-2"
              style={{
                ...faceBoxStyle(face.bbox),
                borderStyle: 'solid',
                borderColor: named ? 'var(--bs-success)' : 'var(--bs-warning)',
                background: 'transparent',
                cursor: 'pointer',
              }}
            />
          )
        })}
      </div>

      {faces.length === 0 && <p className="text-secondary small mt-2 mb-0">{t('faces.none')}</p>}

      {actionError && (
        <Alert variant="danger" className="mt-2 py-2 small">
          {t('faces.assignError')}
        </Alert>
      )}

      {selectedFace && (
        <FaceAssignPanel
          face={selectedFace}
          busy={busy}
          onAcceptSuggestion={(suggestion) => {
            acceptSuggestion(selectedFace, suggestion)
          }}
          onAssignName={(name) => {
            assignName(selectedFace, name)
          }}
          onUnassign={() => {
            unassign(selectedFace)
          }}
          onClose={() => {
            setSelected(null)
          }}
        />
      )}
    </section>
  )
}

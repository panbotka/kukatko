import { useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import {
  assignFace,
  fetchOutliers,
  type OutlierFace,
  type OutlierResult,
} from '../../services/people'

import { FaceThumb } from './FaceThumb'

/** Props for {@link Outliers}. */
export interface OutliersProps {
  /** Subject whose assigned faces are ranked for review. */
  subjectUid: string
}

/** Fetch lifecycle of the outlier list. */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; result: OutlierResult }

/** A stable key for an outlier face (unique within a subject). */
function faceKey(face: OutlierFace): string {
  return `${face.photo_uid}:${String(face.face_index)}`
}

/**
 * The outlier-review section of a subject page: the subject's assigned faces
 * ranked by distance from their embedding centroid (most likely mis-assigned
 * first), each with a one-tap unassign. Removing a face detaches it from the
 * subject via the face-assignment API and drops it from the list optimistically.
 * With too few faces to be meaningful the list is shown but flagged.
 */
export function Outliers({ subjectUid }: OutliersProps) {
  const { t } = useTranslation()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [busyKey, setBusyKey] = useState<string | null>(null)
  const [actionError, setActionError] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchOutliers(subjectUid, controller.signal)
      .then((result) => {
        setState({ status: 'ready', result })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setState({ status: 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [subjectUid])

  const unassign = useCallback(async (face: OutlierFace) => {
    if (face.marker_uid === undefined || face.marker_uid === '') {
      return
    }
    const key = faceKey(face)
    setBusyKey(key)
    setActionError(false)
    try {
      await assignFace(face.photo_uid, { action: 'unassign_person', marker_uid: face.marker_uid })
      setState((prev) => {
        if (prev.status !== 'ready') {
          return prev
        }
        const faces = prev.result.faces.filter((f) => faceKey(f) !== key)
        return { status: 'ready', result: { ...prev.result, faces, count: faces.length } }
      })
    } catch {
      setActionError(true)
    } finally {
      setBusyKey(null)
    }
  }, [])

  if (state.status === 'loading') {
    return (
      <div className="d-flex justify-content-center py-3">
        <Spinner animation="border" role="status" size="sm">
          <span className="visually-hidden">{t('outliers.loading')}</span>
        </Spinner>
      </div>
    )
  }

  if (state.status === 'error') {
    return <p className="text-secondary small mb-0">{t('outliers.error')}</p>
  }

  const { faces, meaningful } = state.result
  if (faces.length === 0) {
    return <p className="text-secondary small mb-0">{t('outliers.empty')}</p>
  }

  return (
    <section aria-label={t('outliers.title')}>
      {!meaningful && (
        <Alert variant="info" className="py-2 small">
          {t('outliers.notMeaningful')}
        </Alert>
      )}
      {actionError && (
        <Alert variant="danger" className="py-2 small">
          {t('outliers.unassignError')}
        </Alert>
      )}
      <div className="d-flex flex-wrap gap-3">
        {faces.map((face) => {
          const key = faceKey(face)
          const label = t('outliers.faceLabel', { distance: face.distance.toFixed(3) })
          return (
            <div key={key} className="text-center" style={{ width: '96px' }}>
              <Link to={`/photos/${face.photo_uid}`} aria-label={t('outliers.openPhoto')}>
                <FaceThumb photoUid={face.photo_uid} bbox={face.bbox} label={label} />
              </Link>
              <div className="small text-secondary mt-1">{face.distance.toFixed(3)}</div>
              <Button
                variant="outline-danger"
                size="sm"
                className="mt-1 w-100"
                disabled={
                  busyKey === key || face.marker_uid === undefined || face.marker_uid === ''
                }
                onClick={() => {
                  void unassign(face)
                }}
              >
                {t('outliers.unassign')}
              </Button>
            </div>
          )
        })}
      </div>
    </section>
  )
}

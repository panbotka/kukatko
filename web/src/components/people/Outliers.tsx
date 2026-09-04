import { useCallback, useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { canUnassign, distancePercent, outlierKey } from '../../lib/outlierReview'
import {
  answerQuestion,
  askableQuestions,
  hiddenCount,
  OUTLIER_SECTION_BATCH,
  type OutlierQuestion,
  revealedQuestions,
  toQuestions,
} from '../../lib/outlierSection'
import { assignFace, fetchOutliers, type OutlierFace } from '../../services/people'
import { Icon } from '../Icon'

import { FaceCrop } from './FaceCrop'

import './outliers.css'

/** Props for {@link Outliers}. */
export interface OutliersProps {
  /** Subject whose assigned faces are ranked for review. */
  subjectUid: string
}

/** Fetch lifecycle of the outlier list. */
type State = { status: 'loading' } | { status: 'error' } | { status: 'ready'; meaningful: boolean }

/** Which write failed, so the section can name it. */
type ActionError = 'unassign' | 'restore'

/** Edge length of one face in the strip, in CSS pixels. */
const FACE_SIZE = 112

/**
 * The outlier section of a subject page: **a question, asked a few faces at a
 * time.** The backend ranks the person's assigned faces by distance from their
 * embedding centroid; the least alike come first, and the reader answers "no,
 * that is not them" on the ones that are wrong.
 *
 * Everything about the section follows from it being a question rather than a
 * fault report. It shows one small batch ({@link OUTLIER_SECTION_BATCH}) with an
 * explicit way to see more, instead of the wall of some three hundred tiles it
 * used to be. It says *least like the others first* in words and keeps the raw
 * cosine distance in the tile's tooltip, where a number that means nothing to a
 * reader can still serve a diagnosis. The answer is one quiet link under each
 * face, not a row of red buttons — and it is **undoable on the spot**: the tile
 * stays where it is, dimmed, with the way back next to it.
 *
 * Nothing is written until the reader acts, and a write is applied only once the
 * server has taken it, so a tile never claims a change that did not happen.
 *
 * A face whose picture cannot be produced ({@link FaceCrop} could not fetch its
 * rendition) is **dropped from the list** rather than offered as a grey square:
 * the next ranked face slides into the place it held. With nothing left to ask
 * about — no outliers at all, or none of them renderable — the section renders
 * nothing, frame and heading included.
 */
export function Outliers({ subjectUid }: OutliersProps) {
  const { t } = useTranslation()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [items, setItems] = useState<OutlierQuestion[]>([])
  const [unavailable, setUnavailable] = useState<ReadonlySet<string>>(() => new Set<string>())
  const [revealed, setRevealed] = useState(OUTLIER_SECTION_BATCH)
  const [busyKey, setBusyKey] = useState<string | null>(null)
  const [actionError, setActionError] = useState<ActionError | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    setItems([])
    setUnavailable(new Set<string>())
    setRevealed(OUTLIER_SECTION_BATCH)
    setActionError(null)
    fetchOutliers(subjectUid, undefined, controller.signal)
      .then((result) => {
        setItems(toQuestions(result.faces))
        setState({ status: 'ready', meaningful: result.meaningful })
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

  // The two writes are exact opposites and go through the ordinary assign state
  // machine: `unassign_person` detaches the marker from the person,
  // `assign_person` puts the very same marker back. That is what makes the undo
  // a real undo rather than a re-tag: the marker survives the detach (only its
  // subject is cleared), so nothing about the face's region is lost in between.
  const answer = useCallback(
    async (face: OutlierFace, next: 'removed' | 'pending') => {
      if (!canUnassign(face)) {
        return
      }
      const key = outlierKey(face)
      setBusyKey(key)
      setActionError(null)
      try {
        await assignFace(
          face.photo_uid,
          next === 'removed'
            ? { action: 'unassign_person', marker_uid: face.marker_uid }
            : { action: 'assign_person', marker_uid: face.marker_uid, subject_uid: subjectUid },
        )
        setItems((prev) => answerQuestion(prev, key, next))
      } catch {
        setActionError(next === 'removed' ? 'unassign' : 'restore')
      } finally {
        setBusyKey(null)
      }
    },
    [subjectUid],
  )

  const markUnavailable = useCallback((key: string) => {
    setUnavailable((prev) => (prev.has(key) ? prev : new Set(prev).add(key)))
  }, [])

  const askable = useMemo(() => askableQuestions(items, unavailable), [items, unavailable])

  if (state.status === 'loading') {
    // Nothing is drawn while the ranking is in flight: a heading that appears and
    // then vanishes for the many people who have no outliers at all is exactly the
    // empty frame this section is meant not to be.
    return null
  }

  const shown = state.status === 'ready' ? revealedQuestions(askable, revealed) : []
  if (state.status === 'ready' && shown.length === 0) {
    return null
  }
  const hidden = hiddenCount(askable, revealed)

  return (
    <section className="mt-4" aria-label={t('outliers.title')}>
      <h2 className="kk-section-title">{t('outliers.title')}</h2>
      <p className="text-secondary small">{t('outliers.subtitle')}</p>

      {state.status === 'error' && (
        <p className="text-secondary small mb-0">{t('outliers.error')}</p>
      )}

      {state.status === 'ready' && !state.meaningful && (
        <Alert variant="info" className="py-2 small">
          {t('outliers.notMeaningful')}
        </Alert>
      )}
      {actionError !== null && (
        <Alert
          variant="danger"
          dismissible
          onClose={() => {
            setActionError(null)
          }}
          className="py-2 small"
        >
          {actionError === 'unassign' ? t('outliers.unassignError') : t('outliers.restoreError')}
        </Alert>
      )}

      <div className="d-flex flex-wrap gap-3">
        {shown.map(({ face, answer: given }) => {
          const key = outlierKey(face)
          const removed = given === 'removed'
          return (
            <div key={key} className="text-center" style={{ width: `${String(FACE_SIZE)}px` }}>
              <Link
                to={`/photos/${face.photo_uid}`}
                aria-label={t('outliers.openPhoto')}
                /* The number the tiles used to be captioned with. It says nothing
                   to a reader ranking faces by eye, but it is still the thing the
                   ordering is made of, so it stays reachable for a diagnosis. */
                title={t('outliers.distanceTitle', { percent: distancePercent(face.distance) })}
                className="d-block"
              >
                <FaceCrop
                  photoUid={face.photo_uid}
                  bbox={face.bbox}
                  // The link around it already names the action; a second
                  // announcement of the same face would only be noise.
                  label=""
                  size={FACE_SIZE}
                  className={`rounded${removed ? ' opacity-50' : ''}`}
                  onUnavailable={() => {
                    markUnavailable(key)
                  }}
                />
              </Link>
              {removed ? (
                <div className="small text-secondary mt-1 lh-sm">
                  <span className="d-block">{t('outliers.removed')}</span>
                  <Button
                    variant="link"
                    size="sm"
                    className="kk-outlier-answer"
                    disabled={busyKey === key}
                    onClick={() => {
                      void answer(face, 'pending')
                    }}
                  >
                    <Icon name="arrow-counterclockwise" /> {t('outliers.undo')}
                  </Button>
                </div>
              ) : (
                <Button
                  variant="link"
                  size="sm"
                  className="kk-outlier-answer mt-1"
                  disabled={busyKey === key || !canUnassign(face)}
                  onClick={() => {
                    void answer(face, 'removed')
                  }}
                >
                  {t('outliers.unassign')}
                </Button>
              )}
            </div>
          )
        })}
      </div>

      <div className="d-flex flex-wrap align-items-center gap-2 mt-3">
        {hidden > 0 && (
          <Button
            variant="outline-primary"
            size="sm"
            onClick={() => {
              setRevealed((count) => count + OUTLIER_SECTION_BATCH)
            }}
          >
            {t('outliers.showMore', { remaining: hidden })}
          </Button>
        )}
        {/* This panel is the right tool when you are already looking at a person;
            the full page is the one for a proper sweep — threshold, context crops,
            bulk and keyboard. Hand the person over so it opens ready to work. */}
        <Link
          to={`/outliers?subject=${encodeURIComponent(subjectUid)}`}
          className="btn btn-sm btn-outline-secondary"
        >
          {t('outliers.reviewAll')}
        </Link>
      </div>
    </section>
  )
}

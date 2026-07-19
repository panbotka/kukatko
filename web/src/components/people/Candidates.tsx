import { type ReactNode, useCallback, useEffect, useRef, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useCandidateReview } from '../../hooks/useCandidateReview'
import { candidateKey, isActionable } from '../../lib/candidateReview'
import { percentToDistance, THRESHOLD_DEFAULT_PERCENT } from '../../lib/faceThreshold'
import { type CandidateResult, searchCandidates } from '../../services/faces'
import { CandidateCard } from '../faces/CandidateCard'
import { EmptyState } from '../EmptyState'
import { Icon } from '../Icon'

/** Props for {@link Candidates}. */
export interface CandidatesProps {
  /** Subject whose untagged look-alikes the search hunts for. */
  subjectUid: string
  /**
   * Called after the user confirms a candidate (a face was assigned to this subject),
   * so the page can refresh its gallery/counters. Fired optimistically, once per
   * confirm; the pages that pass it hand over their `reload`.
   */
  onAssigned?: () => void
}

/**
 * How many candidates the embedded section asks for. The cards are large full-frame
 * previews and the search is expensive, so the on-page section caps the list and sends
 * anyone wanting a full sweep to the `/faces` workspace (no limit, threshold slider,
 * keyboard flow).
 */
const SEARCH_LIMIT = 60

/** Fetch lifecycle of the candidate search; it stays idle until the user asks for it. */
type State =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; result: CandidateResult }

/**
 * The per-subject candidate section of a subject page: photos where this person
 * probably appears by face similarity but is not tagged yet, each with a one-tap
 * confirm/reject. It is the `/faces` workspace in miniature — reusing its search
 * service, {@link useCandidateReview} lifecycle and {@link CandidateCard} — but the
 * search is expensive, so nothing runs until the button is pressed.
 *
 * Confirming assigns the face (through the shared write path), drops the card and
 * refreshes the gallery; rejecting persists "not this person" and drops the card. The
 * structural empty cases — the subject has no tagged faces yet, or its faces have no
 * embedding (the box was offline) — are explained rather than left blank.
 */
export function Candidates({ subjectUid, onAssigned }: CandidatesProps) {
  const { t } = useTranslation()
  const [state, setState] = useState<State>({ status: 'idle' })
  const abortRef = useRef<AbortController | null>(null)

  const result = state.status === 'ready' ? state.result : null
  // The review acts on the subject the *results* belong to, and resets whenever a fresh
  // search hands it a new `candidates` array — the same contract the `/faces` page uses.
  const review = useCandidateReview(
    result === null ? null : result.subject_uid,
    result === null ? null : result.candidates,
  )

  const search = useCallback(() => {
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    setState({ status: 'loading' })
    searchCandidates(
      subjectUid,
      { threshold: percentToDistance(THRESHOLD_DEFAULT_PERCENT), limit: SEARCH_LIMIT },
      controller.signal,
    )
      .then((res) => {
        setState({ status: 'ready', result: res })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setState({ status: 'error' })
      })
  }, [subjectUid])

  // Walking from one person to the next reuses this page: re-arm the section (back to
  // the button) for the new subject, and abort any search still in flight.
  useEffect(() => {
    setState({ status: 'idle' })
    return () => {
      abortRef.current?.abort()
    }
  }, [subjectUid])

  // A confirmed card leaves the list, a rejected one is already gone: show only the
  // faces still awaiting a verdict (or whose confirm errored and can be retried).
  const visible = review.items.filter(isActionable)

  let body: ReactNode = null
  if (result !== null) {
    if (result.reason === 'no_faces') {
      body = (
        <EmptyState
          size="sm"
          icon={<Icon name="person-circle" />}
          title={t('candidates.noFaces.title')}
          hint={t('candidates.noFaces.hint')}
        />
      )
    } else if (result.reason === 'no_embeddings') {
      body = (
        <EmptyState
          size="sm"
          icon={<Icon name="person-circle" />}
          title={t('candidates.noEmbeddings.title')}
          hint={t('candidates.noEmbeddings.hint')}
        />
      )
    } else if (result.candidates.length === 0) {
      body = (
        <EmptyState
          size="sm"
          icon={<Icon name="people" />}
          title={t('candidates.zero.title')}
          hint={t('candidates.zero.hint')}
        />
      )
    } else if (visible.length === 0) {
      body = <EmptyState size="sm" title={t('candidates.allReviewed')} />
    } else {
      body = (
        <div
          className="d-grid gap-3"
          style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(14rem, 1fr))' }}
        >
          {visible.map((item) => (
            <CandidateCard
              key={candidateKey(item.candidate)}
              item={item}
              focused={false}
              onConfirm={() => {
                review.confirm(item.candidate)
                onAssigned?.()
              }}
              onReject={() => {
                review.reject(item.candidate)
              }}
            />
          ))}
        </div>
      )
    }
  }

  const loading = state.status === 'loading'

  return (
    <section aria-label={t('candidates.title')}>
      <div className="d-flex flex-wrap align-items-center gap-2 mb-3">
        <Button variant="primary" size="sm" disabled={loading} onClick={search}>
          {loading ? (
            <>
              <Spinner as="span" animation="border" size="sm" role="status" aria-hidden="true" />{' '}
              {t('candidates.searching')}
            </>
          ) : (
            <>
              <Icon name="search" />{' '}
              {state.status === 'ready' ? t('candidates.rerun') : t('candidates.search')}
            </>
          )}
        </Button>
        {/* Power users get the full sweep — threshold slider, keyboard flow, confirm-all
            — on /faces, which pre-fills this subject and auto-runs. */}
        <Link
          to={`/faces?subject=${encodeURIComponent(subjectUid)}`}
          className="btn btn-sm btn-outline-secondary"
        >
          {t('candidates.reviewAll')}
        </Link>
      </div>

      {review.actionError !== null && (
        <Alert variant="danger" dismissible onClose={review.dismissError} className="py-2 small">
          {review.actionError.kind === 'reject'
            ? t('candidates.rejectError')
            : t('candidates.confirmError')}
        </Alert>
      )}

      {state.status === 'idle' && (
        <p className="text-secondary small mb-0">{t('candidates.idleHint')}</p>
      )}
      {state.status === 'error' && (
        <p className="text-secondary small mb-0">{t('candidates.error')}</p>
      )}
      {body}
    </section>
  )
}

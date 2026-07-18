import { useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import ButtonGroup from 'react-bootstrap/ButtonGroup'
import Table from 'react-bootstrap/Table'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { FadeInImage } from '../components/FadeInImage'
import { Icon } from '../components/Icon'
import { ListSkeleton } from '../components/Skeleton'
import { useReloadKey } from '../hooks/useReloadKey'
import { useUrlState } from '../lib/urlState'
import { formatDateTime } from '../lib/format'
import {
  type DecisionFilter,
  parseDecisionFilter,
  REVIEW_DECISIONS_DEFAULTS,
  REVIEW_DECISIONS_PAGE_SIZE,
  type ReviewDecision,
  type ReviewDecisionsView,
  toReviewDecision,
  viewToAuditParams,
} from '../lib/reviewDecisions'
import { type AuditListResponse, fetchAuditLog } from '../services/audit'
import { fetchLabels } from '../services/organize'
import { fetchSubjects } from '../services/people'
import { fetchLeaderboard, type Leaderboard, type LeaderboardEntry } from '../services/review'
import { thumbUrl } from '../services/photos'

/** The centre-cropped square thumbnail size for the compact decision list. */
const THUMB_SIZE = 'tile_100'

/**
 * The Ano/Ne filter options with the i18n key for each button label, `as const`
 * so the keys stay literal and the typed `t()` accepts them (a typo is a compile
 * error). Mirrors the WINDOW_LABEL_KEYS pattern in {@link LeaderboardPage}.
 */
const FILTERS = [
  { value: '', labelKey: 'reviewDecisions.filter.all' },
  { value: 'yes', labelKey: 'reviewDecisions.decision.yes' },
  { value: 'no', labelKey: 'reviewDecisions.decision.no' },
] as const

/** The data-load state of the decision listing. */
type State =
  | { status: 'noUser' }
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; data: AuditListResponse }

/**
 * The admin per-user review-decision view (reached from the leaderboard): one
 * user's sorting decisions from the review game — which photos they confirmed
 * (Ano) or rejected (Ne) as which person or label — read from the durable audit
 * trail via the `via=review` filter. It shows the user's name and tallies up top
 * (reusing the leaderboard counts), a photo thumbnail and Ano/Ne for every
 * decision, and an Ano/Ne filter, with the selected user, filter and page kept in
 * the URL so "Back always works". Admin-or-higher only. See docs/FRONTEND.md.
 */
export function ReviewDecisionsPage() {
  const { t, i18n } = useTranslation()
  const { isAdmin } = useAuth()
  const [view, setView] = useUrlState<ReviewDecisionsView>(REVIEW_DECISIONS_DEFAULTS)
  const [reloadKey, reload] = useReloadKey()
  const params = useMemo(() => viewToAuditParams(view), [view])

  const [board, setBoard] = useState<Leaderboard | null>(null)
  const [subjects, setSubjects] = useState<ReadonlyMap<string, string>>(
    () => new Map<string, string>(),
  )
  const [labels, setLabels] = useState<ReadonlyMap<string, string>>(() => new Map<string, string>())
  const [state, setState] = useState<State>({ status: 'loading' })

  // The leaderboard (for the header name + tallies) and the subject/label rosters
  // (to resolve a decision's target to a name) are fetched once and best-effort:
  // a roster failure must not blank the page, so each result is applied on its own.
  useEffect(() => {
    if (!isAdmin) {
      return
    }
    const controller = new AbortController()
    Promise.allSettled([
      fetchLeaderboard('all', controller.signal),
      fetchSubjects(controller.signal),
      fetchLabels(controller.signal),
    ])
      .then(([boardResult, subjectsResult, labelsResult]) => {
        if (controller.signal.aborted) {
          return
        }
        if (boardResult.status === 'fulfilled') {
          setBoard(boardResult.value)
        }
        if (subjectsResult.status === 'fulfilled') {
          setSubjects(
            new Map(
              subjectsResult.value.map((subject): [string, string] => [subject.uid, subject.name]),
            ),
          )
        }
        if (labelsResult.status === 'fulfilled') {
          setLabels(
            new Map(labelsResult.value.map((label): [string, string] => [label.uid, label.name])),
          )
        }
      })
      .catch(() => {
        // allSettled never rejects; this only guards a synchronous mishap.
      })
    return () => {
      controller.abort()
    }
  }, [isAdmin])

  // The decision page itself: refetched whenever the user, Ano/Ne filter or page
  // (all folded into `params`) changes, or the reload button is pressed.
  useEffect(() => {
    if (!isAdmin) {
      return
    }
    if (view.user === '') {
      setState({ status: 'noUser' })
      return
    }
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchAuditLog(params, controller.signal)
      .then((data) => {
        if (!controller.signal.aborted) {
          setState({ status: 'ready', data })
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          setState({ status: 'error' })
        }
      })
    return () => {
      controller.abort()
    }
  }, [isAdmin, params, reloadKey, view.user])

  const headerEntry = useMemo<LeaderboardEntry | null>(() => {
    if (view.user === '') {
      return null
    }
    return board?.entries.find((entry) => entry.user_uid === view.user) ?? null
  }, [board, view.user])

  const decisions = useMemo<ReviewDecision[]>(() => {
    if (state.status !== 'ready') {
      return []
    }
    return state.data.entries
      .map((record) => toReviewDecision(record, subjects, labels))
      .filter((decision): decision is ReviewDecision => decision !== null)
  }, [state, subjects, labels])

  const activeFilter = parseDecisionFilter(view.decision)

  /** Applies an Ano/Ne filter, resetting to the first page (pushes history). */
  function selectFilter(next: DecisionFilter) {
    setView({ decision: next, offset: '0' })
  }

  /** Jumps to a page offset, clamped at zero (pushes history so Back works). */
  function goToOffset(next: number) {
    setView({ offset: String(Math.max(0, next)) })
  }

  if (!isAdmin) {
    return <Alert variant="danger">{t('reviewDecisions.adminOnly')}</Alert>
  }

  const displayName = headerEntry?.display_name ?? view.user

  return (
    <>
      <div className="mb-3">
        <Link to="/leaderboard" className="d-inline-flex align-items-center gap-1 small mb-2">
          <Icon name="arrow-left" />
          {t('reviewDecisions.back')}
        </Link>
        <h1 className="kk-page-title mb-1">
          {view.user === '' ? t('reviewDecisions.title') : displayName}
        </h1>
        <p className="text-secondary mb-0">{t('reviewDecisions.subtitle')}</p>
      </div>

      {headerEntry !== null && (
        <div className="d-flex flex-wrap gap-2 mb-3" data-testid="decision-tallies">
          <Badge bg="success" className="fs-6">
            {t('reviewDecisions.decision.yes')}: {headerEntry.yes_count}
          </Badge>
          <Badge bg="danger" className="fs-6">
            {t('reviewDecisions.decision.no')}: {headerEntry.no_count}
          </Badge>
          <Badge bg="secondary" className="fs-6">
            {t('reviewDecisions.total')}: {headerEntry.total}
          </Badge>
        </div>
      )}

      {view.user !== '' && (
        <ButtonGroup size="sm" aria-label={t('reviewDecisions.filter.label')} className="mb-3">
          {FILTERS.map((filter) => {
            const isActive = filter.value === activeFilter
            return (
              <Button
                key={filter.value || 'all'}
                variant={isActive ? 'light' : 'outline-light'}
                active={isActive}
                aria-pressed={isActive}
                onClick={() => {
                  selectFilter(filter.value)
                }}
              >
                {t(filter.labelKey)}
              </Button>
            )
          })}
        </ButtonGroup>
      )}

      <DecisionContent
        state={state}
        decisions={decisions}
        locale={i18n.language}
        onRetry={reload}
        onOffset={goToOffset}
      />
    </>
  )
}

/** Props for {@link DecisionContent}. */
interface DecisionContentProps {
  state: State
  decisions: ReviewDecision[]
  locale: string
  onRetry: () => void
  onOffset: (next: number) => void
}

/** Renders the body for the current load state: skeleton, error, empty or table. */
function DecisionContent({ state, decisions, locale, onRetry, onOffset }: DecisionContentProps) {
  const { t } = useTranslation()
  if (state.status === 'noUser') {
    return (
      <EmptyState
        icon={<Icon name="people" />}
        title={t('reviewDecisions.noUser.title')}
        hint={t('reviewDecisions.noUser.hint')}
        action={
          <Link to="/leaderboard" className="btn btn-primary">
            {t('reviewDecisions.back')}
          </Link>
        }
      />
    )
  }
  if (state.status === 'loading') {
    return <ListSkeleton label={t('reviewDecisions.loading')} count={6} />
  }
  if (state.status === 'error') {
    return <ErrorState title={t('reviewDecisions.error')} onRetry={onRetry} />
  }
  if (decisions.length === 0) {
    return (
      <EmptyState
        icon={<Icon name="ui-checks" />}
        title={t('reviewDecisions.empty.title')}
        hint={t('reviewDecisions.empty.hint')}
      />
    )
  }
  return (
    <>
      <Table responsive hover className="align-middle mb-0">
        <thead>
          <tr>
            <th scope="col">{t('reviewDecisions.columns.photo')}</th>
            <th scope="col">{t('reviewDecisions.columns.decision')}</th>
            <th scope="col">{t('reviewDecisions.columns.subject')}</th>
            <th scope="col">{t('reviewDecisions.columns.when')}</th>
          </tr>
        </thead>
        <tbody>
          {decisions.map((decision) => (
            <DecisionRow key={decision.id} decision={decision} locale={locale} />
          ))}
        </tbody>
      </Table>
      <DecisionPager data={state.data} onOffset={onOffset} />
    </>
  )
}

/** Props for {@link DecisionRow}. */
interface DecisionRowProps {
  decision: ReviewDecision
  locale: string
}

/** One decision: its photo, the Ano/Ne verdict, the person or label, and when. */
function DecisionRow({ decision, locale }: DecisionRowProps) {
  const { t } = useTranslation()
  const kindLabel =
    decision.kind === 'face' ? t('reviewDecisions.kind.face') : t('reviewDecisions.kind.label')
  return (
    <tr data-testid={`decision-row-${String(decision.id)}`}>
      <td>
        <DecisionThumb photoUid={decision.photoUid} />
      </td>
      <td>
        {decision.verdict === 'yes' ? (
          <Badge bg="success" className="d-inline-flex align-items-center gap-1">
            <Icon name="check-lg" />
            {t('reviewDecisions.decision.yes')}
          </Badge>
        ) : (
          <Badge bg="danger" className="d-inline-flex align-items-center gap-1">
            <Icon name="x-lg" />
            {t('reviewDecisions.decision.no')}
          </Badge>
        )}
      </td>
      <td className="text-break">
        <Icon name={decision.kind === 'face' ? 'person-bounding-box' : 'tags'} />{' '}
        <span className="text-secondary small">{kindLabel}</span>
        <div>{decision.targetName || '—'}</div>
      </td>
      <td className="text-nowrap">{formatDateTime(decision.createdAt, locale)}</td>
    </tr>
  )
}

/** A fixed-size photo thumbnail that falls back to a blank well if it fails. */
function DecisionThumb({ photoUid }: { photoUid: string | null }) {
  const [failed, setFailed] = useState(false)
  if (photoUid === null || failed) {
    return (
      <span
        className="d-inline-block rounded bg-body-tertiary"
        style={{ width: 48, height: 48 }}
        aria-hidden="true"
        data-testid="decision-thumb-empty"
      />
    )
  }
  return (
    <FadeInImage
      src={thumbUrl(photoUid, THUMB_SIZE)}
      alt=""
      className="rounded"
      style={{ width: 48, height: 48, objectFit: 'cover' }}
      data-testid="decision-thumb"
      onError={() => {
        setFailed(true)
      }}
    />
  )
}

/** Props for {@link DecisionPager}. */
interface DecisionPagerProps {
  data: AuditListResponse
  onOffset: (next: number) => void
}

/** Prev/Next controls plus the "showing X–Y of N" range for the current page. */
function DecisionPager({ data, onOffset }: DecisionPagerProps) {
  const { t } = useTranslation()
  const from = data.total === 0 ? 0 : data.offset + 1
  const to = data.offset + data.entries.length
  return (
    <div className="d-flex align-items-center justify-content-between mt-3">
      <span className="text-secondary small">
        {t('reviewDecisions.pagination.range', { from, to, total: data.total })}
      </span>
      <div className="d-flex gap-2">
        <Button
          variant="outline-light"
          size="sm"
          disabled={data.offset === 0}
          onClick={() => {
            onOffset(data.offset - REVIEW_DECISIONS_PAGE_SIZE)
          }}
        >
          {t('reviewDecisions.pagination.prev')}
        </Button>
        <Button
          variant="outline-light"
          size="sm"
          disabled={data.next_offset === null}
          onClick={() => {
            if (data.next_offset !== null) {
              onOffset(data.next_offset)
            }
          }}
        >
          {t('reviewDecisions.pagination.next')}
        </Button>
      </div>
    </div>
  )
}

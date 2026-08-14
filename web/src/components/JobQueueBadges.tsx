import type { ParseKeys } from 'i18next'
import Badge from 'react-bootstrap/Badge'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { useJobStats } from '../hooks/useJobStats'

/** One job-queue state surfaced as a footer badge. */
interface BadgeState {
  /** The backend state key looked up in `by_state`. */
  state: string
  labelKey: ParseKeys
  /** Whether a non-zero count is a problem worth danger styling. */
  danger: boolean
}

/**
 * The job-queue states worth surfacing, in display order. Terminal successes
 * (`done`) are deliberately omitted: the badges summarise *live* queue work and
 * its failures, not the ever-growing history of finished jobs. `failed` and
 * `dead` are failure states, so a non-zero count draws the eye with danger
 * styling; only non-zero states are rendered at all.
 */
const BADGE_STATES: BadgeState[] = [
  { state: 'queued', labelKey: 'footer.jobs.queued', danger: false },
  { state: 'running', labelKey: 'footer.jobs.running', danger: false },
  { state: 'failed', labelKey: 'footer.jobs.failed', danger: true },
  { state: 'dead', labelKey: 'footer.jobs.dead', danger: true },
]

/**
 * Where the badges lead. The queue itself is a panel inside the system-status
 * page — the frontend has no route of its own for it — so that page is the
 * destination that actually shows what the counts stand for.
 */
const QUEUE_ROUTE = '/system'

/**
 * The right-hand footer status area: compact badges summarising the background
 * job queue for maintainers, wrapped in one link to {@link QUEUE_ROUTE}. The
 * `/jobs` stats endpoint is a maintainer-only operations capability, so everyone
 * below sees nothing and — because {@link useJobStats} only polls when enabled —
 * issues no request. That is the same threshold the `/system` route is guarded
 * by (`RequireRole role="maintainer"`, i.e. `roleAtLeast(role, 'maintainer')` —
 * the very predicate behind `isMaintainer`), so the link is only ever offered to
 * someone the page will actually open for. A failing request hides the badges
 * silently. When every tracked state is empty a single quiet "idle" badge stands
 * in for a row of zeros.
 *
 * The whole row is one link rather than one link per badge: it is a single
 * destination, so it earns a single tab stop, and being a real anchor makes it
 * keyboard-reachable and Enter-activatable for free. `text-decoration-none`
 * keeps the badges looking exactly as they did as plain text — the underline is
 * the only thing an anchor would have added, since a badge sets its own colour.
 * The counts alone would read as bare numbers out of context, so the link
 * carries an explicit label naming the queue, its state and where it leads.
 */
export function JobQueueBadges() {
  const { t } = useTranslation()
  const { isMaintainer } = useAuth()
  const stats = useJobStats(isMaintainer)

  if (!isMaintainer || stats === null) {
    return null
  }

  const active = BADGE_STATES.map((entry) => ({
    ...entry,
    count: stats.by_state[entry.state] ?? 0,
  })).filter((entry) => entry.count > 0)

  const summary =
    active.length === 0
      ? t('footer.jobs.idle')
      : active.map((entry) => `${t(entry.labelKey)} ${String(entry.count)}`).join(', ')
  const label = t('footer.jobs.link', { summary })

  return (
    <Link
      to={QUEUE_ROUTE}
      className="d-inline-flex flex-wrap align-items-center gap-1 text-decoration-none"
      title={label}
      aria-label={label}
    >
      {active.length === 0 ? (
        <Badge bg="secondary" className="fw-normal">
          {t('footer.jobs.idle')}
        </Badge>
      ) : (
        active.map((entry) => (
          <Badge key={entry.state} bg={entry.danger ? 'danger' : 'secondary'} className="fw-normal">
            {t(entry.labelKey)} {entry.count}
          </Badge>
        ))
      )}
    </Link>
  )
}

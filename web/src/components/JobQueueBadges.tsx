import type { ParseKeys } from 'i18next'
import Badge from 'react-bootstrap/Badge'
import { useTranslation } from 'react-i18next'

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
 * The right-hand footer status area: compact badges summarising the background
 * job queue for administrators. Non-admins see nothing and — because
 * {@link useJobStats} only polls when enabled — issue no request. A failing
 * request hides the badges silently. When every tracked state is empty a single
 * quiet "idle" badge stands in for a row of zeros.
 */
export function JobQueueBadges() {
  const { t } = useTranslation()
  const { isAdmin } = useAuth()
  const stats = useJobStats(isAdmin)

  if (!isAdmin || stats === null) {
    return null
  }

  const active = BADGE_STATES.map((entry) => ({
    ...entry,
    count: stats.by_state[entry.state] ?? 0,
  })).filter((entry) => entry.count > 0)

  return (
    <span
      className="d-inline-flex flex-wrap align-items-center gap-1"
      title={t('footer.jobs.title')}
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
    </span>
  )
}

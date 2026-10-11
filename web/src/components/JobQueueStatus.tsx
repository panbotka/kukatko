import type { ParseKeys } from 'i18next'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { useJobStats } from '../hooks/useJobStats'

import { Icon, type IconName } from './Icon'

/** One job-queue state surfaced as a phrase of the footer status line. */
interface StatusState {
  /** The backend state key looked up in `by_state`. */
  state: string
  /** The pluralised phrase (`{{count}}` + noun + verb) naming the count. */
  phraseKey: ParseKeys
  /** The glyph leading the phrase, so the line reads at a glance. */
  icon: IconName
  /** Whether a non-zero count is a problem worth the danger colour. */
  danger: boolean
}

/**
 * The job-queue states worth surfacing, in display order: the failures first,
 * because they are the one thing on the line a maintainer has to act on, then
 * the live work. Terminal successes (`done`) are deliberately omitted — the line
 * summarises *live* queue work and its failures, not the ever-growing history of
 * finished jobs. `failed` (a terminal, non-retryable failure) and `dead`
 * (attempts exhausted) are failure states and wear the danger colour; the live
 * states stay muted. Only non-zero states are rendered at all.
 */
const STATUS_STATES: StatusState[] = [
  { state: 'dead', phraseKey: 'footer.jobs.dead', icon: 'exclamation-triangle', danger: true },
  { state: 'failed', phraseKey: 'footer.jobs.failed', icon: 'exclamation-triangle', danger: true },
  { state: 'queued', phraseKey: 'footer.jobs.queued', icon: 'hourglass-split', danger: false },
  { state: 'running', phraseKey: 'footer.jobs.running', icon: 'arrow-repeat', danger: false },
]

/**
 * Where the status line leads. The queue itself is a panel inside the
 * system-status page — the frontend has no route of its own for it — and that
 * panel lists the failed jobs per type with a requeue button next to each, so
 * it is the place the counts can be acted on.
 */
const QUEUE_ROUTE = '/system'

/**
 * The right-hand footer status area: one readable line summarising the
 * background job queue for maintainers — an icon plus a full, pluralised phrase
 * per non-empty state ("17 úloh trvale selhalo · 1 úloha ve frontě"), wrapped
 * in one link to {@link QUEUE_ROUTE}. The `/jobs` stats endpoint is a
 * maintainer-only operations capability, so everyone below sees nothing and —
 * because {@link useJobStats} only polls when enabled — issues no request. That
 * is the same threshold the `/system` route is guarded by
 * (`RequireRole role="maintainer"`), so the link is only ever offered to
 * someone the page will actually open for. A failing request hides the line
 * silently, and so does a queue with nothing to report: an idle queue is not
 * news, and the footer stays the credits line it is for everyone else.
 *
 * The whole line is one link rather than one per phrase: it is a single
 * destination, so it earns a single tab stop. Its accessible name repeats the
 * visible phrases (so "label in name" holds) and adds the queue and the
 * destination, which the visible text leaves to the context.
 */
export function JobQueueStatus() {
  const { t } = useTranslation()
  const { isMaintainer } = useAuth()
  const stats = useJobStats(isMaintainer)

  if (!isMaintainer || stats === null) {
    return null
  }

  const active = STATUS_STATES.map((entry) => ({
    ...entry,
    count: stats.by_state[entry.state] ?? 0,
  }))
    .filter((entry) => entry.count > 0)
    .map((entry) => ({ ...entry, phrase: t(entry.phraseKey, { count: entry.count }) }))

  if (active.length === 0) {
    return null
  }

  const label = t('footer.jobs.link', {
    summary: active.map((entry) => entry.phrase).join(', '),
  })

  return (
    <Link to={QUEUE_ROUTE} className="kukatko-job-status" title={label} aria-label={label}>
      {active.map((entry) => (
        <span
          key={entry.state}
          data-state={entry.state}
          className={`kukatko-job-status-item ${entry.danger ? 'text-danger' : 'text-body-secondary'}`}
        >
          <Icon name={entry.icon} className="me-1" />
          {entry.phrase}
        </span>
      ))}
    </Link>
  )
}

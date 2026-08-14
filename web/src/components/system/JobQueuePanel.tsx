import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Spinner from 'react-bootstrap/Spinner'
import Table from 'react-bootstrap/Table'
import { useTranslation } from 'react-i18next'

import { formatCount } from '../../lib/format'
import type { JobsStatus } from '../../services/system'
import { JobStateLegend, type JobStateKey } from '../JobStateLegend'

/**
 * The lifecycle states the breakdown has a column for, in the order work moves
 * through them. `done` is included because without it a row's numbers would not
 * add up to its total and the reader would be left hunting for the difference.
 */
const STATE_COLUMNS = ['queued', 'running', 'failed', 'dead', 'done'] as const

/** The states explained beneath the table; `pending` is the box-waiting aggregate. */
const EXPLAINED_STATES: readonly JobStateKey[] = [
  'queued',
  'running',
  'failed',
  'dead',
  'done',
  'pending',
]

/** One row of the breakdown: a job type with its per-state counts and total. */
interface TypeRow {
  type: string
  /** Counts per state; a state the type has no jobs in is simply absent. */
  counts: Record<string, number | undefined>
  total: number
  dead: number
}

/**
 * Turns the `by_type_state` map into rows ordered by what needs attention: the
 * types with a dead letter first (that is what the page is opened for), then the
 * busiest, then alphabetically so the order is stable between two polls of an
 * idle queue.
 */
function rowsFor(jobs: JobsStatus): TypeRow[] {
  const rows = Object.entries(jobs.by_type_state).map(([type, states]) => {
    const counts: Record<string, number | undefined> = {}
    let total = 0
    for (const [state, count] of Object.entries(states ?? {})) {
      counts[state] = count ?? 0
      total += count ?? 0
    }
    return { type, counts, total, dead: counts.dead ?? 0 }
  })
  rows.sort((a, b) => {
    if (a.dead !== b.dead) {
      return b.dead - a.dead
    }
    if (a.total !== b.total) {
      return b.total - a.total
    }
    return a.type.localeCompare(b.type)
  })
  return rows
}

/** Props for {@link JobQueuePanel}. */
interface JobQueuePanelProps {
  /** The queue section of the status snapshot. */
  jobs: JobsStatus
  /** Requeues the dead letter — the whole of it, or one job type. */
  onRequeue: (jobType?: string) => void
  /** The job type whose requeue is in flight, `''` for the whole dead letter. */
  requeuing: string | null
}

/**
 * The job queue, broken down by type **and** state.
 *
 * It replaces a row of badges that summed the whole queue per type, which was
 * actively misleading: the queue table keeps finished jobs, so `image_embed:
 * 41 594` against a library of 20 930 photos described a re-embedding that
 * happened once, not a backlog of twice the library. Here every type's work is
 * split across the states it is actually in, its lifetime total is labelled as
 * history, and the one number worth acting on — the dead letter — has its own
 * requeue button per row, next to the count it retries.
 */
export function JobQueuePanel({ jobs, onRequeue, requeuing }: JobQueuePanelProps) {
  const { t, i18n } = useTranslation()
  const rows = rowsFor(jobs)
  const locale = i18n.language
  return (
    <section className="mb-4" aria-labelledby="system-jobs-title">
      <h2 id="system-jobs-title" className="kk-section-title mb-1">
        {t('system.jobs.title')}
      </h2>
      <p className="text-secondary small">{t('system.jobs.intro')}</p>
      <Card>
        <Card.Body>
          {rows.length === 0 ? (
            <p className="text-secondary small mb-0">{t('system.jobs.empty')}</p>
          ) : (
            <div className="table-responsive">
              <Table size="sm" className="align-middle mb-0">
                <thead>
                  <tr>
                    <th scope="col">{t('system.jobs.type')}</th>
                    {STATE_COLUMNS.map((state) => (
                      <th scope="col" key={state} className="text-end">
                        {t(`jobStates.labels.${state}`)}
                      </th>
                    ))}
                    <th scope="col" className="text-end">
                      {t('system.jobs.lifetime')}
                    </th>
                    <th scope="col" className="text-end">
                      <span className="visually-hidden">{t('system.jobs.requeueColumn')}</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((row) => (
                    <tr key={row.type} data-testid={`job-row-${row.type}`}>
                      <th scope="row" className="fw-normal font-monospace">
                        {row.type}
                      </th>
                      {STATE_COLUMNS.map((state) => (
                        <td
                          key={state}
                          className={`text-end${
                            state === 'dead' && row.dead > 0 ? ' text-warning fw-semibold' : ''
                          }`}
                          data-testid={`job-${row.type}-${state}`}
                        >
                          {formatCount(row.counts[state] ?? 0, locale)}
                        </td>
                      ))}
                      <td className="text-end text-secondary">{formatCount(row.total, locale)}</td>
                      <td className="text-end">
                        {row.dead > 0 && (
                          <Button
                            variant="outline-primary"
                            size="sm"
                            disabled={requeuing !== null}
                            onClick={() => {
                              onRequeue(row.type)
                            }}
                          >
                            {requeuing === row.type && (
                              <Spinner
                                animation="border"
                                size="sm"
                                role="status"
                                className="me-2"
                              />
                            )}
                            {t('system.jobs.requeueType')}
                          </Button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            </div>
          )}
          <p className="text-secondary kk-text-caption mt-3 mb-0">
            {t('system.jobs.lifetimeNote', { total: formatCount(jobs.total, locale) })}
          </p>
          <Button
            className="mt-3"
            variant="outline-primary"
            size="sm"
            disabled={jobs.dead_letter === 0 || requeuing !== null}
            onClick={() => {
              onRequeue()
            }}
          >
            {requeuing === '' && (
              <Spinner animation="border" size="sm" role="status" className="me-2" />
            )}
            {t('system.jobs.requeue')}
          </Button>
          <div className="mt-3">
            <JobStateLegend states={EXPLAINED_STATES} />
          </div>
        </Card.Body>
      </Card>
    </section>
  )
}

import type { ParseKeys, TFunction } from 'i18next'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Spinner from 'react-bootstrap/Spinner'
import Table from 'react-bootstrap/Table'
import { useTranslation } from 'react-i18next'

import { formatCount } from '../../lib/format'
import type { JobsStatus } from '../../services/system'
import { JobStateLegend, type JobStateKey } from '../JobStateLegend'
import { TechnicalDetail } from '../TechnicalDetail'

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

/**
 * The job types that have a name in the family's vocabulary. A type missing here
 * — a new handler shipped before its translation, say — falls back to its own
 * id, which is wrong-looking rather than blank and says exactly what to add.
 */
const TYPE_LABELS: Record<string, ParseKeys | undefined> = {
  image_embed: 'system.jobs.types.image_embed',
  face_detect: 'system.jobs.types.face_detect',
  thumbnail: 'system.jobs.types.thumbnail',
  places: 'system.jobs.types.places',
  metadata: 'system.jobs.types.metadata',
  ocr: 'system.jobs.types.ocr',
  sidecar: 'system.jobs.types.sidecar',
  storyboard: 'system.jobs.types.storyboard',
  backup: 'system.jobs.types.backup',
  mail_send: 'system.jobs.types.mail_send',
  nameless_detach: 'system.jobs.types.nameless_detach',
  nameless_restore: 'system.jobs.types.nameless_restore',
  face_cluster: 'system.jobs.types.face_cluster',
}

/** The job type's name in the family's vocabulary, or its raw id if it has none. */
function typeLabel(type: string, t: TFunction): string {
  const key = TYPE_LABELS[type]
  return key === undefined ? type : t(key)
}

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
 * Every row is named in the family's vocabulary — "looking for faces", not
 * `face_detect` — and the ids those names stand for are listed once, behind the
 * disclosure under the table, for whoever has to match a row to a log line.
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
                      <th scope="row" className="fw-normal">
                        {typeLabel(row.type, t)}
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
          {/* The rows are named in words, but a maintainer reading a log, a
              `kukatko ctl` output or the source needs the ids those words stand
              for. One disclosure for the whole table, rather than a monospace id
              in every row nobody else can read. */}
          {rows.length > 0 && (
            <TechnicalDetail
              id="system-job-types"
              label={t('system.jobs.typesTitle')}
              className="mt-3"
            >
              <dl className="small text-secondary mb-0">
                {rows.map((row) => (
                  <div key={row.type} className="mb-1">
                    <dt className="d-inline fw-semibold text-body font-monospace">{row.type}</dt>
                    {' — '}
                    <dd className="d-inline mb-0">{typeLabel(row.type, t)}</dd>
                  </div>
                ))}
              </dl>
            </TechnicalDetail>
          )}
        </Card.Body>
      </Card>
    </section>
  )
}

import type { ParseKeys } from 'i18next'
import { useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Card from 'react-bootstrap/Card'
import Spinner from 'react-bootstrap/Spinner'
import Table from 'react-bootstrap/Table'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../auth/AuthContext'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { JobStateLegend, type JobStateKey } from '../components/JobStateLegend'
import { RecordTable, type RecordColumn } from '../components/RecordTable'
import { TechnicalDetail } from '../components/TechnicalDetail'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { formatDateTime } from '../lib/format'
import {
  fetchImportFailures,
  fetchImportRuns,
  fetchJobStats,
  type ImportCounts,
  type ImportFailure,
  type ImportRun,
  type JobStats,
  type RunStatus,
} from '../services/import'

/** How often the run history and job stats are re-polled while the page is open. */
const POLL_INTERVAL_MS = 3000

/** Bootstrap badge variant per run status. */
const STATUS_VARIANT: Record<RunStatus, string> = {
  running: 'info',
  done: 'success',
  partial: 'warning',
  failed: 'danger',
}

/** How many failure rows to show at once. */
const FAILURES_LIMIT = 100

/**
 * The queue states explained under the badges, in display order. The import page
 * shows no `total` badge (it is not what an import is watched for) and no
 * `pending` one, so the legend explains exactly the four counts above it.
 */
const IMPORT_JOB_STATES: readonly JobStateKey[] = ['queued', 'running', 'failed', 'dead']

/**
 * The one-line summary of a run that recorded an error, keyed by what the run
 * ended as: a run that stopped is a different story from one that finished with
 * a few files it could not read, and both are stories rather than stack traces.
 */
function errorSummaryKey(status: RunStatus): ParseKeys {
  if (status === 'failed') {
    return 'import.history.errorSummary.failed'
  }
  return status === 'partial'
    ? 'import.history.errorSummary.partial'
    : 'import.history.errorSummary.generic'
}

/** Fetch lifecycle of the import page data. */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; runs: ImportRun[]; failures: ImportFailure[] }

/** Formats an ISO timestamp for display using the active UI language. */
function formatTimestamp(value: string, locale: string): string {
  return formatDateTime(value, locale)
}

/** A compact row of imported/updated/skipped/deduplicated/failed count badges. */
function CountsBadges({ counts }: { counts: ImportCounts }) {
  const { t } = useTranslation()
  return (
    <span className="d-inline-flex gap-2 flex-wrap">
      <Badge bg="success">
        {t('import.counts.imported')}: {counts.imported}
      </Badge>
      <Badge bg="primary">
        {t('import.counts.updated')}: {counts.updated}
      </Badge>
      <Badge bg="secondary">
        {t('import.counts.skipped')}: {counts.skipped}
      </Badge>
      {counts.deduplicated !== undefined && counts.deduplicated > 0 && (
        <Badge bg="info">
          {t('import.counts.deduplicated')}: {counts.deduplicated}
        </Badge>
      )}
      <Badge bg={counts.failed > 0 ? 'danger' : 'secondary'}>
        {t('import.counts.failed')}: {counts.failed}
      </Badge>
    </span>
  )
}

/**
 * What is still being worked out after an import — the queue counts as badges,
 * with the shared {@link JobStateLegend} spelling out what each of them means.
 * The badge labels come from the same `jobStates.*` block as the legend, so the
 * word above a number and the word explaining it can never drift apart.
 */
function JobStatsBar({ stats }: { stats: JobStats }) {
  const { t } = useTranslation()
  return (
    <Card className="mb-4">
      <Card.Body>
        <h2 className="kk-section-title mb-1">{t('import.jobs.title')}</h2>
        <p className="text-secondary small">{t('import.jobs.intro')}</p>
        <div className="d-flex gap-2 flex-wrap mb-3">
          <Badge bg="secondary">
            {t('jobStates.labels.queued')}: {stats.by_state.queued ?? 0}
          </Badge>
          <Badge bg="info">
            {t('jobStates.labels.running')}: {stats.by_state.running ?? 0}
          </Badge>
          <Badge bg="warning" text="dark">
            {t('jobStates.labels.failed')}: {stats.by_state.failed ?? 0}
          </Badge>
          <Badge bg="dark">
            {t('jobStates.labels.dead')}: {stats.by_state.dead ?? 0}
          </Badge>
        </div>
        <JobStateLegend states={IMPORT_JOB_STATES} />
      </Card.Body>
    </Card>
  )
}

/**
 * The run-history table across all sources, most recent first. Six columns only
 * ever fitted a phone by scrolling sideways, so below `md` the shared
 * {@link RecordTable} re-lays each run as one stacked "label: value" card.
 */
function RunHistoryTable({ runs }: { runs: ImportRun[] }) {
  const { t, i18n } = useTranslation()
  if (runs.length === 0) {
    return <EmptyState size="sm" title={t('import.history.empty')} />
  }
  const columns: RecordColumn<ImportRun>[] = [
    {
      key: 'source',
      header: t('import.history.source'),
      cell: (run) => t(`import.source.${run.source}`),
    },
    {
      key: 'started',
      header: t('import.history.started'),
      cell: (run) => formatTimestamp(run.started_at, i18n.language),
    },
    {
      key: 'finished',
      header: t('import.history.finished'),
      cell: (run) => (run.finished_at ? formatTimestamp(run.finished_at, i18n.language) : '—'),
    },
    {
      key: 'status',
      header: t('import.history.status'),
      cell: (run) => (
        <Badge bg={STATUS_VARIANT[run.status]}>{t(`import.status.${run.status}`)}</Badge>
      ),
    },
    {
      key: 'counts',
      header: t('import.history.counts'),
      cell: (run) => <CountsBadges counts={run.counts} />,
    },
    {
      key: 'outcome',
      header: t('import.history.outcome'),
      // The styling rides on the value, not on `cellClassName`: the card shows the
      // same summary and it has to stay just as red and just as small there.
      cell: (run) =>
        run.last_error === '' ? (
          <span className="text-secondary small">{t('import.history.ok')}</span>
        ) : (
          <>
            <span className="text-danger small">{t(errorSummaryKey(run.status))}</span>
            <TechnicalDetail id={`import-run-error-${String(run.id)}`}>
              <div className="small font-monospace text-break text-secondary">{run.last_error}</div>
            </TechnicalDetail>
          </>
        ),
    },
  ]
  return <RecordTable records={runs} columns={columns} rowKey={(run) => String(run.id)} size="sm" />
}

/**
 * The recorded per-photo/per-file import failures (the unresolved ones).
 *
 * Each row names the step that went wrong in words — "the thumbnail", "looking
 * for faces" — instead of the `importer.Stage` id, and points at the file it
 * happened to. What the server actually said is the one thing here a maintainer
 * cannot do without and nobody else can read, so it lives behind the row's
 * {@link TechnicalDetail} together with the step id and the source reference.
 */
function FailuresPanel({ failures }: { failures: ImportFailure[] }) {
  const { t, i18n } = useTranslation()
  return (
    <Card className="mb-4">
      <Card.Body>
        <h2 className="kk-section-title mb-2">{t('import.failures.title')}</h2>
        <p className="text-secondary small">{t('import.failures.intro')}</p>
        {failures.length === 0 ? (
          <EmptyState size="sm" title={t('import.failures.empty')} />
        ) : (
          <Table striped hover responsive size="sm">
            <thead>
              <tr>
                <th>{t('import.failures.colStage')}</th>
                <th>{t('import.failures.colSource')}</th>
                <th>{t('import.failures.colRef')}</th>
                <th>{t('import.failures.colWhen')}</th>
                <th>{t('import.failures.colDetail')}</th>
              </tr>
            </thead>
            <tbody>
              {failures.map((f) => (
                <tr key={f.id}>
                  <td className="small">{t(`import.failures.stages.${f.stage}`)}</td>
                  <td className="small">{t(`import.source.${f.source}`)}</td>
                  <td className="text-break small">{f.source_ref || f.photo_uid || '—'}</td>
                  <td className="small">{formatTimestamp(f.created_at, i18n.language)}</td>
                  <td>
                    <TechnicalDetail id={`import-failure-${String(f.id)}`}>
                      <dl className="small mb-0">
                        <dt className="fw-normal text-secondary">
                          {t('import.failures.stageLabel')}
                        </dt>
                        <dd className="font-monospace text-break">{f.stage}</dd>
                        {f.detail !== '' && (
                          <>
                            <dt className="fw-normal text-secondary">
                              {t('import.failures.detailLabel')}
                            </dt>
                            <dd className="text-break">{f.detail}</dd>
                          </>
                        )}
                        <dt className="fw-normal text-secondary">
                          {t('import.failures.errorLabel')}
                        </dt>
                        <dd className="font-monospace text-break mb-0">{f.error}</dd>
                      </dl>
                    </TechnicalDetail>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card.Body>
    </Card>
  )
}

/**
 * Admin-only import console: the history of import runs, the recorded
 * per-photo/per-file failures and the background job queue.
 *
 * It is read-only. The one import that still exists is `kukatko import dir`,
 * which reads a directory on the server's disk and is therefore driven from the
 * CLI; the PhotoPrism and photo-sorter migration finished in August 2026 and was
 * removed, so there is nothing left to trigger from a browser. Its runs stay in
 * the history below — they are the catalogue's provenance record.
 */
export function ImportPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('import.title'))
  // Import is an operations capability, reachable by maintainers only (see
  // RequireImport); this in-page gate is a defensive fallback behind that guard.
  const { canImport } = useAuth()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [jobStats, setJobStats] = useState<JobStats | null>(null)

  const refresh = useCallback(async (signal?: AbortSignal) => {
    const runsResp = await fetchImportRuns(signal)
    let failures: ImportFailure[] = []
    try {
      failures = (
        await fetchImportFailures({ unresolvedOnly: true, limit: FAILURES_LIMIT }, signal)
      ).failures
    } catch {
      // The failures list is supplementary; ignore so the page still renders.
    }
    setState({ status: 'ready', runs: runsResp.runs, failures })
    try {
      setJobStats(await fetchJobStats(signal))
    } catch {
      // Job stats are supplementary; ignore failures so the page still renders.
    }
  }, [])

  useEffect(() => {
    if (!canImport) {
      return
    }
    const controller = new AbortController()
    refresh(controller.signal).catch((err: unknown) => {
      if (err instanceof DOMException && err.name === 'AbortError') {
        return
      }
      setState({ status: 'error' })
    })
    const id = window.setInterval(() => {
      // Silent poll: keep showing the last good data on a transient failure.
      void refresh().catch(() => undefined)
    }, POLL_INTERVAL_MS)
    return () => {
      controller.abort()
      window.clearInterval(id)
    }
  }, [canImport, refresh])

  if (!canImport) {
    return <Alert variant="danger">{t('import.maintainerOnly')}</Alert>
  }

  return (
    <>
      <h1 className="kk-page-title mb-3">{t('import.title')}</h1>
      <Alert variant="info">
        <p className="mb-0">{t('import.intro')}</p>
      </Alert>
      {/* The command is the one thing on this page only a maintainer at a shell
          can use, so it sits behind the same disclosure as every other
          machine-readable fact rather than in the opening paragraph. It sits
          *below* the alert, not inside it: a `variant="link"` button on a filled
          Alert inherits nothing and reads as invisible. */}
      <TechnicalDetail id="import-cli" className="mb-3">
        <p className="small mb-1">{t('import.cli')}</p>
        <code>kukatko import dir</code>
      </TechnicalDetail>

      {state.status === 'loading' && (
        <div className="d-flex justify-content-center py-5">
          <Spinner animation="border" role="status">
            <span className="visually-hidden">{t('import.loading')}</span>
          </Spinner>
        </div>
      )}

      {state.status === 'error' && (
        <ErrorState
          title={t('import.error')}
          onRetry={() => {
            void refresh()
          }}
        />
      )}

      {state.status === 'ready' && (
        <>
          {jobStats && <JobStatsBar stats={jobStats} />}

          <FailuresPanel failures={state.failures} />

          <h2 className="kk-section-title mb-3">{t('import.history.title')}</h2>
          <RunHistoryTable runs={state.runs} />
        </>
      )}
    </>
  )
}

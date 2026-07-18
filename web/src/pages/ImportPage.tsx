import { useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Col from 'react-bootstrap/Col'
import Row from 'react-bootstrap/Row'
import Spinner from 'react-bootstrap/Spinner'
import Table from 'react-bootstrap/Table'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../auth/AuthContext'
import { ConfirmModal } from '../components/ConfirmModal'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { formatDateTime } from '../lib/format'
import { ApiError } from '../services/auth'
import {
  fetchImportRuns,
  fetchJobStats,
  startImport,
  type ImportCounts,
  type ImportRun,
  type ImportSource,
  type ImportSources,
  type JobStats,
  type RunStatus,
} from '../services/import'

/** How often the run history and job stats are re-polled while the page is open. */
const POLL_INTERVAL_MS = 3000

/** The two import sources rendered as sections, in display order. */
const SOURCES: ImportSource[] = ['photoprism', 'photosorter']

/** Bootstrap badge variant per run status. */
const STATUS_VARIANT: Record<RunStatus, string> = {
  running: 'info',
  done: 'success',
  failed: 'danger',
}

/** Transient outcome of a start-import action, shown on the relevant section. */
type NoticeKind = 'started' | 'conflict' | 'error'

/** Fetch lifecycle of the import page data. */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; runs: ImportRun[]; sources: ImportSources }

/** Formats an ISO timestamp for display using the active UI language. */
function formatTimestamp(value: string, locale: string): string {
  return formatDateTime(value, locale)
}

/** Returns the most recent run for a source, or null when there is none. */
function latestForSource(runs: ImportRun[], source: ImportSource): ImportRun | null {
  return runs.find((run) => run.source === source) ?? null
}

/** A compact row of imported/updated/skipped/failed count badges. */
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
      <Badge bg={counts.failed > 0 ? 'danger' : 'secondary'}>
        {t('import.counts.failed')}: {counts.failed}
      </Badge>
    </span>
  )
}

/** Props for one import source section. */
interface SourceCardProps {
  source: ImportSource
  enabled: boolean
  latestRun: ImportRun | null
  starting: boolean
  notice: NoticeKind | null
  onStart: (source: ImportSource) => void
}

/**
 * One import source section: a description, the current/last run status with live
 * counts, and a start button. The button is disabled while a run of this source
 * is in progress, while its trigger request is in flight, or when the source is
 * not configured.
 */
function SourceCard({ source, enabled, latestRun, starting, notice, onStart }: SourceCardProps) {
  const { t, i18n } = useTranslation()
  const inProgress = latestRun?.status === 'running'

  return (
    <Card className="h-100">
      <Card.Header className="d-flex justify-content-between align-items-center">
        <span className="fw-semibold">{t(`import.source.${source}`)}</span>
        {inProgress && <Badge bg="info">{t('import.inProgress')}</Badge>}
      </Card.Header>
      <Card.Body className="d-flex flex-column">
        <p className="text-secondary small">{t(`import.${source}Desc`)}</p>

        {!enabled && <Alert variant="secondary">{t('import.notConfigured')}</Alert>}
        {notice === 'started' && <Alert variant="success">{t('import.startedNotice')}</Alert>}
        {notice === 'conflict' && <Alert variant="warning">{t('import.conflictNotice')}</Alert>}
        {notice === 'error' && <Alert variant="danger">{t('import.errorNotice')}</Alert>}

        {latestRun?.status === 'running' && (
          <div className="mb-3">
            <div className="d-flex align-items-center gap-2 mb-2">
              <Spinner animation="border" size="sm" role="status" />
              <span>
                {t('import.runningSince', {
                  time: formatTimestamp(latestRun.started_at, i18n.language),
                })}
              </span>
            </div>
            <CountsBadges counts={latestRun.counts} />
          </div>
        )}

        {latestRun && latestRun.status !== 'running' && (
          <div className="mb-3 small">
            <div className="mb-2">
              {t('import.lastRun')}:{' '}
              <Badge bg={STATUS_VARIANT[latestRun.status]}>
                {t(`import.status.${latestRun.status}`)}
              </Badge>{' '}
              {formatTimestamp(latestRun.finished_at ?? latestRun.started_at, i18n.language)}
            </div>
            <CountsBadges counts={latestRun.counts} />
          </div>
        )}

        {enabled && !latestRun && <p className="text-secondary small">{t('import.noRuns')}</p>}

        <div className="mt-auto pt-2">
          <Button
            variant="primary"
            disabled={!enabled || inProgress || starting}
            onClick={() => {
              onStart(source)
            }}
          >
            {starting && <Spinner animation="border" size="sm" role="status" className="me-2" />}
            {starting ? t('import.starting') : t('import.start')}
          </Button>
        </div>
      </Card.Body>
    </Card>
  )
}

/** The job-queue stats summary (background processing backlog). */
function JobStatsBar({ stats }: { stats: JobStats }) {
  const { t } = useTranslation()
  return (
    <Card className="mb-4">
      <Card.Body>
        <h2 className="kk-section-title mb-2">{t('import.jobs.title')}</h2>
        <div className="d-flex gap-2 flex-wrap">
          <Badge bg="secondary">
            {t('import.jobs.queued')}: {stats.by_state.queued ?? 0}
          </Badge>
          <Badge bg="info">
            {t('import.jobs.running')}: {stats.by_state.running ?? 0}
          </Badge>
          <Badge bg="warning" text="dark">
            {t('import.jobs.failed')}: {stats.by_state.failed ?? 0}
          </Badge>
          <Badge bg="dark">
            {t('import.jobs.dead')}: {stats.by_state.dead ?? 0}
          </Badge>
        </div>
      </Card.Body>
    </Card>
  )
}

/** The run-history table across all sources, most recent first. */
function RunHistoryTable({ runs }: { runs: ImportRun[] }) {
  const { t, i18n } = useTranslation()
  if (runs.length === 0) {
    return <EmptyState size="sm" title={t('import.history.empty')} />
  }
  return (
    <Table striped hover responsive size="sm">
      <thead>
        <tr>
          <th>{t('import.history.source')}</th>
          <th>{t('import.history.started')}</th>
          <th>{t('import.history.finished')}</th>
          <th>{t('import.history.status')}</th>
          <th>{t('import.history.counts')}</th>
          <th>{t('import.history.lastError')}</th>
        </tr>
      </thead>
      <tbody>
        {runs.map((run) => (
          <tr key={run.id}>
            <td>{t(`import.source.${run.source}`)}</td>
            <td>{formatTimestamp(run.started_at, i18n.language)}</td>
            <td>{run.finished_at ? formatTimestamp(run.finished_at, i18n.language) : '—'}</td>
            <td>
              <Badge bg={STATUS_VARIANT[run.status]}>{t(`import.status.${run.status}`)}</Badge>
            </td>
            <td>
              <CountsBadges counts={run.counts} />
            </td>
            <td className="text-danger small">{run.last_error || '—'}</td>
          </tr>
        ))}
      </tbody>
    </Table>
  )
}

/**
 * Admin-only import/migration console: triggers a PhotoPrism import or a
 * photo-sorter migration, shows live progress (polled run status + counts and job
 * queue stats) and the full run history. PhotoPrism stays the primary store; the
 * imports are read-only, incremental and repeatable, so the page warns before a
 * first (potentially large) run of each source.
 */
export function ImportPage() {
  const { t } = useTranslation()
  // Import is reachable by admins and the ai agent (see RequireImport); this
  // in-page gate is a defensive fallback behind that route guard.
  const { canImport } = useAuth()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [jobStats, setJobStats] = useState<JobStats | null>(null)
  const [starting, setStarting] = useState<ImportSource | null>(null)
  const [notice, setNotice] = useState<{ source: ImportSource; kind: NoticeKind } | null>(null)
  // The source awaiting first-run confirmation, or null when no dialog is open.
  const [pendingSource, setPendingSource] = useState<ImportSource | null>(null)

  const refresh = useCallback(async (signal?: AbortSignal) => {
    const runsResp = await fetchImportRuns(signal)
    setState({ status: 'ready', runs: runsResp.runs, sources: runsResp.sources })
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

  async function doStart(source: ImportSource) {
    setStarting(source)
    setNotice(null)
    try {
      await startImport(source)
      setNotice({ source, kind: 'started' })
      await refresh()
    } catch (err) {
      const kind: NoticeKind = err instanceof ApiError && err.status === 409 ? 'conflict' : 'error'
      setNotice({ source, kind })
    } finally {
      setStarting(null)
    }
  }

  // A first run of a source (nothing completed yet) can be large, so confirm it
  // through the shared dialog; later runs only process new changes and start at once.
  function handleStart(source: ImportSource) {
    const runs = state.status === 'ready' ? state.runs : []
    const hasCompleted = runs.some((run) => run.source === source && run.status === 'done')
    if (!hasCompleted) {
      setPendingSource(source)
      return
    }
    void doStart(source)
  }

  if (!canImport) {
    return <Alert variant="danger">{t('import.adminOnly')}</Alert>
  }

  return (
    <>
      <h1 className="kk-page-title mb-3">{t('import.title')}</h1>
      <Alert variant="info">
        <p className="mb-1">{t('import.intro')}</p>
        <p className="mb-0 small">{t('import.warnFirstRun')}</p>
      </Alert>

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
          <Row className="g-3 mb-4">
            {SOURCES.map((source) => (
              <Col md={6} key={source}>
                <SourceCard
                  source={source}
                  enabled={state.sources[source]}
                  latestRun={latestForSource(state.runs, source)}
                  starting={starting === source}
                  notice={notice?.source === source ? notice.kind : null}
                  onStart={(src) => {
                    handleStart(src)
                  }}
                />
              </Col>
            ))}
          </Row>

          {jobStats && <JobStatsBar stats={jobStats} />}

          <h2 className="kk-section-title mb-3">{t('import.history.title')}</h2>
          <RunHistoryTable runs={state.runs} />
        </>
      )}

      <ConfirmModal
        show={pendingSource !== null}
        variant="primary"
        title={t('import.confirmFirstRunTitle')}
        confirmLabel={t('import.start')}
        onCancel={() => {
          setPendingSource(null)
        }}
        onConfirm={() => {
          const source = pendingSource
          setPendingSource(null)
          if (source) {
            void doStart(source)
          }
        }}
      >
        {t('import.confirmFirstRun')}
      </ConfirmModal>
    </>
  )
}

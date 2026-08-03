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
import { RecordTable, type RecordColumn } from '../components/RecordTable'
import { formatDateTime, formatPercent } from '../lib/format'
import { ApiError } from '../services/auth'
import {
  fetchImportFailures,
  fetchImportRuns,
  fetchJobStats,
  fetchVerifyReport,
  startImport,
  type EntityReport,
  type ImportCounts,
  type ImportFailure,
  type ImportRun,
  type ImportSource,
  type ImportSources,
  type JobStats,
  type RunStatus,
  type VectorsReport,
  type VerifyReport,
} from '../services/import'

/** How often the run history and job stats are re-polled while the page is open. */
const POLL_INTERVAL_MS = 3000

/** The two import sources rendered as sections, in display order. */
const SOURCES: ImportSource[] = ['photoprism', 'photosorter']

/** Bootstrap badge variant per run status. */
const STATUS_VARIANT: Record<RunStatus, string> = {
  running: 'info',
  done: 'success',
  partial: 'warning',
  failed: 'danger',
}

/** How many failure rows to show at once. */
const FAILURES_LIMIT = 100

/** Transient outcome of a start-import action, shown on the relevant section. */
type NoticeKind = 'started' | 'conflict' | 'error'

/** Fetch lifecycle of the import page data. */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; runs: ImportRun[]; sources: ImportSources; failures: ImportFailure[] }

/** Formats an ISO timestamp for display using the active UI language. */
function formatTimestamp(value: string, locale: string): string {
  return formatDateTime(value, locale)
}

/** Returns the most recent run for a source, or null when there is none. */
function latestForSource(runs: ImportRun[], source: ImportSource): ImportRun | null {
  return runs.find((run) => run.source === source) ?? null
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
      key: 'lastError',
      header: t('import.history.lastError'),
      // The styling rides on the value, not on `cellClassName`: the card shows the
      // same error and it has to stay just as red and just as small there.
      cell: (run) => <span className="text-danger small">{run.last_error || '—'}</span>,
    },
  ]
  return <RecordTable records={runs} columns={columns} rowKey={(run) => String(run.id)} size="sm" />
}

/**
 * One reconciliation row: source vs catalogue counts, an optional source-coverage
 * share, and the missing tally.
 *
 * `coverage` is only supplied by the vector rows, whose `missing` is scoped to
 * photos already in the catalogue and therefore reads 0 on a catalogue holding a
 * fraction of the source. A green zero there would say "done" about a migration
 * that has barely started, so the zero badge only turns green once the coverage
 * confirms it; short of that it stays neutral next to the warning coverage.
 */
function VerifyRow({
  label,
  source,
  catalog,
  missing,
  coverage,
}: {
  label: string
  source: string | number
  catalog: string | number
  missing: number
  coverage?: number
}) {
  const { i18n } = useTranslation()
  const covered = coverage === undefined || coverage >= 1
  return (
    <tr>
      <td>{label}</td>
      <td>{source}</td>
      <td>{catalog}</td>
      <td>
        {coverage === undefined ? (
          '—'
        ) : (
          <Badge bg={covered ? 'success' : 'warning'} text={covered ? undefined : 'dark'}>
            {formatPercent(coverage, i18n.language)}
          </Badge>
        )}
      </td>
      <td>
        {missing > 0 ? (
          <Badge bg="warning" text="dark">
            {missing}
          </Badge>
        ) : (
          <Badge bg={covered ? 'success' : 'secondary'}>0</Badge>
        )}
      </td>
    </tr>
  )
}

/** A reconciliation row for one structure entity kind (albums/labels/subjects). */
function EntityRow({ label, report }: { label: string; report: EntityReport }) {
  return (
    <VerifyRow
      label={label}
      source={report.source_count}
      catalog={report.catalog_count}
      missing={report.missing_count}
    />
  )
}

/**
 * Whether the catalogue holds every embedding and every face the vector source
 * has (`importverify.VectorsReport.FullSourceCoverage`). Short of that, the
 * section's zero "missing" tallies — scoped to imported photos — need the
 * explanatory note, or a reviewer reads the vector migration as finished.
 */
function fullVectorCoverage(vectors: VectorsReport): boolean {
  return vectors.embeddings_source_coverage >= 1 && vectors.faces_source_coverage >= 1
}

/** A capped list of missing identifiers with a "showing N of total" note. */
function MissingSample({ label, ids, total }: { label: string; ids: string[]; total: number }) {
  const { t } = useTranslation()
  if (ids.length === 0) {
    return null
  }
  return (
    <div className="small mb-2">
      <strong>{label}:</strong> <code className="text-break">{ids.join(', ')}</code>
      {total > ids.length && (
        <div className="text-secondary">
          {t('import.verify.sampleNote', { shown: ids.length, total })}
        </div>
      )}
    </div>
  )
}

/** Renders a completed reconciliation report: a verdict, a counts table and the
 * concrete lists of what is missing. */
function VerifyReportView({ report }: { report: VerifyReport }) {
  const { t } = useTranslation()
  const { photoprism: pp, vectors: v, structure: s } = report
  return (
    <div className="mt-3">
      <Alert variant={report.complete ? 'success' : 'warning'}>
        {report.complete ? t('import.verify.complete') : t('import.verify.incomplete')}
      </Alert>
      {pp.listing_shortfall > 0 && (
        <Alert variant="danger" className="small py-2">
          {t('import.verify.listingShortfall', {
            reported: pp.source_reported_total,
            served: pp.source_total,
            n: pp.listing_shortfall,
          })}
        </Alert>
      )}
      <Table size="sm" bordered responsive>
        <thead>
          <tr>
            <th>{t('import.verify.colCheck')}</th>
            <th>{t('import.verify.colSource')}</th>
            <th>{t('import.verify.colCatalog')}</th>
            <th>{t('import.verify.colCoverage')}</th>
            <th>{t('import.verify.colMissing')}</th>
          </tr>
        </thead>
        <tbody>
          <VerifyRow
            label={t('import.verify.photos')}
            source={pp.source_total}
            catalog={pp.imported_count}
            missing={pp.missing_count}
          />
          <VerifyRow
            label={t('import.verify.files')}
            source="—"
            catalog="—"
            missing={pp.file_gap_count}
          />
          {!v.not_configured && (
            <>
              <VerifyRow
                label={t('import.verify.embeddings')}
                source={v.source_photos_with_embeddings}
                catalog={v.catalog_embeddings}
                missing={v.embeddings_missing_for_imported_photos}
                coverage={v.embeddings_source_coverage}
              />
              <VerifyRow
                label={t('import.verify.faces')}
                source={v.source_total_faces}
                catalog={v.catalog_faces}
                missing={v.faces_missing_for_imported_photos}
                coverage={v.faces_source_coverage}
              />
            </>
          )}
          <EntityRow label={t('import.verify.albums')} report={s.albums} />
          <EntityRow label={t('import.verify.labels')} report={s.labels} />
          <EntityRow label={t('import.verify.subjects')} report={s.subjects} />
        </tbody>
      </Table>
      {pp.deduplicated_count > 0 && (
        <p className="small text-secondary">
          {t('import.verify.deduplicated')}: {pp.deduplicated_count}
        </p>
      )}
      {s.albums.skipped_by_design_count > 0 && (
        <p className="small text-secondary">
          {t('import.verify.albumsSkipped', {
            n: s.albums.skipped_by_design_count,
            types: s.albums.skipped_types.join(', '),
          })}
        </p>
      )}
      {v.not_configured && (
        <Alert variant="secondary" className="small py-2">
          {t('import.verify.notConfigured')}
        </Alert>
      )}
      {!v.not_configured && !fullVectorCoverage(v) && (
        <Alert variant="warning" className="small py-2">
          {t('import.verify.vectorsScopeNote')}
        </Alert>
      )}
      <MissingSample
        label={t('import.verify.photos')}
        ids={pp.missing_uids}
        total={pp.missing_count}
      />
      <MissingSample
        label={t('import.verify.surplus')}
        ids={pp.surplus_uids}
        total={pp.surplus_count}
      />
      <MissingSample
        label={t('import.verify.embeddings')}
        ids={v.embeddings_missing_uids}
        total={v.embeddings_missing_for_imported_photos}
      />
    </div>
  )
}

/**
 * The completeness-check section: a button that runs the reconciliation on demand
 * (it walks the whole source library, so it is not polled) and renders the report.
 * The button is disabled when PhotoPrism is not configured (verify would 503).
 */
function CompletenessCard({ enabled }: { enabled: boolean }) {
  const { t } = useTranslation()
  const [phase, setPhase] = useState<'idle' | 'running' | 'error'>('idle')
  const [report, setReport] = useState<VerifyReport | null>(null)

  async function run() {
    setPhase('running')
    try {
      setReport(await fetchVerifyReport())
      setPhase('idle')
    } catch {
      setPhase('error')
    }
  }

  return (
    <Card className="mb-4">
      <Card.Body>
        <h2 className="kk-section-title mb-2">{t('import.verify.title')}</h2>
        <p className="text-secondary small">{t('import.verify.desc')}</p>
        <Button
          variant="outline-primary"
          disabled={!enabled || phase === 'running'}
          onClick={() => {
            void run()
          }}
        >
          {phase === 'running' && (
            <Spinner animation="border" size="sm" role="status" className="me-2" />
          )}
          {phase === 'running' ? t('import.verify.running') : t('import.verify.button')}
        </Button>
        {phase === 'error' && (
          <Alert variant="danger" className="mt-3">
            {t('import.verify.error')}
          </Alert>
        )}
        {report && <VerifyReportView report={report} />}
      </Card.Body>
    </Card>
  )
}

/** The recorded per-photo/per-file import-failure list (unresolved failures). */
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
                <th>{t('import.failures.colDetail')}</th>
                <th>{t('import.failures.colError')}</th>
                <th>{t('import.failures.colWhen')}</th>
              </tr>
            </thead>
            <tbody>
              {failures.map((f) => (
                <tr key={f.id}>
                  <td>
                    <Badge bg="secondary">{f.stage}</Badge>
                  </td>
                  <td className="small">{f.source}</td>
                  <td className="text-break">
                    <code>{f.source_ref || f.photo_uid || '—'}</code>
                  </td>
                  <td className="small text-break">{f.detail || '—'}</td>
                  <td className="small text-danger text-break">{f.error}</td>
                  <td className="small">{formatTimestamp(f.created_at, i18n.language)}</td>
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
 * Admin-only import/migration console: triggers a PhotoPrism import or a
 * photo-sorter migration, shows live progress (polled run status + counts and job
 * queue stats), the full run history, a completeness check that reconciles the
 * sources against the catalogue, and the recorded per-photo/per-file failures.
 * PhotoPrism stays the primary store; the imports are read-only, incremental and
 * repeatable, so the page warns before a first (potentially large) run of each source.
 */
export function ImportPage() {
  const { t } = useTranslation()
  // Import is an operations capability, reachable by maintainers only (see
  // RequireImport); this in-page gate is a defensive fallback behind that guard.
  const { canImport } = useAuth()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [jobStats, setJobStats] = useState<JobStats | null>(null)
  const [starting, setStarting] = useState<ImportSource | null>(null)
  const [notice, setNotice] = useState<{ source: ImportSource; kind: NoticeKind } | null>(null)
  // The source awaiting first-run confirmation, or null when no dialog is open.
  const [pendingSource, setPendingSource] = useState<ImportSource | null>(null)

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
    setState({ status: 'ready', runs: runsResp.runs, sources: runsResp.sources, failures })
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
    return <Alert variant="danger">{t('import.maintainerOnly')}</Alert>
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

          <CompletenessCard enabled={state.sources.photoprism} />

          <FailuresPanel failures={state.failures} />

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

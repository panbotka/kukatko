import { useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Col from 'react-bootstrap/Col'
import Row from 'react-bootstrap/Row'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { ErrorState } from '../components/ErrorState'
import { JobStateLegend, type JobStateKey } from '../components/JobStateLegend'
import { formatBytes, formatDateTime } from '../lib/format'
import { ApiError } from '../services/auth'
import type { ImportRun, ImportSource } from '../services/import'
import {
  fetchSystemStatus,
  requeueDeadLetterJobs,
  triggerBackup,
  type BackupStatus,
  type DatabaseStatus,
  type EmbeddingsStatus,
  type ImportsStatus,
  type JobsStatus,
  type MapsState,
  type MapsStatus,
  type StorageStatus,
  type SystemStatus,
  type VersionInfo,
} from '../services/system'

/** How often the status snapshot is re-polled while the page is open. */
const POLL_INTERVAL_MS = 5000

/**
 * The job-queue states explained beneath the queue badges, in display order.
 * Includes `pending` (work waiting on the AI box), which the System page shows
 * and the Maintenance page does not.
 */
const SYSTEM_JOB_STATES: readonly JobStateKey[] = [
  'total',
  'queued',
  'running',
  'failed',
  'dead',
  'pending',
]

/** Fetch lifecycle of the system-status page. */
type State = { status: 'loading' } | { status: 'error' } | { status: 'ready'; data: SystemStatus }

/** Transient outcome of a quick action, shown as a dismissible alert. */
type ActionNotice = { kind: 'success'; message: string } | { kind: 'error'; message: string }

/** Formats an ISO timestamp for display using the active UI language. */
function formatTimestamp(value: string, locale: string): string {
  return formatDateTime(value, locale)
}

/** The build version / commit card. */
function VersionCard({ version }: { version: VersionInfo }) {
  const { t } = useTranslation()
  return (
    <Card className="h-100">
      <Card.Body>
        <h2 className="kk-section-title mb-2">{t('system.version.title')}</h2>
        <div className="kk-section-title">{version.version}</div>
        <div className="text-secondary small font-monospace text-break">{version.commit}</div>
      </Card.Body>
    </Card>
  )
}

/** The database-reachability card. */
function DatabaseCard({ database }: { database: DatabaseStatus }) {
  const { t } = useTranslation()
  return (
    <Card className="h-100">
      <Card.Body>
        <h2 className="kk-section-title mb-2">{t('system.database.title')}</h2>
        {database.reachable ? (
          <Badge bg="success">{t('system.database.reachable')}</Badge>
        ) : (
          <Badge bg="danger">{t('system.database.unreachable')}</Badge>
        )}
      </Card.Body>
    </Card>
  )
}

/** The embeddings-sidecar card, surfacing the offline-but-queued state. */
function EmbeddingsCard({
  embeddings,
  pending,
}: {
  embeddings: EmbeddingsStatus
  pending: number
}) {
  const { t } = useTranslation()
  return (
    <Card className="h-100">
      <Card.Body>
        <h2 className="kk-section-title mb-2">{t('system.embeddings.title')}</h2>
        {embeddings.online ? (
          <Badge bg="success">{t('system.embeddings.online')}</Badge>
        ) : (
          <Badge bg="warning" text="dark">
            {t('system.embeddings.offline')}
          </Badge>
        )}
        <div className="text-secondary small font-monospace text-break mt-2">{embeddings.url}</div>
        {!embeddings.online && pending > 0 && (
          <Alert variant="warning" className="mt-3 mb-0 small">
            {t('system.embeddings.offlineHint', { n: pending })}
          </Alert>
        )}
      </Card.Body>
    </Card>
  )
}

/** The badge variant per map-provider state: only a degradation is alarming. */
const MAPS_BADGE = {
  unknown: 'secondary',
  ok: 'success',
  key_rejected: 'danger',
  rate_limited: 'warning',
  unavailable: 'warning',
  error: 'warning',
} as const satisfies Record<MapsState, string>

/** The i18n label per map-provider state. */
const MAPS_LABEL = {
  unknown: 'system.maps.unknown',
  ok: 'system.maps.ok',
  key_rejected: 'system.maps.keyRejected',
  rate_limited: 'system.maps.rateLimited',
  unavailable: 'system.maps.unavailable',
  error: 'system.maps.error',
} as const satisfies Record<MapsState, string>

/**
 * The map-provider card. A rejected mapy.com key is the failure that otherwise
 * hides — the map view just goes grey — so it is called out here, in red, with
 * what has to be done about it.
 */
function MapsCard({ maps }: { maps: MapsStatus }) {
  const { t, i18n } = useTranslation()
  if (!maps.configured) {
    return (
      <Card className="h-100">
        <Card.Body>
          <h2 className="kk-section-title mb-2">{t('system.maps.title')}</h2>
          <Badge bg="secondary">{t('system.maps.notConfigured')}</Badge>
        </Card.Body>
      </Card>
    )
  }
  const variant = MAPS_BADGE[maps.state]
  return (
    <Card className="h-100">
      <Card.Body>
        <h2 className="kk-section-title mb-2">{t('system.maps.title')}</h2>
        <Badge bg={variant} text={variant === 'warning' ? 'dark' : undefined}>
          {t(MAPS_LABEL[maps.state])}
        </Badge>
        {maps.checked_at !== undefined && (
          <div className="text-secondary small mt-2">
            {t('system.maps.checkedAt')}: {formatTimestamp(maps.checked_at, i18n.language)}
          </div>
        )}
        {maps.state === 'key_rejected' && (
          <Alert variant="danger" className="mt-3 mb-0 small">
            {t('system.maps.keyRejectedHint')}
          </Alert>
        )}
        {maps.degraded && maps.detail !== undefined && maps.detail !== '' && (
          <div className="text-secondary small font-monospace text-break mt-2">{maps.detail}</div>
        )}
      </Card.Body>
    </Card>
  )
}

/** The job-queue depth card with the dead-letter requeue quick action. */
function JobsCard({
  jobs,
  onRequeue,
  requeuing,
}: {
  jobs: JobsStatus
  onRequeue: () => void
  requeuing: boolean
}) {
  const { t } = useTranslation()
  return (
    <Card className="h-100">
      <Card.Body>
        <h2 className="kk-section-title mb-1">{t('system.jobs.title')}</h2>
        <p className="text-secondary small">{t('system.jobs.intro')}</p>
        <div className="d-flex gap-2 flex-wrap mb-3">
          <Badge bg="primary">
            {t('system.jobs.total')}: {jobs.total}
          </Badge>
          <Badge bg="secondary">
            {t('system.jobs.queued')}: {jobs.by_state.queued ?? 0}
          </Badge>
          <Badge bg="info">
            {t('system.jobs.running')}: {jobs.by_state.running ?? 0}
          </Badge>
          <Badge bg="warning" text="dark">
            {t('system.jobs.failed')}: {jobs.by_state.failed ?? 0}
          </Badge>
          <Badge bg="dark">
            {t('system.jobs.dead')}: {jobs.dead_letter}
          </Badge>
          <Badge bg="secondary">
            {t('system.jobs.pending')}: {jobs.pending_embeddings}
          </Badge>
        </div>
        <JobStateLegend states={SYSTEM_JOB_STATES} />
        <Button
          className="mt-3"
          variant="outline-primary"
          size="sm"
          disabled={jobs.dead_letter === 0 || requeuing}
          onClick={onRequeue}
        >
          {requeuing && <Spinner animation="border" size="sm" role="status" className="me-2" />}
          {t('system.jobs.requeue')}
        </Button>
      </Card.Body>
    </Card>
  )
}

/** Renders the most recent run of one import source, or a "never" placeholder. */
function ImportRunLine({ source, run }: { source: ImportSource; run: ImportRun | null }) {
  const { t, i18n } = useTranslation()
  return (
    <div className="mb-2">
      <span className="fw-semibold">{t(`import.source.${source}`)}</span>:{' '}
      {run ? (
        <>
          <Badge
            bg={run.status === 'done' ? 'success' : run.status === 'failed' ? 'danger' : 'info'}
          >
            {t(`import.status.${run.status}`)}
          </Badge>{' '}
          <span className="text-secondary small">
            {formatTimestamp(run.finished_at ?? run.started_at, i18n.language)} ·{' '}
            {t('import.counts.imported')} {run.counts.imported}
          </span>
        </>
      ) : (
        <span className="text-secondary small">{t('system.imports.never')}</span>
      )}
    </div>
  )
}

/** The last-import-per-source card with a link to the import flow. */
function ImportsCard({ imports }: { imports: ImportsStatus }) {
  const { t } = useTranslation()
  return (
    <Card className="h-100">
      <Card.Body className="d-flex flex-column">
        <h2 className="kk-section-title mb-2">{t('system.imports.title')}</h2>
        <ImportRunLine source="photoprism" run={imports.photoprism} />
        <ImportRunLine source="photosorter" run={imports.photosorter} />
        <div className="mt-auto pt-2">
          <Link to="/import" className="btn btn-outline-primary btn-sm">
            {t('system.imports.trigger')}
          </Link>
        </div>
      </Card.Body>
    </Card>
  )
}

/** The backup-subsystem card with the trigger quick action. */
function BackupCard({
  backup,
  onTrigger,
  triggering,
}: {
  backup: BackupStatus
  onTrigger: () => void
  triggering: boolean
}) {
  const { t, i18n } = useTranslation()
  return (
    <Card className="h-100">
      <Card.Body className="d-flex flex-column">
        <h2 className="kk-section-title mb-2">{t('system.backup.title')}</h2>
        {!backup.configured ? (
          <Alert variant="secondary" className="mb-3">
            {t('system.backup.notConfigured')}
          </Alert>
        ) : (
          <div className="mb-3 small">
            <div className="mb-1">
              {backup.running ? (
                <Badge bg="info">{t('system.backup.running')}</Badge>
              ) : (
                <Badge bg="secondary">{t('system.backup.idle')}</Badge>
              )}
            </div>
            <div className="text-secondary">
              {t('system.backup.lastRun')}:{' '}
              {backup.last_finished_at ? (
                <>
                  {formatTimestamp(backup.last_finished_at, i18n.language)}{' '}
                  {backup.last_error ? (
                    <Badge bg="danger">{t('system.backup.failed')}</Badge>
                  ) : (
                    <Badge bg="success">{t('system.backup.success')}</Badge>
                  )}
                </>
              ) : (
                t('system.backup.never')
              )}
            </div>
          </div>
        )}
        <div className="mt-auto pt-2">
          <Button
            variant="outline-primary"
            size="sm"
            disabled={!backup.configured || backup.running || triggering}
            onClick={onTrigger}
          >
            {triggering && <Spinner animation="border" size="sm" role="status" className="me-2" />}
            {t('system.backup.trigger')}
          </Button>
        </div>
      </Card.Body>
    </Card>
  )
}

/** The storage-usage card. */
function StorageCard({ storage }: { storage: StorageStatus }) {
  const { t } = useTranslation()
  return (
    <Card className="h-100">
      <Card.Body>
        <h2 className="kk-section-title mb-2">{t('system.storage.title')}</h2>
        <dl className="row mb-0 small">
          <dt className="col-6 text-secondary fw-normal">{t('system.storage.originals')}</dt>
          <dd className="col-6 text-end mb-1">{formatBytes(storage.originals_bytes)}</dd>
          <dt className="col-6 text-secondary fw-normal">{t('system.storage.cache')}</dt>
          <dd className="col-6 text-end mb-1">{formatBytes(storage.cache_bytes)}</dd>
          <dt className="col-6 text-secondary fw-normal">{t('system.storage.free')}</dt>
          <dd className="col-6 text-end mb-1">{formatBytes(storage.free_bytes)}</dd>
          <dt className="col-6 text-secondary fw-normal">{t('system.storage.total')}</dt>
          <dd className="col-6 text-end mb-0">{formatBytes(storage.total_bytes)}</dd>
        </dl>
      </Card.Body>
    </Card>
  )
}

/** The remaining quick-action links (maintenance scan flow). */
function QuickActions() {
  const { t } = useTranslation()
  return (
    <Card className="mb-4">
      <Card.Body className="d-flex gap-2 flex-wrap align-items-center">
        <span className="fw-semibold me-2">{t('system.actions.title')}</span>
        <Link to="/maintenance" className="btn btn-outline-secondary btn-sm">
          {t('system.actions.maintenance')}
        </Link>
      </Card.Body>
    </Card>
  )
}

/**
 * Admin-only system-status dashboard: a single, auto-refreshing view of the
 * running instance's operational health — database and embeddings-sidecar
 * reachability, job-queue depth and dead-letter backlog, the backup subsystem,
 * the last import per source, on-disk storage usage, and the map provider's
 * state — with quick actions to requeue the dead-letter jobs, trigger a backup,
 * and jump to the import and maintenance flows. When the embeddings box is
 * offline, the queued embedding work is surfaced so it is clear the backlog
 * resumes once the box returns; when mapy.com is rejecting the API key, that is
 * called out here rather than left to show up as a grey map.
 */
export function SystemStatusPage() {
  const { t } = useTranslation()
  const { isAdmin } = useAuth()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [notice, setNotice] = useState<ActionNotice | null>(null)
  const [requeuing, setRequeuing] = useState(false)
  const [triggering, setTriggering] = useState(false)

  const refresh = useCallback(async (signal?: AbortSignal) => {
    const data = await fetchSystemStatus(signal)
    setState({ status: 'ready', data })
  }, [])

  useEffect(() => {
    if (!isAdmin) {
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
  }, [isAdmin, refresh])

  const handleRequeue = useCallback(async () => {
    setRequeuing(true)
    setNotice(null)
    try {
      const count = await requeueDeadLetterJobs()
      setNotice({ kind: 'success', message: t('system.jobs.requeued', { n: count }) })
      await refresh()
    } catch {
      setNotice({ kind: 'error', message: t('system.jobs.requeueError') })
    } finally {
      setRequeuing(false)
    }
  }, [refresh, t])

  const handleBackup = useCallback(async () => {
    setTriggering(true)
    setNotice(null)
    try {
      await triggerBackup()
      setNotice({ kind: 'success', message: t('system.backup.triggered') })
      await refresh()
    } catch (err) {
      const message =
        err instanceof ApiError && err.status === 409
          ? t('system.backup.triggerConflict')
          : t('system.backup.triggerError')
      setNotice({ kind: 'error', message })
    } finally {
      setTriggering(false)
    }
  }, [refresh, t])

  if (!isAdmin) {
    return <Alert variant="danger">{t('system.adminOnly')}</Alert>
  }

  return (
    <>
      <h1 className="kk-page-title mb-1">{t('system.title')}</h1>
      <p className="text-secondary">{t('system.subtitle')}</p>

      {notice && (
        <Alert
          variant={notice.kind === 'success' ? 'success' : 'danger'}
          dismissible
          onClose={() => {
            setNotice(null)
          }}
        >
          {notice.message}
        </Alert>
      )}

      {state.status === 'loading' && (
        <div className="d-flex justify-content-center py-5">
          <Spinner animation="border" role="status">
            <span className="visually-hidden">{t('system.loading')}</span>
          </Spinner>
        </div>
      )}

      {state.status === 'error' && (
        <ErrorState
          title={t('system.error')}
          onRetry={() => {
            void refresh()
          }}
        />
      )}

      {state.status === 'ready' && (
        <>
          <QuickActions />
          <Row className="g-3" xs={1} md={2} lg={3}>
            <Col>
              <DatabaseCard database={state.data.database} />
            </Col>
            <Col>
              <EmbeddingsCard
                embeddings={state.data.embeddings}
                pending={state.data.jobs.pending_embeddings}
              />
            </Col>
            <Col>
              <JobsCard
                jobs={state.data.jobs}
                onRequeue={() => {
                  void handleRequeue()
                }}
                requeuing={requeuing}
              />
            </Col>
            <Col>
              <BackupCard
                backup={state.data.backup}
                onTrigger={() => {
                  void handleBackup()
                }}
                triggering={triggering}
              />
            </Col>
            <Col>
              <ImportsCard imports={state.data.imports} />
            </Col>
            <Col>
              <StorageCard storage={state.data.storage} />
            </Col>
            <Col>
              <MapsCard maps={state.data.maps} />
            </Col>
            <Col>
              <VersionCard version={state.data.version} />
            </Col>
          </Row>
        </>
      )}
    </>
  )
}

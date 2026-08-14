import type { ParseKeys } from 'i18next'
import { useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Col from 'react-bootstrap/Col'
import Form from 'react-bootstrap/Form'
import Row from 'react-bootstrap/Row'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { ErrorState } from '../components/ErrorState'
import { JobQueuePanel } from '../components/system/JobQueuePanel'
import { LibraryOverview } from '../components/system/LibraryOverview'
import { RemainingWorkPanel } from '../components/system/RemainingWorkPanel'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { formatBytes, formatDateTime } from '../lib/format'
import { formatRelativeTime } from '../lib/relativeTime'
import {
  clearAnnouncement,
  fetchAnnouncement,
  setAnnouncement,
  type AnnouncementLevel,
} from '../services/announcement'
import { ApiError } from '../services/auth'
import type { ImportRun } from '../services/import'
import {
  fetchSystemStatus,
  requeueDeadLetterJobs,
  triggerBackup,
  type BackupStatus,
  type DatabaseStatus,
  type EmbeddingsStatus,
  type GeocodeStatus,
  type ImportsStatus,
  type MapsState,
  type MapsStatus,
  type StorageStatus,
  type SystemStatus,
  type VersionInfo,
} from '../services/system'

/** How often the status snapshot is re-polled while the page is open. */
const POLL_INTERVAL_MS = 5000

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
 * The reverse-geocode credit line inside the map card: how much of the current
 * budget window an import has spent and when it refills. Credits are metered
 * money, so the spend belongs where it can be watched while a run is happening,
 * not reconstructed from the bill afterwards. Renders nothing when no budget
 * caps the spend.
 */
function GeocodeCredits({ geocode }: { geocode: GeocodeStatus }) {
  const { t, i18n } = useTranslation()
  if (!geocode.budget_enabled) {
    return null
  }
  const exhausted = geocode.remaining === 0
  return (
    <div className="mt-3">
      <div className="text-secondary small">
        {t('system.geocode.credits')}:{' '}
        <span className={exhausted ? 'text-warning' : undefined}>
          {geocode.spent} / {geocode.limit}
        </span>
      </div>
      {geocode.resets_at !== undefined && (
        <div className="text-secondary small">
          {t(exhausted ? 'system.geocode.exhausted' : 'system.geocode.resetsAt')}:{' '}
          {formatTimestamp(geocode.resets_at, i18n.language)}
        </div>
      )}
    </div>
  )
}

/**
 * The map-provider card. A rejected mapy.com key is the failure that otherwise
 * hides — the map view just goes grey — so it is called out here, in red, with
 * what has to be done about it. The reverse-geocode credit budget rides along:
 * it is the same metered mapy.com account.
 */
function MapsCard({ maps, geocode }: { maps: MapsStatus; geocode: GeocodeStatus }) {
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
        <GeocodeCredits geocode={geocode} />
      </Card.Body>
    </Card>
  )
}

/**
 * How long ago something happened, as a muted line — "před 3 h" / "3h ago" — with
 * the exact stamp in the tooltip. The age is what a health readout is read for
 * ("is the backup recent?"), the timestamp is what it is checked with.
 */
function AgeLine({ at, labelKey }: { at: string; labelKey: ParseKeys }) {
  const { t, i18n } = useTranslation()
  return (
    <div className="text-secondary small" title={formatTimestamp(at, i18n.language)}>
      {t(labelKey)}: {formatRelativeTime(at, i18n.language)}
    </div>
  )
}

/** Renders the most recent folder-import run, or a "never" placeholder. */
function ImportRunLine({ run }: { run: ImportRun | null }) {
  const { t } = useTranslation()
  if (!run) {
    return (
      <div className="mb-2">
        <span className="fw-semibold">{t('import.source.folder')}</span>:{' '}
        <span className="text-secondary small">{t('system.imports.never')}</span>
      </div>
    )
  }
  const at = run.finished_at ?? run.started_at
  return (
    <div className="mb-2">
      <span className="fw-semibold">{t('import.source.folder')}</span>:{' '}
      <Badge bg={run.status === 'done' ? 'success' : run.status === 'failed' ? 'danger' : 'info'}>
        {t(`import.status.${run.status}`)}
      </Badge>{' '}
      <span className="text-secondary small">
        {t('import.counts.imported')} {run.counts.imported}
      </span>
      <AgeLine at={at} labelKey="system.imports.age" />
    </div>
  )
}

/**
 * The last-import card. It reports the folder import (`kukatko import dir`),
 * the only import that can still run — the PhotoPrism/photo-sorter migration
 * finished and was removed — and links to the full run history.
 */
function ImportsCard({ imports }: { imports: ImportsStatus }) {
  const { t } = useTranslation()
  return (
    <Card className="h-100">
      <Card.Body className="d-flex flex-column">
        <h2 className="kk-section-title mb-2">{t('system.imports.title')}</h2>
        <ImportRunLine run={imports.folder} />
        <div className="mt-auto pt-2">
          <Link to="/import" className="btn btn-outline-primary btn-sm">
            {t('system.imports.history')}
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
            {backup.last_finished_at !== undefined && (
              <AgeLine at={backup.last_finished_at} labelKey="system.backup.age" />
            )}
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

/**
 * The server's disk. Deliberately titled as the server's disk rather than as
 * "storage": on this instance the originals live in an object store, so the
 * originals directory measured here holds next to nothing while the library
 * itself weighs tens of gigabytes — which is what the catalogue's own storage
 * card in the Library section reports. Two different questions, two different
 * cards, each saying which one it answers.
 */
function ServerDiskCard({ storage }: { storage: StorageStatus }) {
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
        <p className="text-secondary kk-text-caption mt-3 mb-0">{t('system.storage.note')}</p>
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
 * The maintainer compose control for the instance-wide announcement banner. It
 * loads the current message on mount (so an existing announcement can be edited
 * in place), and lets a maintainer publish a new/updated message at an info or
 * warning level, or clear it for everyone. Feedback uses the same dismissible
 * {@link ActionNotice} `<Alert>` pattern as the page's other quick actions. It is
 * self-contained — it owns its own form and notice state — and is only rendered
 * inside the already maintainer-gated {@link SystemStatusPage}.
 */
function AnnouncementCard() {
  const { t } = useTranslation()
  const [message, setMessage] = useState('')
  const [level, setLevel] = useState<AnnouncementLevel>('info')
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState<ActionNotice | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    // Prefill from whatever is currently published; a failure just leaves the
    // form blank (the maintainer can still publish a fresh message).
    fetchAnnouncement(controller.signal)
      .then((current) => {
        setMessage(current.message)
        if (current.level !== undefined) {
          setLevel(current.level)
        }
      })
      .catch(() => undefined)
    return () => {
      controller.abort()
    }
  }, [])

  const handlePublish = useCallback(async () => {
    setBusy(true)
    setNotice(null)
    try {
      const saved = await setAnnouncement(message.trim(), level)
      setMessage(saved.message)
      if (saved.level !== undefined) {
        setLevel(saved.level)
      }
      setNotice({ kind: 'success', message: t('announcement.compose.published') })
    } catch {
      setNotice({ kind: 'error', message: t('announcement.compose.publishError') })
    } finally {
      setBusy(false)
    }
  }, [level, message, t])

  const handleClear = useCallback(async () => {
    setBusy(true)
    setNotice(null)
    try {
      await clearAnnouncement()
      setMessage('')
      setLevel('info')
      setNotice({ kind: 'success', message: t('announcement.compose.cleared') })
    } catch {
      setNotice({ kind: 'error', message: t('announcement.compose.clearError') })
    } finally {
      setBusy(false)
    }
  }, [t])

  return (
    <Card className="mb-4">
      <Card.Body>
        <h2 className="kk-section-title mb-1">{t('announcement.compose.title')}</h2>
        <p className="text-secondary small">{t('announcement.compose.intro')}</p>
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
        <Form.Group className="mb-3" controlId="announcement-message">
          <Form.Label>{t('announcement.compose.messageLabel')}</Form.Label>
          <Form.Control
            as="textarea"
            rows={2}
            value={message}
            placeholder={t('announcement.compose.messagePlaceholder')}
            disabled={busy}
            onChange={(event) => {
              setMessage(event.target.value)
            }}
          />
        </Form.Group>
        <Form.Group className="mb-3" controlId="announcement-level">
          <Form.Label>{t('announcement.compose.levelLabel')}</Form.Label>
          <Form.Select
            value={level}
            disabled={busy}
            onChange={(event) => {
              setLevel(event.target.value as AnnouncementLevel)
            }}
          >
            <option value="info">{t('announcement.compose.level.info')}</option>
            <option value="warning">{t('announcement.compose.level.warning')}</option>
          </Form.Select>
        </Form.Group>
        <div className="d-flex gap-2 flex-wrap">
          <Button
            variant="primary"
            size="sm"
            disabled={busy || message.trim() === ''}
            onClick={() => {
              void handlePublish()
            }}
          >
            {busy && <Spinner animation="border" size="sm" role="status" className="me-2" />}
            {t('announcement.compose.publish')}
          </Button>
          <Button
            variant="outline-secondary"
            size="sm"
            disabled={busy}
            onClick={() => {
              void handleClear()
            }}
          >
            {t('announcement.compose.clear')}
          </Button>
        </div>
      </Card.Body>
    </Card>
  )
}

/**
 * Admin-only dashboard of the running instance, auto-refreshing from one
 * snapshot, read top to bottom the way the questions are actually asked:
 *
 *  1. **Library** — what is in it, what arrived recently, what it weighs.
 *  2. **Remaining work** — the backlogs, each linking to the screen it is worked
 *     through on.
 *  3. **Job queue** — the background work broken down by type *and* state, with
 *     a per-type dead-letter requeue.
 *  4. **Health** — the operational cards: database and embeddings-sidecar
 *     reachability, the backup and last import with their ages, the map provider
 *     (a rejected mapy.com key shows up here rather than only as a grey map), the
 *     geocode credit budget, the server's disk and the build version.
 *  5. The announcement compose box, last: publishing a banner is a rare errand,
 *     not what the page is opened for, and it used to sit above everything.
 *
 * Every number comes from the single `GET /system/status` snapshot — the page
 * does no arithmetic of its own — so nothing here can drift from what the backend
 * counted.
 */
export function SystemStatusPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('system.title'))
  const { isMaintainer } = useAuth()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [notice, setNotice] = useState<ActionNotice | null>(null)
  // The requeue in flight: a job type, `''` for the whole dead letter, null for
  // none. One piece of state, because only one requeue may run at a time.
  const [requeuing, setRequeuing] = useState<string | null>(null)
  const [triggering, setTriggering] = useState(false)

  const refresh = useCallback(async (signal?: AbortSignal) => {
    const data = await fetchSystemStatus(signal)
    setState({ status: 'ready', data })
  }, [])

  useEffect(() => {
    if (!isMaintainer) {
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
  }, [isMaintainer, refresh])

  const handleRequeue = useCallback(
    async (jobType?: string) => {
      setRequeuing(jobType ?? '')
      setNotice(null)
      try {
        const count = await requeueDeadLetterJobs(jobType)
        setNotice({ kind: 'success', message: t('system.jobs.requeued', { n: count }) })
        await refresh()
      } catch {
        setNotice({ kind: 'error', message: t('system.jobs.requeueError') })
      } finally {
        setRequeuing(null)
      }
    },
    [refresh, t],
  )

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

  if (!isMaintainer) {
    return <Alert variant="danger">{t('system.maintainerOnly')}</Alert>
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
          <LibraryOverview library={state.data.library} />
          <RemainingWorkPanel remaining={state.data.remaining} />
          <JobQueuePanel
            jobs={state.data.jobs}
            onRequeue={(jobType) => {
              void handleRequeue(jobType)
            }}
            requeuing={requeuing}
          />
          <h2 className="kk-section-title mb-1">{t('system.dashboard.healthTitle')}</h2>
          <p className="text-secondary small">{t('system.dashboard.healthIntro')}</p>
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
              <ServerDiskCard storage={state.data.storage} />
            </Col>
            <Col>
              <MapsCard maps={state.data.maps} geocode={state.data.geocode} />
            </Col>
            <Col>
              <VersionCard version={state.data.version} />
            </Col>
          </Row>
          <QuickActions />
        </>
      )}

      <AnnouncementCard />
    </>
  )
}

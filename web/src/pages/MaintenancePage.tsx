import { useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Form from 'react-bootstrap/Form'
import Spinner from 'react-bootstrap/Spinner'
import Table from 'react-bootstrap/Table'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../auth/AuthContext'
import { ApiError } from '../services/auth'
import { fetchJobStats, type JobStats } from '../services/import'
import {
  fetchMaintenanceScan,
  runMaintenanceRepair,
  type Finding,
  type RepairOptions,
  type RepairResult,
  type ScanReport,
} from '../services/maintenance'

/** How often the background job-queue stats are re-polled while the page is open. */
const POLL_INTERVAL_MS = 3000

/**
 * The integrity-problem classes rendered in the scan-result table, in display
 * order. Each name is both the i18n suffix (`maintenance.findings.<key>`) and the
 * snake_case field of {@link ScanReport} holding its {@link Finding}.
 */
const FINDING_KEYS = [
  'missing_originals',
  'orphan_files',
  'missing_thumbnails',
  'missing_embeddings',
  'missing_faces',
  'missing_phashes',
] as const

/** A finding key, narrowing {@link ScanReport} access to the Finding fields only. */
type FindingKey = (typeof FINDING_KEYS)[number]

/**
 * The opt-in repairs rendered as checkboxes, in display order. Each name is both
 * the i18n suffix (`maintenance.repair.<key>`) and a boolean field of
 * {@link RepairOptions}.
 */
const REPAIR_KEYS = ['thumbnails', 'embeddings', 'faces', 'phashes', 'import_orphans'] as const

/** A repair key, used to index {@link RepairOptions} and toggle its selection. */
type RepairKey = (typeof REPAIR_KEYS)[number]

/** Returns the {@link Finding} for a finding key from the scan report. */
function findingOf(report: ScanReport, key: FindingKey): Finding {
  return report[key]
}

/** Maps each finding key to its RepairOptions flag, or null when none applies. */
const REPAIR_FOR_FINDING: Record<FindingKey, RepairKey | null> = {
  missing_originals: null,
  orphan_files: 'import_orphans',
  missing_thumbnails: 'thumbnails',
  missing_embeddings: 'embeddings',
  missing_faces: 'faces',
  missing_phashes: 'phashes',
}

/** Lifecycle of the integrity-scan request. */
type ScanState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; report: ScanReport }

/** Lifecycle of the repair request. */
type RepairState =
  | { status: 'idle' }
  | { status: 'running' }
  | { status: 'error' }
  | { status: 'done'; result: RepairResult }

/** The scan-result table: catalogue/disk totals and one row per problem class. */
function ScanResult({ report }: { report: ScanReport }) {
  const { t } = useTranslation()
  return (
    <>
      <p className="text-secondary">
        {t('maintenance.scan.summary', {
          photos: report.photos,
          files: report.files_in_db,
          disk: report.originals_on_disk,
        })}
      </p>
      {report.missing_originals.count === 0 &&
      report.orphan_files.count === 0 &&
      report.missing_thumbnails.count === 0 &&
      report.missing_embeddings.count === 0 &&
      report.missing_faces.count === 0 &&
      report.missing_phashes.count === 0 ? (
        <Alert variant="success">{t('maintenance.scan.clean')}</Alert>
      ) : (
        <Table striped hover responsive size="sm">
          <tbody>
            {FINDING_KEYS.map((key) => {
              const finding = findingOf(report, key)
              return (
                <tr key={key}>
                  <td className="fw-semibold">{t(`maintenance.findings.${key}`)}</td>
                  <td style={{ width: '6rem' }}>
                    <Badge bg={finding.count > 0 ? 'warning' : 'secondary'} text="dark">
                      {finding.count}
                    </Badge>
                  </td>
                  <td className="text-secondary small font-monospace text-break">
                    {finding.samples.length > 0 ? finding.samples.join(', ') : '—'}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </Table>
      )}
    </>
  )
}

/** Props for the repair form. */
interface RepairFormProps {
  report: ScanReport | null
  selection: Record<RepairKey, boolean>
  onToggle: (key: RepairKey) => void
  onRun: () => void
  state: RepairState
}

/** Reports whether at least one repair is selected. */
function anySelected(selection: Record<RepairKey, boolean>): boolean {
  return REPAIR_KEYS.some((key) => selection[key])
}

/**
 * The repair form: an opt-in checkbox per repair (annotated with the matching
 * outstanding count from the latest scan) and a run button. The button is disabled
 * until at least one repair is selected or while a repair request is in flight.
 */
function RepairForm({ report, selection, onToggle, onRun, state }: RepairFormProps) {
  const { t } = useTranslation()
  const running = state.status === 'running'
  return (
    <Card className="mb-4">
      <Card.Body>
        <h2 className="kk-section-title mb-1">{t('maintenance.repair.title')}</h2>
        <p className="text-secondary small">{t('maintenance.repair.hint')}</p>
        <Form>
          {REPAIR_KEYS.map((key) => {
            const finding = Object.entries(REPAIR_FOR_FINDING).find(([, r]) => r === key)?.[0] as
              | FindingKey
              | undefined
            const outstanding = report && finding ? findingOf(report, finding).count : null
            return (
              <Form.Check
                key={key}
                type="checkbox"
                id={`repair-${key}`}
                label={
                  outstanding !== null
                    ? `${t(`maintenance.repair.${key}`)} (${String(outstanding)})`
                    : t(`maintenance.repair.${key}`)
                }
                checked={selection[key]}
                onChange={() => {
                  onToggle(key)
                }}
              />
            )
          })}
        </Form>
        <div className="mt-3">
          <Button variant="primary" disabled={!anySelected(selection) || running} onClick={onRun}>
            {running && <Spinner animation="border" size="sm" role="status" className="me-2" />}
            {running ? t('maintenance.repair.running') : t('maintenance.repair.run')}
          </Button>
        </div>
        {!anySelected(selection) && (
          <p className="text-secondary small mt-2 mb-0">{t('maintenance.repair.none')}</p>
        )}
        {state.status === 'error' && (
          <Alert variant="danger" className="mt-3 mb-0">
            {t('maintenance.repair.error')}
          </Alert>
        )}
        {state.status === 'done' && (
          <Alert variant="success" className="mt-3 mb-0">
            {t('maintenance.repair.result', {
              thumbnails: state.result.thumbnails_enqueued,
              phashes: state.result.phashes_enqueued,
              embeddings: state.result.embeddings_enqueued,
              faces: state.result.faces_enqueued,
              imported: state.result.orphans_imported,
              skipped: state.result.orphans_skipped,
              failed: state.result.orphans_failed,
            })}
          </Alert>
        )}
      </Card.Body>
    </Card>
  )
}

/** The background job-queue stats summary (repair progress). */
function JobStatsBar({ stats }: { stats: JobStats }) {
  const { t } = useTranslation()
  return (
    <Card className="mb-4">
      <Card.Body>
        <h2 className="kk-section-title mb-2">{t('maintenance.jobs.title')}</h2>
        <div className="d-flex gap-2 flex-wrap">
          <Badge bg="primary">
            {t('maintenance.jobs.total')}: {stats.total}
          </Badge>
          <Badge bg="secondary">
            {t('maintenance.jobs.queued')}: {stats.by_state.queued ?? 0}
          </Badge>
          <Badge bg="info">
            {t('maintenance.jobs.running')}: {stats.by_state.running ?? 0}
          </Badge>
          <Badge bg="warning" text="dark">
            {t('maintenance.jobs.failed')}: {stats.by_state.failed ?? 0}
          </Badge>
          <Badge bg="dark">
            {t('maintenance.jobs.dead')}: {stats.by_state.dead ?? 0}
          </Badge>
        </div>
      </Card.Body>
    </Card>
  )
}

/** The empty selection, used to initialise and reset the repair checkboxes. */
function emptySelection(): Record<RepairKey, boolean> {
  return {
    thumbnails: false,
    embeddings: false,
    faces: false,
    phashes: false,
    import_orphans: false,
  }
}

/**
 * Admin-only library-maintenance console: runs an integrity scan that reports
 * catalogue/disk drift (missing originals, orphan files, missing thumbnails,
 * embeddings, faces and pHashes) with counts and samples, and triggers the opt-in
 * repairs. Repairs run in the background through the job queue, so the page polls
 * the queue stats to show progress. Every action is admin-only, safe and
 * idempotent; originals are never deleted.
 */
export function MaintenancePage() {
  const { t } = useTranslation()
  const { isAdmin } = useAuth()
  const [scan, setScan] = useState<ScanState>({ status: 'idle' })
  const [repair, setRepair] = useState<RepairState>({ status: 'idle' })
  const [selection, setSelection] = useState<Record<RepairKey, boolean>>(emptySelection)
  const [jobStats, setJobStats] = useState<JobStats | null>(null)

  useEffect(() => {
    if (!isAdmin) {
      return
    }
    let cancelled = false
    const poll = () => {
      fetchJobStats()
        .then((stats) => {
          if (!cancelled) {
            setJobStats(stats)
          }
        })
        .catch(() => undefined)
    }
    poll()
    const id = window.setInterval(poll, POLL_INTERVAL_MS)
    return () => {
      cancelled = true
      window.clearInterval(id)
    }
  }, [isAdmin])

  const handleScan = useCallback(async () => {
    setScan({ status: 'loading' })
    try {
      const report = await fetchMaintenanceScan()
      setScan({ status: 'ready', report })
    } catch {
      setScan({ status: 'error' })
    }
  }, [])

  const handleRepair = useCallback(async () => {
    const options: RepairOptions = {}
    for (const key of REPAIR_KEYS) {
      if (selection[key]) {
        options[key] = true
      }
    }
    setRepair({ status: 'running' })
    try {
      const result = await runMaintenanceRepair(options)
      setRepair({ status: 'done', result })
      // A repair changes the outstanding counts; refresh the scan if we have one.
      if (scan.status === 'ready') {
        await handleScan()
      }
    } catch (err) {
      // 503 (orphan import not configured) and any other failure surface the same
      // generic repair error; the selection stays so the admin can retry.
      void (err instanceof ApiError)
      setRepair({ status: 'error' })
    }
  }, [selection, scan.status, handleScan])

  const toggle = useCallback((key: RepairKey) => {
    setSelection((prev) => ({ ...prev, [key]: !prev[key] }))
  }, [])

  if (!isAdmin) {
    return <Alert variant="danger">{t('maintenance.adminOnly')}</Alert>
  }

  return (
    <>
      <h1 className="kk-page-title mb-1">{t('maintenance.title')}</h1>
      <p className="text-secondary">{t('maintenance.subtitle')}</p>

      <Card className="mb-4">
        <Card.Body>
          <div className="d-flex justify-content-between align-items-center mb-3">
            <h2 className="kk-section-title mb-0">{t('maintenance.scan.title')}</h2>
            <Button
              variant="outline-primary"
              disabled={scan.status === 'loading'}
              onClick={() => {
                void handleScan()
              }}
            >
              {scan.status === 'loading' && (
                <Spinner animation="border" size="sm" role="status" className="me-2" />
              )}
              {scan.status === 'loading'
                ? t('maintenance.scan.running')
                : t('maintenance.scan.run')}
            </Button>
          </div>
          {scan.status === 'idle' && (
            <p className="text-secondary mb-0">{t('maintenance.scan.empty')}</p>
          )}
          {scan.status === 'error' && (
            <Alert variant="danger" className="mb-0">
              {t('maintenance.scan.error')}
            </Alert>
          )}
          {scan.status === 'ready' && <ScanResult report={scan.report} />}
        </Card.Body>
      </Card>

      <RepairForm
        report={scan.status === 'ready' ? scan.report : null}
        selection={selection}
        onToggle={toggle}
        onRun={() => {
          void handleRepair()
        }}
        state={repair}
      />

      {jobStats && <JobStatsBar stats={jobStats} />}
    </>
  )
}

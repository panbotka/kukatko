import { useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import ProgressBar from 'react-bootstrap/ProgressBar'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { useRekeyRun } from '../../hooks/useRekeyRun'
import { rekeyEtaSeconds, rekeyPercent, truncateMiddle } from '../../lib/rekeyProgress'
import { etaPhrase } from '../../lib/uploadEta'
import { formatBytes, formatCount, formatDateTime } from '../../lib/format'
import {
  pauseRekey,
  type RekeyRun,
  type RekeyState,
  RekeyApiError,
  resumeRekey,
  retryRekey,
  startRekey,
} from '../../services/rekey'
import type { AdminProject } from '../../services/projects'
import { ConfirmModal } from '../ConfirmModal'
import { ErrorState } from '../ErrorState'
import { Icon } from '../Icon'

/** Props of {@link StorageMoveCard}. */
export interface StorageMoveCardProps {
  project: AdminProject
}

/** The longest last key shown before its middle is cut out. */
const LAST_KEY_CHARS = 48

/** The state badge's Bootstrap variant. */
const STATE_VARIANT: Record<RekeyState, string> = {
  running: 'primary',
  paused: 'warning',
  done: 'success',
  failed: 'danger',
}

/** The steering actions a run offers, by the state they start from. */
type RekeyAction = 'pause' | 'resume' | 'retry'

const ACTIONS: Record<RekeyAction, (uid: string) => Promise<RekeyRun>> = {
  pause: pauseRekey,
  resume: resumeRekey,
  retry: retryRekey,
}

/**
 * The „Úložiště" card of `/system/projects/:slug`: where the project's objects
 * live and, for a project still in the root of the store, its move under
 * `projects/<uid>/` (`internal/rekeyapi`).
 *
 * Four faces: a project in the root with no run gets the safety promise and
 * „Spustit přesun" (behind a confirmation); a running, paused or failed run gets
 * the progress — the bar, photos and bytes, the rate the browser measures
 * between two polls and the ETA it implies, the state, the timestamps, the last
 * key and the errors — with pause / resume / retry; a done run gets its new
 * prefix and finish time. A project that never lived in the root just shows its
 * prefix. {@link useRekeyRun} keeps it current.
 */
export function StorageMoveCard({ project }: StorageMoveCardProps) {
  const { t, i18n } = useTranslation()
  const locale = i18n.language
  const { status, run, ratePerMinute, adopt, reload } = useRekeyRun(project.uid)

  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState<string | null>(null)
  const [errorsOpen, setErrorsOpen] = useState(false)

  async function handleStart() {
    setBusy(true)
    setNotice(null)
    try {
      adopt(await startRekey(project.uid))
    } catch (error: unknown) {
      const code = error instanceof RekeyApiError ? error.code : ''
      if (code === 'already_running') {
        // Somebody (the CLI, another tab) was first: show their run.
        reload()
      }
      setNotice(
        code === 'already_running'
          ? t('systemProject.storageMove.alreadyRunning')
          : code === 'already_prefixed'
            ? t('systemProject.storageMove.alreadyPrefixed')
            : t('systemProject.storageMove.actionFailed'),
      )
    } finally {
      setBusy(false)
      setConfirming(false)
    }
  }

  async function handleAction(action: RekeyAction) {
    setBusy(true)
    setNotice(null)
    try {
      adopt(await ACTIONS[action](project.uid))
    } catch (error: unknown) {
      const stale = error instanceof RekeyApiError && error.code === 'invalid_state'
      if (stale) {
        reload()
      }
      setNotice(
        stale
          ? t('systemProject.storageMove.stateChanged')
          : t('systemProject.storageMove.actionFailed'),
      )
    } finally {
      setBusy(false)
    }
  }

  /** The remaining time in words, or the placeholder while it is unknown. */
  function etaText(current: RekeyRun): string {
    const seconds = rekeyEtaSeconds(current, ratePerMinute)
    if (seconds === null) {
      return t('systemProject.storageMove.eta.calculating')
    }
    const phrase = etaPhrase(seconds)
    switch (phrase.unit) {
      case 'lessThanMinute':
        return t('systemProject.storageMove.eta.lessThanMinute')
      case 'minutes':
        return t('systemProject.storageMove.eta.minutes', { n: phrase.count })
      case 'hours':
        return t('systemProject.storageMove.eta.hours', { count: phrase.count })
    }
  }

  function renderPrefix(prefix: string) {
    return (
      <dl className="row mb-3">
        <dt className="col-6 fw-normal text-secondary">{t('systemProject.storagePrefix')}</dt>
        <dd className="col-6 mb-0">
          {prefix === '' ? (
            t('systemProject.storageRoot')
          ) : (
            <span className="font-monospace">{prefix}</span>
          )}
        </dd>
      </dl>
    )
  }

  function renderErrors(current: RekeyRun) {
    if (current.error_count === 0) {
      return null
    }
    // Newest first: the one that stopped the run is the one to read.
    const recent = [...(current.errors ?? [])].reverse()
    return (
      <div className="mb-3">
        <Button
          variant="link"
          className="p-0 text-danger"
          aria-expanded={errorsOpen}
          aria-controls="storage-move-errors"
          onClick={() => {
            setErrorsOpen((open) => !open)
          }}
        >
          <Icon name={errorsOpen ? 'chevron-down' : 'chevron-right'} className="me-1" />
          {t('systemProject.storageMove.errorCount', { count: current.error_count })}
        </Button>
        {errorsOpen && (
          <ul id="storage-move-errors" className="list-unstyled small mt-2 mb-0">
            {recent.map((item, index) => (
              <li key={`${item.at}-${String(index)}`} className="mb-2">
                <div className="font-monospace text-break">{item.key}</div>
                <div>{item.message}</div>
                <div className="text-secondary">{formatDateTime(item.at, locale)}</div>
              </li>
            ))}
          </ul>
        )}
      </div>
    )
  }

  /**
   * The warning about photos whose thumbnails or segments did not move yet,
   * with what will move them: the running pass itself, „Zkusit znovu" on a
   * failed run, or the maintenance repair once the run is over.
   */
  function renderDerivedFailed(current: RekeyRun) {
    if (current.derived_failed <= 0) {
      return null
    }
    const next =
      current.state === 'failed'
        ? t('systemProject.storageMove.derivedRetry')
        : current.state === 'done'
          ? t('systemProject.storageMove.derivedRepair')
          : t('systemProject.storageMove.derivedRetrying')
    return (
      <Alert variant="warning" className="mb-3" data-testid="storage-move-derived-failed">
        <Icon name="exclamation-triangle" className="me-2" />
        {t('systemProject.storageMove.derivedFailed', { count: current.derived_failed })} {next}
      </Alert>
    )
  }

  function renderDone(current: RekeyRun) {
    return (
      <>
        <Alert variant="success" className="mb-3">
          <Icon name="check-circle-fill" className="me-2" />
          {t('systemProject.storageMove.doneSummary', {
            prefix: current.target_prefix,
            at: formatDateTime(current.finished_at ?? current.updated_at, locale),
          })}
        </Alert>
        {renderPrefix(current.target_prefix)}
        {renderDerivedFailed(current)}
        {renderErrors(current)}
      </>
    )
  }

  function renderProgress(current: RekeyRun) {
    const percent = rekeyPercent(current)
    const percentText = t('systemProject.storageMove.percent', { percent })
    return (
      <>
        {renderPrefix(current.target_prefix)}
        {current.state === 'failed' && (
          <Alert variant="danger" className="mb-3">
            {t('systemProject.storageMove.failedText')}
          </Alert>
        )}
        {renderDerivedFailed(current)}
        <ProgressBar className="mb-2">
          <ProgressBar
            now={percent}
            variant={current.state === 'failed' ? 'danger' : undefined}
            striped={current.state === 'running'}
            animated={current.state === 'running'}
            aria-label={t('systemProject.storageMove.barLabel')}
            aria-valuetext={percentText}
          />
        </ProgressBar>
        <p className="mb-1" data-testid="storage-move-counts">
          {t('systemProject.storageMove.counts', {
            done: formatCount(current.done_photos, locale),
            total: formatCount(current.total_photos, locale),
            doneBytes: formatBytes(current.done_bytes, locale),
            totalBytes: formatBytes(current.total_bytes, locale),
            percent: percentText,
          })}
        </p>
        {current.state === 'running' && (
          <p className="mb-3 text-secondary" data-testid="storage-move-rate">
            {ratePerMinute === null
              ? t('systemProject.storageMove.rateUnknown')
              : t('systemProject.storageMove.rate', {
                  rate: formatCount(Math.round(ratePerMinute), locale),
                })}
            {' · '}
            {etaText(current)}
          </p>
        )}
        <dl className="row mb-3 mt-3">
          <dt className="col-6 fw-normal text-secondary">
            {t('systemProject.storageMove.startedAt')}
          </dt>
          <dd className="col-6">{formatDateTime(current.started_at, locale)}</dd>
          <dt className="col-6 fw-normal text-secondary">
            {t('systemProject.storageMove.updatedAt')}
          </dt>
          <dd className="col-6">{formatDateTime(current.updated_at, locale)}</dd>
          {current.last_key !== '' && (
            <>
              <dt className="col-6 fw-normal text-secondary">
                {t('systemProject.storageMove.lastKey')}
              </dt>
              <dd className="col-6 mb-0 font-monospace text-break" title={current.last_key}>
                {truncateMiddle(current.last_key, LAST_KEY_CHARS)}
              </dd>
            </>
          )}
        </dl>
        {renderErrors(current)}
        <div className="d-flex flex-wrap gap-2">
          {current.state === 'running' && (
            <Button
              variant="outline-secondary"
              disabled={busy}
              onClick={() => {
                void handleAction('pause')
              }}
            >
              <Icon name="pause-fill" className="me-1" />
              {t('systemProject.storageMove.pause')}
            </Button>
          )}
          {current.state === 'paused' && (
            <Button
              variant="primary"
              disabled={busy}
              onClick={() => {
                void handleAction('resume')
              }}
            >
              <Icon name="play-fill" className="me-1" />
              {t('systemProject.storageMove.resume')}
            </Button>
          )}
          {current.state === 'failed' && (
            <Button
              variant="primary"
              disabled={busy}
              onClick={() => {
                void handleAction('retry')
              }}
            >
              <Icon name="arrow-clockwise" className="me-1" />
              {t('systemProject.storageMove.retry')}
            </Button>
          )}
        </div>
      </>
    )
  }

  function renderNoRun() {
    if (project.storage_prefix !== '') {
      // A project founded with its own prefix: nothing to move, ever.
      return renderPrefix(project.storage_prefix)
    }
    return (
      <>
        {renderPrefix('')}
        <p>{t('systemProject.storageMove.rootText')}</p>
        <Button
          variant="primary"
          disabled={busy}
          onClick={() => {
            setConfirming(true)
          }}
        >
          <Icon name="box-arrow-right" className="me-1" />
          {t('systemProject.storageMove.start')}
        </Button>
      </>
    )
  }

  function renderBody() {
    if (status === 'loading') {
      return (
        <div className="d-flex justify-content-center py-3">
          <Spinner animation="border" size="sm" role="status">
            <span className="visually-hidden">{t('systemProject.storageMove.loading')}</span>
          </Spinner>
        </div>
      )
    }
    if (status === 'error') {
      return <ErrorState title={t('systemProject.storageMove.loadFailed')} onRetry={reload} />
    }
    if (run === null) {
      return renderNoRun()
    }
    return run.state === 'done' ? renderDone(run) : renderProgress(run)
  }

  return (
    <Card className="mb-4" data-testid="project-storage">
      <Card.Body>
        <div className="d-flex justify-content-between align-items-start gap-2 mb-3">
          <h2 className="kk-section-title mb-0">{t('systemProject.storageTitle')}</h2>
          {status === 'ready' && run !== null && (
            <Badge bg={STATE_VARIANT[run.state]} data-testid="storage-move-state">
              {t(`systemProject.storageMove.state.${run.state}`)}
            </Badge>
          )}
        </div>
        {notice !== null && (
          <Alert
            variant="warning"
            role="alert"
            dismissible
            onClose={() => {
              setNotice(null)
            }}
          >
            {notice}
          </Alert>
        )}
        {renderBody()}
      </Card.Body>

      <ConfirmModal
        show={confirming}
        title={t('systemProject.storageMove.confirmTitle', { name: project.name })}
        confirmLabel={t('systemProject.storageMove.start')}
        variant="primary"
        busy={busy}
        onConfirm={() => {
          void handleStart()
        }}
        onCancel={() => {
          setConfirming(false)
        }}
      >
        {t('systemProject.storageMove.confirmBody', { prefix: `projects/${project.uid}/` })}
      </ConfirmModal>
    </Card>
  )
}

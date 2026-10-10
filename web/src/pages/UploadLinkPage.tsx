import { type ReactNode, useCallback, useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Col from 'react-bootstrap/Col'
import Form from 'react-bootstrap/Form'
import ProgressBar from 'react-bootstrap/ProgressBar'
import Row from 'react-bootstrap/Row'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link, useParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { ConfirmModal } from '../components/ConfirmModal'
import { ENTITY_STYLE, type EntityKind } from '../components/entityStyle'
import { Icon } from '../components/Icon'
import { DropZone } from '../components/upload/DropZone'
import { PickFilesButton } from '../components/upload/PickFilesButton'
import { UploadQueuePanel } from '../components/upload/UploadQueuePanel'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useLeaveGuard } from '../hooks/useLeaveGuard'
import { usePasteFiles } from '../hooks/usePasteFiles'
import { usePublicSettings } from '../hooks/usePublicSettings'
import { registrationOpenFrom } from '../hooks/useRegistrationOpen'
import { useOnline } from '../hooks/useOnline'
import { useUploadEta } from '../hooks/useUploadEta'
import { type UploadQueueItem, useUploadQueue } from '../hooks/useUploadQueue'
import { useUploadResilience } from '../hooks/useUploadResilience'
import { canRetryUpload } from '../lib/uploadErrors'
import { etaPhrase } from '../lib/uploadEta'
import { forgetBatch, readInterruptedBatch, rememberBatch } from '../lib/uploadLinkBatch'
import { UPLOADER_NAME_KEY, uploadLinkSummary } from '../lib/uploadLinks'
import { ApiError, type User } from '../services/auth'
import {
  fetchPublicUploadLink,
  linkUploader,
  type PublicUploadLink,
  UploadLinkGoneError,
} from '../services/uploadLinks'

/** Where loading the link's public description stands. */
type LoadState =
  | { status: 'loading' }
  | { status: 'ready'; link: PublicUploadLink }
  | { status: 'notFound' }
  | { status: 'gone'; state: 'expired' | 'revoked' }
  | { status: 'error' }

/** Reads the remembered uploader name; storage may be unavailable (private mode). */
function readStoredName(): string {
  try {
    return localStorage.getItem(UPLOADER_NAME_KEY) ?? ''
  } catch {
    return ''
  }
}

/** Remembers the uploader name on this device, forgetting it when cleared. */
function storeName(name: string): void {
  try {
    if (name.trim() === '') {
      localStorage.removeItem(UPLOADER_NAME_KEY)
    } else {
      localStorage.setItem(UPLOADER_NAME_KEY, name)
    }
  } catch {
    // Storage refused (private mode, quota): the name simply is not remembered.
  }
}

/** The name a signed-in uploader is greeted by: their display name, else username. */
function accountName(user: User | null): string {
  if (user === null) {
    return ''
  }
  return user.display_name !== '' ? user.display_name : user.username
}

/** Maps a failed description fetch onto the state the page shows. */
function failureState(error: unknown): LoadState {
  if (error instanceof UploadLinkGoneError) {
    return { status: 'gone', state: error.state }
  }
  if (error instanceof ApiError && error.status === 404) {
    return { status: 'notFound' }
  }
  return { status: 'error' }
}

/** A non-clickable album/label chip: an uploader may not open the album. */
function TargetChip({ kind, children }: { kind: EntityKind; children: ReactNode }) {
  const style = ENTITY_STYLE[kind]
  return (
    <span
      className={`badge rounded-pill ${style.className} d-inline-flex align-items-center gap-1`}
    >
      <Icon name={style.icon} />
      {children}
    </span>
  )
}

/** The three things the page can be once the link is loaded. */
type Phase = 'pick' | 'uploading' | 'done'

/**
 * The public page behind an upload link (`/u/:code`). Anybody holding the link
 * — signed in or not — sees the curator's title and note, exactly where the
 * photos go (album and label names as chips) and until when the link is valid,
 * and nothing else about the library.
 *
 * Almost everyone opens it on a phone, often someone who has never seen the
 * app, so it is three plain screens with one thing to do on each:
 *
 * - **pick** — the optional "Od koho?" (remembered on the device) and one large
 *   "Vybrat fotky" button; the drop zone is there only from the `md` width up;
 * - **uploading** — nothing but the overall progress (bytes, percentage,
 *   "X z Y fotek", a smoothed remaining time) and "keep the page open"; no
 *   control that could cancel the batch, no link that could leave it. The
 *   per-file list waits closed behind "Podrobnosti", without Remove until
 *   something fails. A dropped connection resumes by itself
 *   (`useUploadResilience`), offline pauses the queue, the screen is kept awake;
 * - **done** — a big check mark, "Hotovo, nahráno N fotek", "Nahrát další" and,
 *   for an anonymous uploader on an instance with open registration, the offer
 *   to register through the same link (no secret needed, the photos just sent
 *   are claimed).
 *
 * A reload loses the picked files; a batch still running is remembered per
 * device (`lib/uploadLinkBatch`), so the next visit can say kindly what happened.
 */
export function UploadLinkPage() {
  const { t, i18n } = useTranslation()
  const { code = '' } = useParams()
  const { status: authStatus, user } = useAuth()
  const registration = registrationOpenFrom(usePublicSettings())
  const [load, setLoad] = useState<LoadState>({ status: 'loading' })
  const [name, setName] = useState(readStoredName)
  const [lostBatch, setLostBatch] = useState(() => readInterruptedBatch(code, Date.now()))

  const title =
    load.status === 'ready' && load.link.title !== '' ? load.link.title : t('uploadLink.title')
  useDocumentTitle(title)

  useEffect(() => {
    const controller = new AbortController()
    fetchPublicUploadLink(code, controller.signal)
      .then((link) => {
        setLoad({ status: 'ready', link })
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) {
          return
        }
        setLoad(failureState(error))
      })
    return () => {
      controller.abort()
    }
  }, [code])

  const online = useOnline()
  // Each file reads the name when it starts, so a name typed mid-batch applies
  // to the files still waiting. Offline, nothing starts: the waiting files would
  // only fail one after another.
  const queue = useUploadQueue(
    linkUploader(code, () => name),
    { paused: !online },
  )
  const { items, summary, isComplete } = queue
  const queueAdd = queue.addFiles
  // Picking files starts a new batch, so the note about the lost one has done its job.
  const addFiles = useCallback(
    (files: FileList | File[]) => {
      setLostBatch(null)
      queueAdd(files)
    },
    [queueAdd],
  )
  // The drop zone promises Ctrl+V; addFiles is stable, as the hook requires.
  usePasteFiles(addFiles)
  const signedIn = authStatus === 'authenticated'

  // A file whose connection dropped is about to go again by itself, so it keeps
  // the batch in its running state rather than ending it with an error.
  const interrupted = items.some((item) => item.status === 'error' && item.interrupted === true)
  const phase: Phase =
    items.length === 0 ? 'pick' : !isComplete || interrupted ? 'uploading' : 'done'
  const running = phase === 'uploading'
  // A failure a person has to look at (not one that resumes by itself).
  const hardFailure = items.some((item) => item.status === 'error' && item.interrupted !== true)

  const leaving = useLeaveGuard(running)
  useUploadResilience({
    active: running,
    interrupted,
    online,
    onResume: queue.retryInterrupted,
  })
  const eta = useUploadEta(items, running && online)

  // Remember a running batch on the device, so a reload can explain itself.
  useEffect(() => {
    if (phase === 'uploading') {
      rememberBatch(code, summary.total, Date.now())
    } else if (phase === 'done') {
      forgetBatch()
    }
  }, [code, phase, summary.total])

  // A link that went dead mid-batch answers 410: say so instead of a wall of
  // per-file failures nobody can fix.
  const linkDied = items.some(
    (item) => item.status === 'error' && item.error?.includes('no longer'),
  )
  // A file refused for what it is (not a photo at all) fails the same way
  // again: retry is offered only while some failure might go differently.
  const retryable = items.some(canRetryUpload)

  function changeName(value: string) {
    setName(value)
    storeName(value)
  }

  // The panel's badges count a file that is about to go again as waiting, not
  // as failed: a red "1 selhalo" is the scary error the reconnect line replaces.
  const waitingAgain = countInterrupted(items)
  const panelSummary = useMemo(
    () => ({
      ...summary,
      error: summary.error - waitingAgain,
      queued: summary.queued + waitingAgain,
    }),
    [summary, waitingAgain],
  )

  const details = (
    <div className="mt-3">
      <UploadQueuePanel
        items={items}
        summary={panelSummary}
        // No Remove while the batch runs — one stray tap would cancel a file —
        // unless something failed and may need taking out.
        onRemove={running && !hardFailure ? undefined : queue.removeItem}
        onRetry={queue.retry}
        toggleLabel={t('uploadLink.details')}
        autoOpen={hardFailure}
      />
    </div>
  )

  return (
    <Row className="justify-content-center kk-upload-link">
      <Col xs={12} sm={11} md={9} lg={7} xl={6}>
        <Card text="light" className="mt-3 mt-md-5" data-testid="upload-link-page">
          <Card.Body>
            {load.status === 'loading' && (
              <div className="text-center py-4">
                <Spinner animation="border" role="status">
                  <span className="visually-hidden">{t('uploadLink.loading')}</span>
                </Spinner>
              </div>
            )}

            {load.status !== 'loading' && load.status !== 'ready' && <Unavailable load={load} />}

            {load.status === 'ready' && phase === 'pick' && (
              <>
                <h1 className="kk-page-title mb-2">{title}</h1>
                {load.link.note !== '' && (
                  <p className="mb-3" style={{ whiteSpace: 'pre-line' }}>
                    {load.link.note}
                  </p>
                )}

                <section aria-labelledby="upload-link-targets" className="mb-3">
                  <h2 id="upload-link-targets" className="h6 text-secondary mb-2">
                    {t('uploadLink.targets')}
                  </h2>
                  <div className="d-flex flex-wrap gap-2">
                    {load.link.albums.map((album) => (
                      <TargetChip key={`a-${album}`} kind="album">
                        {album}
                      </TargetChip>
                    ))}
                    {load.link.labels.map((label) => (
                      <TargetChip key={`l-${label}`} kind="tag">
                        {label}
                      </TargetChip>
                    ))}
                  </div>
                  <p className="small text-secondary mt-2 mb-0">
                    <Icon name="hourglass-split" className="me-1" />
                    {t('uploadLink.validUntil', {
                      date: new Date(load.link.expires_at).toLocaleDateString(i18n.language),
                    })}
                  </p>
                </section>

                {lostBatch !== null && (
                  <Alert variant="info" role="status" data-testid="upload-link-lost-batch">
                    <Icon name="info-circle" className="me-2" />
                    {t('uploadLink.interruptedBatch', { count: lostBatch.count })}
                  </Alert>
                )}

                {signedIn ? (
                  <p className="text-secondary" data-testid="upload-link-signed-in">
                    <Icon name="person-circle" className="me-1" />
                    {t('uploadLink.signedInAs', { name: accountName(user) })}
                  </p>
                ) : (
                  <Form.Group controlId="upload-link-name" className="mb-3">
                    <Form.Label>{t('uploadLink.name')}</Form.Label>
                    <Form.Control
                      type="text"
                      size="lg"
                      autoComplete="name"
                      maxLength={100}
                      value={name}
                      placeholder={t('uploadLink.namePlaceholder')}
                      onChange={(event) => {
                        changeName(event.target.value)
                      }}
                    />
                    <Form.Text className="text-secondary">{t('uploadLink.nameHint')}</Form.Text>
                  </Form.Group>
                )}

                <div className="d-grid">
                  <PickFilesButton
                    onFiles={addFiles}
                    label={t('uploadLink.choose')}
                    inputLabel={t('upload.pick.ariaInput')}
                    className="kk-upload-link__primary"
                  />
                </div>
                {/* A phone has nothing to drag: the drop zone is a desktop extra. */}
                <div className="d-none d-md-block mt-3">
                  <DropZone onFiles={addFiles} />
                </div>
              </>
            )}

            {load.status === 'ready' && phase === 'uploading' && (
              <div data-testid="upload-link-uploading">
                <div className="kk-upload-link__progress" data-testid="upload-link-progress">
                  <h1 className="kk-section-title mb-3">{t('uploadLink.uploadingTitle')}</h1>
                  <BatchProgress
                    fraction={eta.fraction}
                    done={summary.created + summary.duplicate + panelSummary.error}
                    total={summary.total}
                    seconds={online ? eta.seconds : null}
                  />
                  {!online && (
                    <Alert
                      variant="warning"
                      role="status"
                      className="mt-3 mb-0"
                      data-testid="upload-link-offline"
                    >
                      <Icon name="wifi-off" className="me-2" />
                      {t('uploadLink.offline')}
                    </Alert>
                  )}
                  {online && interrupted && (
                    <Alert
                      variant="info"
                      role="status"
                      className="mt-3 mb-0"
                      data-testid="upload-link-reconnecting"
                    >
                      <Icon name="arrow-repeat" className="me-2" />
                      {t('uploadLink.reconnecting')}
                    </Alert>
                  )}
                  <p className="mt-3 mb-0 fw-semibold">
                    <Icon name="phone" className="me-2" />
                    {t('uploadLink.keepOpen')}
                  </p>
                </div>
                {details}
              </div>
            )}

            {load.status === 'ready' && phase === 'done' && (
              <div data-testid="upload-link-done">
                <div className="text-center">
                  <Icon
                    name={summary.error > 0 ? 'exclamation-triangle' : 'check-circle-fill'}
                    className={`kk-upload-link__mark ${summary.error > 0 ? 'text-warning' : 'text-success'}`}
                  />
                  <h1 className="kk-page-title mt-2 mb-3">
                    {summary.error > 0
                      ? t('uploadLink.done.partial', {
                          done: summary.created + summary.duplicate,
                          count: summary.total,
                        })
                      : t('uploadLink.done.title', { count: summary.total })}
                  </h1>
                </div>
                {/* The breakdown only when it says more than the heading. */}
                {(summary.duplicate > 0 || summary.error > 0) && (
                  <Alert
                    variant={summary.error > 0 ? 'warning' : 'success'}
                    role="status"
                    className="mb-3"
                    data-testid="upload-link-summary"
                  >
                    {uploadLinkSummary(summary, t)}
                  </Alert>
                )}
                {linkDied && (
                  <Alert variant="danger" role="alert">
                    {t('uploadLink.diedMidway')}
                  </Alert>
                )}
                <div className="d-grid gap-2">
                  {/* Outside the alert: a button on the filled warning
                      background has no contrast on this theme. */}
                  {retryable && (
                    <Button variant="outline-warning" size="lg" onClick={queue.retryFailed}>
                      <Icon name="arrow-clockwise" className="me-1" />
                      {t('upload.actions.retryFailed')}
                    </Button>
                  )}
                  <Button
                    variant="primary"
                    size="lg"
                    className="kk-upload-link__primary"
                    onClick={queue.clear}
                  >
                    <Icon name="plus-lg" className="me-2" />
                    {t('uploadLink.uploadMore')}
                  </Button>
                </div>
                {details}

                {!signedIn && summary.created > 0 && registration === 'open' && (
                  <Alert variant="info" className="mt-4" data-testid="upload-link-register">
                    <Alert.Heading as="h2" className="h5">
                      <Icon name="person-plus" className="me-2" />
                      {t('uploadLink.registerTitle')}
                    </Alert.Heading>
                    <p>{t('uploadLink.registerBody')}</p>
                    <Link
                      to={`/register?link=${encodeURIComponent(code)}`}
                      className="btn btn-primary"
                    >
                      {t('uploadLink.registerAction')}
                    </Link>
                  </Alert>
                )}
              </div>
            )}
          </Card.Body>
        </Card>
      </Col>

      <ConfirmModal
        show={leaving.asking}
        title={t('upload.leave.title')}
        confirmLabel={t('upload.leave.confirm')}
        cancelLabel={t('upload.leave.stay')}
        onConfirm={leaving.confirm}
        onCancel={leaving.cancel}
      >
        {t('upload.leave.body', { count: summary.queued + summary.uploading })}
      </ConfirmModal>
    </Row>
  )
}

/** How many failed files are only waiting to be sent again. */
function countInterrupted(items: readonly UploadQueueItem[]): number {
  return items.filter((item) => item.status === 'error' && item.interrupted === true).length
}

/** Props for {@link BatchProgress}. */
interface BatchProgressProps {
  /** The batch's bytes sent, `[0, 1]`. */
  fraction: number
  /** Files settled so far. */
  done: number
  /** Files in the batch. */
  total: number
  /** Seconds left, `null` while there is no estimate yet. */
  seconds: number | null
}

/** The remaining time as a short phrase: "zbývá asi 3 min", or "počítám…". */
function useEtaText(seconds: number | null): string {
  const { t } = useTranslation()
  if (seconds === null) {
    return t('uploadLink.eta.calculating')
  }
  const phrase = etaPhrase(seconds)
  switch (phrase.unit) {
    case 'lessThanMinute':
      return t('uploadLink.eta.lessThanMinute')
    case 'minutes':
      return t('uploadLink.eta.minutes', { n: phrase.count })
    case 'hours':
      return t('uploadLink.eta.hours', { count: phrase.count })
  }
}

/**
 * The batch's one progress readout: a tall bar weighted by bytes (a video is not
 * one small step like a photo), its percentage, "X z Y fotek" and the remaining
 * time. The live region carries only the file count and the time — the
 * percentage changes every moment and would chatter in a screen reader.
 */
function BatchProgress({ fraction, done, total, seconds }: BatchProgressProps) {
  const { t } = useTranslation()
  const percent = Math.floor(fraction * 100)
  const etaText = useEtaText(seconds)
  return (
    <div>
      <ProgressBar
        now={percent}
        aria-label={t('uploadLink.progress')}
        className="kk-upload-link__bar"
      />
      <div className="d-flex flex-wrap justify-content-between align-items-baseline gap-2 mt-2">
        <span className="kk-upload-link__percent" data-testid="upload-link-percent">
          {t('uploadLink.percent', { value: percent })}
        </span>
        <span aria-live="polite" className="text-end">
          <span className="d-block fw-semibold" data-testid="upload-link-count">
            {t('uploadLink.count', { done, count: total })}
          </span>
          <span className="d-block text-secondary" data-testid="upload-link-eta">
            {etaText}
          </span>
        </span>
      </div>
    </div>
  )
}

/** The page for a link that cannot take uploads: unknown, expired, revoked or unreachable. */
function Unavailable({ load }: { load: Exclude<LoadState, { status: 'loading' | 'ready' }> }) {
  const { t } = useTranslation()
  let message: string
  if (load.status === 'gone') {
    message = load.state === 'revoked' ? t('uploadLink.revoked') : t('uploadLink.expired')
  } else if (load.status === 'notFound') {
    message = t('uploadLink.notFound')
  } else {
    message = t('uploadLink.loadError')
  }
  return (
    <div data-testid="upload-link-unavailable">
      <h1 className="kk-page-title mb-3">{t('uploadLink.title')}</h1>
      <Alert variant={load.status === 'error' ? 'danger' : 'secondary'} role="status">
        <Icon name="slash-circle" className="me-2" />
        {message}
      </Alert>
      <div className="text-center">
        <Link to="/login">{t('uploadLink.toLogin')}</Link>
      </div>
    </div>
  )
}

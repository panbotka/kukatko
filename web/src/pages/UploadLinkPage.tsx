import { type ReactNode, useEffect, useState } from 'react'
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
import { useUploadQueue } from '../hooks/useUploadQueue'
import { canRetryUpload } from '../lib/uploadErrors'
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

/**
 * The public page behind an upload link (`/u/:code`). Anybody holding the link
 * — signed in or not — sees the curator's title and note, exactly where the
 * photos go (album and label names as chips) and until when the link is valid,
 * and nothing else about the library. They pick many photos and videos at once
 * (a phone's library picker, or drag & drop), each file uploads with its own
 * progress and can be retried, and the batch ends with a one-line summary.
 *
 * An anonymous uploader may say who they are ("Od koho?"), remembered on the
 * device; once a batch finishes they are offered registration through the same
 * link, which needs no shared secret and claims the photos they just sent.
 */
export function UploadLinkPage() {
  const { t, i18n } = useTranslation()
  const { code = '' } = useParams()
  const { status: authStatus, user } = useAuth()
  const registration = registrationOpenFrom(usePublicSettings())
  const [load, setLoad] = useState<LoadState>({ status: 'loading' })
  const [name, setName] = useState(readStoredName)

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

  // Each file reads the name when it starts, so a name typed mid-batch applies
  // to the files still waiting.
  const queue = useUploadQueue(linkUploader(code, () => name))
  const { items, summary, isUploading, isComplete } = queue
  const leaving = useLeaveGuard(isUploading || summary.queued > 0)
  // The drop zone promises Ctrl+V; addFiles is stable, as the hook requires.
  usePasteFiles(queue.addFiles)
  const signedIn = authStatus === 'authenticated'

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

  return (
    <Row className="justify-content-center">
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

            {load.status === 'ready' && (
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

                <DropZone onFiles={queue.addFiles} />
                <div className="d-grid mt-3">
                  <PickFilesButton
                    onFiles={queue.addFiles}
                    label={items.length === 0 ? t('uploadLink.choose') : t('uploadLink.chooseMore')}
                    inputLabel={t('upload.pick.ariaInput')}
                  />
                </div>

                {items.length > 0 && (
                  <div className="mt-4" data-testid="upload-link-progress">
                    <ProgressBar
                      now={Math.round(queue.progress * 100)}
                      label={`${String(summary.created + summary.duplicate + summary.error)} / ${String(summary.total)}`}
                      aria-label={t('uploadLink.progress')}
                      className="mb-3"
                    />
                    {isComplete && (
                      <Alert
                        variant={summary.error > 0 ? 'warning' : 'success'}
                        role="status"
                        className="mb-2"
                        data-testid="upload-link-summary"
                      >
                        {uploadLinkSummary(summary, t)}
                      </Alert>
                    )}
                    {/* Outside the alert: a button on the filled warning
                        background has no contrast on this theme. */}
                    {isComplete && retryable && (
                      <Button
                        variant="outline-warning"
                        size="sm"
                        className="mb-3"
                        onClick={queue.retryFailed}
                      >
                        <Icon name="arrow-clockwise" className="me-1" />
                        {t('upload.actions.retryFailed')}
                      </Button>
                    )}
                    {linkDied && (
                      <Alert variant="danger" role="alert">
                        {t('uploadLink.diedMidway')}
                      </Alert>
                    )}
                    <UploadQueuePanel
                      items={items}
                      summary={summary}
                      onRemove={queue.removeItem}
                      onRetry={queue.retry}
                    />
                  </div>
                )}

                {isComplete && !signedIn && summary.created > 0 && registration === 'open' && (
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
              </>
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

import { type SyntheticEvent, useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Form from 'react-bootstrap/Form'
import InputGroup from 'react-bootstrap/InputGroup'
import Modal from '../components/Modal'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { ConfirmModal } from '../components/ConfirmModal'
import { EntityChip } from '../components/EntityChip'
import { Icon } from '../components/Icon'
import { UploadOrganize } from '../components/upload/UploadOrganize'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { type OrganizeLoadState } from '../hooks/useUploadOrganize'
import { resolvePending } from '../lib/pendingCreate'
import { ALBUM_PARAM } from '../lib/uploadLinks'
import { ApiError } from '../services/auth'
import { createAlbum, createLabel, fetchAlbums, fetchLabels } from '../services/organize'
import {
  codeFromInput,
  type CreatedUploadLink,
  createUploadLink,
  extendUploadLink,
  fetchUploadLinks,
  newUploadLinkCode,
  publicLinkURL,
  restoreUploadLinkCode,
  revokeUploadLink,
  type UploadLink,
  type UploadLinkList,
} from '../services/uploadLinks'

/** Where loading the list stands. */
type ListState =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; list: UploadLinkList }

/** The message of a failed request, for an alert. */
function errorMessage(error: unknown): string {
  return error instanceof ApiError ? error.message : String(error)
}

/** Formats an ISO timestamp as a local date. */
function formatDate(iso: string, language: string): string {
  return new Date(iso).toLocaleDateString(language)
}

/**
 * The curators' page for upload links (`/upload-links`): create a link that
 * files every upload into chosen albums and labels, see the links (an
 * administrator sees everybody's) with their targets, expiry, upload count and
 * last use, copy a live link's address again, extend one or revoke it for good.
 *
 * A link created before codes were stored readably has no address to copy: its
 * card offers "restore original code" (the manager types the code they sent out;
 * the backend keeps it only when it matches) and, behind a warning, a new code,
 * which kills the old address. Any live link can get a new code that way.
 *
 * `?album=<uid>` opens the create form with that album already chosen — the
 * album page's "share an upload link" action lands here.
 */
export function UploadLinksPage() {
  const { t, i18n } = useTranslation()
  useDocumentTitle(t('uploadLinks.title'))
  const { user, isAdmin } = useAuth()
  const [searchParams, setSearchParams] = useSearchParams()
  const presetAlbum = searchParams.get(ALBUM_PARAM)
  const [state, setState] = useState<ListState>({ status: 'loading' })
  const [creating, setCreating] = useState(presetAlbum !== null)
  const [extending, setExtending] = useState<UploadLink | null>(null)
  const [revoking, setRevoking] = useState<UploadLink | null>(null)
  const [restoring, setRestoring] = useState<UploadLink | null>(null)
  const [renewing, setRenewing] = useState<UploadLink | null>(null)
  const [busy, setBusy] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)

  const reload = useCallback((signal?: AbortSignal) => {
    fetchUploadLinks(signal)
      .then((list) => {
        setState({ status: 'ready', list })
      })
      .catch(() => {
        if (signal?.aborted !== true) {
          setState({ status: 'error' })
        }
      })
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    reload(controller.signal)
    return () => {
      controller.abort()
    }
  }, [reload])

  /** Replaces one link in the list with its updated record. */
  function replace(updated: UploadLink) {
    setState((prev) =>
      prev.status === 'ready'
        ? {
            ...prev,
            list: {
              ...prev.list,
              links: prev.list.links.map((l) => (l.uid === updated.uid ? updated : l)),
            },
          }
        : prev,
    )
  }

  function closeCreate() {
    setCreating(false)
    if (presetAlbum !== null) {
      const next = new URLSearchParams(searchParams)
      next.delete(ALBUM_PARAM)
      setSearchParams(next, { replace: true })
    }
    reload()
  }

  async function confirmRevoke() {
    if (revoking === null) {
      return
    }
    setBusy(true)
    try {
      replace(await revokeUploadLink(revoking.uid))
      setRevoking(null)
    } catch (error: unknown) {
      setActionError(errorMessage(error))
      setRevoking(null)
    } finally {
      setBusy(false)
    }
  }

  async function confirmNewCode() {
    if (renewing === null) {
      return
    }
    setBusy(true)
    try {
      replace(await newUploadLinkCode(renewing.uid))
    } catch (error: unknown) {
      setActionError(errorMessage(error))
    } finally {
      setRenewing(null)
      setBusy(false)
    }
  }

  const defaults =
    state.status === 'ready'
      ? { days: state.list.default_days, max: state.list.max_days }
      : { days: 30, max: 365 }

  return (
    <>
      <div className="d-flex flex-wrap align-items-center justify-content-between gap-2 mb-2">
        <h1 className="kk-page-title mb-0">{t('uploadLinks.title')}</h1>
        <Button
          variant="primary"
          onClick={() => {
            setCreating(true)
          }}
        >
          <Icon name="plus-lg" className="me-1" />
          {t('uploadLinks.new')}
        </Button>
      </div>
      <p className="text-secondary">{t('uploadLinks.lead')}</p>

      {actionError !== null && (
        <Alert
          variant="danger"
          dismissible
          onClose={() => {
            setActionError(null)
          }}
        >
          {actionError}
        </Alert>
      )}

      {state.status === 'loading' && (
        <div className="text-center py-4">
          <Spinner animation="border" role="status">
            <span className="visually-hidden">{t('uploadLinks.loading')}</span>
          </Spinner>
        </div>
      )}
      {state.status === 'error' && <Alert variant="danger">{t('uploadLinks.loadError')}</Alert>}
      {state.status === 'ready' && state.list.links.length === 0 && (
        <p className="text-secondary" data-testid="upload-links-empty">
          {t('uploadLinks.empty')}
        </p>
      )}
      {state.status === 'ready' && state.list.links.length > 0 && (
        <div className="d-flex flex-column gap-3" data-testid="upload-links-list">
          {state.list.links.map((link) => (
            <LinkCard
              key={link.uid}
              link={link}
              language={i18n.language}
              showCreator={isAdmin && link.created_by !== user?.uid}
              onExtend={() => {
                setExtending(link)
              }}
              onRevoke={() => {
                setRevoking(link)
              }}
              onRestoreCode={() => {
                setRestoring(link)
              }}
              onNewCode={() => {
                setRenewing(link)
              }}
            />
          ))}
        </div>
      )}

      {creating && (
        <CreateLinkModal
          presetAlbum={presetAlbum}
          defaultDays={defaults.days}
          maxDays={defaults.max}
          onClose={closeCreate}
        />
      )}
      {extending !== null && (
        <ExtendLinkModal
          link={extending}
          defaultDays={defaults.days}
          maxDays={defaults.max}
          onDone={(updated) => {
            replace(updated)
            setExtending(null)
          }}
          onCancel={() => {
            setExtending(null)
          }}
        />
      )}
      {restoring !== null && (
        <RestoreCodeModal
          link={restoring}
          onDone={(updated) => {
            replace(updated)
            setRestoring(null)
          }}
          onCancel={() => {
            setRestoring(null)
          }}
        />
      )}
      <ConfirmModal
        show={renewing !== null}
        title={t('uploadLinks.newCodeTitle')}
        confirmLabel={t('uploadLinks.newCode')}
        busy={busy}
        onConfirm={() => {
          void confirmNewCode()
        }}
        onCancel={() => {
          setRenewing(null)
        }}
      >
        <Alert variant="warning" className="mb-0" data-testid="upload-link-new-code-warning">
          {t('uploadLinks.newCodeBody', { title: renewing?.title ?? '' })}
        </Alert>
      </ConfirmModal>
      <ConfirmModal
        show={revoking !== null}
        title={t('uploadLinks.revokeTitle')}
        confirmLabel={t('uploadLinks.revoke')}
        busy={busy}
        onConfirm={() => {
          void confirmRevoke()
        }}
        onCancel={() => {
          setRevoking(null)
        }}
      >
        {t('uploadLinks.revokeBody', { title: revoking?.title ?? '' })}
      </ConfirmModal>
    </>
  )
}

/** Props of {@link LinkCard}. */
interface LinkCardProps {
  link: UploadLink
  language: string
  /** Whether to name the creator (an administrator looking at somebody else's link). */
  showCreator: boolean
  onExtend: () => void
  onRevoke: () => void
  onRestoreCode: () => void
  onNewCode: () => void
}

/**
 * One link in the list: title, state, targets, expiry, use and its actions. A
 * live link whose code is known shows its address with copy (and share); one
 * whose code is not known offers to restore it first, a new code second.
 */
function LinkCard({
  link,
  language,
  showCreator,
  onExtend,
  onRevoke,
  onRestoreCode,
  onNewCode,
}: LinkCardProps) {
  const { t } = useTranslation()
  const variant = { active: 'success', expired: 'secondary', revoked: 'danger' }[link.state]
  return (
    <Card text="light" data-testid="upload-link-card">
      <Card.Body>
        <div className="d-flex flex-wrap align-items-center gap-2 mb-2">
          <h2 className="h5 mb-0">{link.title !== '' ? link.title : t('uploadLinks.untitled')}</h2>
          <Badge bg={variant}>{t(`uploadLinks.state.${link.state}`)}</Badge>
        </div>
        <div className="d-flex flex-wrap gap-2 mb-2">
          {link.albums.map((album) => (
            <EntityChip key={album.uid} kind="album" to={`/albums/${album.uid}`}>
              {album.name}
            </EntityChip>
          ))}
          {link.labels.map((label) => (
            <EntityChip key={label.uid} kind="tag" to={`/labels/${label.uid}`}>
              {label.name}
            </EntityChip>
          ))}
        </div>
        <p className="small text-secondary mb-2">
          {link.state === 'revoked' && link.revoked_at !== null
            ? t('uploadLinks.revokedOn', { date: formatDate(link.revoked_at, language) })
            : t(link.state === 'expired' ? 'uploadLinks.expiredOn' : 'uploadLinks.validUntil', {
                date: formatDate(link.expires_at, language),
              })}
          {' · '}
          {t('uploadLinks.uploads', { count: link.upload_count })}
          {link.last_used_at !== null &&
            ` · ${t('uploadLinks.lastUsed', { date: formatDate(link.last_used_at, language) })}`}
          {showCreator &&
            link.created_by_name !== '' &&
            ` · ${t('uploadLinks.createdBy', { name: link.created_by_name })}`}
        </p>
        {link.state === 'active' && link.path !== undefined && (
          <LinkAddress url={publicLinkURL(link.path)} title={link.title} size="sm" />
        )}
        {link.state !== 'revoked' && link.path === undefined && (
          <p className="small text-warning mb-2" data-testid="upload-link-code-unknown">
            {t(
              link.state === 'expired'
                ? 'uploadLinks.codeUnknownExpired'
                : 'uploadLinks.codeUnknown',
            )}
          </p>
        )}
        {link.state !== 'revoked' && (
          <div className="d-flex flex-wrap gap-2">
            {link.path === undefined && (
              <Button variant="primary" size="sm" onClick={onRestoreCode}>
                <Icon name="key" className="me-1" />
                {t('uploadLinks.restoreCode')}
              </Button>
            )}
            <Button variant="outline-secondary" size="sm" onClick={onExtend}>
              <Icon name="hourglass-split" className="me-1" />
              {t('uploadLinks.extend')}
            </Button>
            <Button variant="outline-warning" size="sm" onClick={onNewCode}>
              <Icon name="arrow-clockwise" className="me-1" />
              {t('uploadLinks.newCode')}
            </Button>
            <Button variant="outline-danger" size="sm" onClick={onRevoke}>
              <Icon name="slash-circle" className="me-1" />
              {t('uploadLinks.revoke')}
            </Button>
          </div>
        )}
      </Card.Body>
    </Card>
  )
}

/** Props of {@link CreateLinkModal}. */
interface CreateLinkModalProps {
  /** An album UID to choose up front, or null. */
  presetAlbum: string | null
  defaultDays: number
  maxDays: number
  onClose: () => void
}

/**
 * The create form — title, note, albums and labels (existing or created inline),
 * validity — and, once created, the link itself with copy and share buttons.
 */
function CreateLinkModal({ presetAlbum, defaultDays, maxDays, onClose }: CreateLinkModalProps) {
  const { t } = useTranslation()
  const { canCurate } = useAuth()
  const [load, setLoad] = useState<OrganizeLoadState>({ status: 'loading' })
  const [albums, setAlbums] = useState<string[]>(presetAlbum === null ? [] : [presetAlbum])
  const [labels, setLabels] = useState<string[]>([])
  const [title, setTitle] = useState('')
  const [note, setNote] = useState('')
  const [days, setDays] = useState(String(defaultDays))
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [created, setCreated] = useState<CreatedUploadLink | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    Promise.all([fetchAlbums(controller.signal), fetchLabels(controller.signal)])
      .then(([albumList, labelList]) => {
        setLoad({ status: 'ready', albums: albumList, labels: labelList })
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          setLoad({ status: 'error' })
        }
      })
    return () => {
      controller.abort()
    }
  }, [])

  const validDays = Number(days)
  const daysValid = Number.isInteger(validDays) && validDays >= 1 && validDays <= maxDays
  const canSubmit = !submitting && daysValid && albums.length + labels.length > 0

  async function submit(event: SyntheticEvent) {
    event.preventDefault()
    if (!canSubmit) {
      return
    }
    setSubmitting(true)
    setError(null)
    try {
      const albumUids = await resolvePending(albums, async (name) => {
        const album = await createAlbum({ title: name, description: '', private: false })
        return album.uid
      })
      const labelUids = await resolvePending(labels, async (name) => {
        const label = await createLabel({ name, priority: 0 })
        return label.uid
      })
      // Whatever was created stays chosen as a real UID, so a retry does not
      // create it twice.
      setAlbums(albumUids.values)
      setLabels(labelUids.values)
      if (albumUids.status === 'failed' || labelUids.status === 'failed') {
        setError(t('uploadLinks.createTargetError'))
        return
      }
      setCreated(
        await createUploadLink({
          title: title.trim(),
          note: note.trim(),
          album_uids: albumUids.values,
          label_uids: labelUids.values,
          valid_days: validDays,
        }),
      )
    } catch (err: unknown) {
      setError(errorMessage(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal show onHide={onClose} size="lg" aria-labelledby="upload-link-create-title">
      <Modal.Header closeButton>
        <Modal.Title id="upload-link-create-title">
          {created === null ? t('uploadLinks.new') : t('uploadLinks.createdTitle')}
        </Modal.Title>
      </Modal.Header>
      {created !== null ? (
        <>
          <Modal.Body>
            <CreatedLink created={created} />
          </Modal.Body>
          <Modal.Footer>
            <Button variant="primary" onClick={onClose}>
              {t('uploadLinks.done')}
            </Button>
          </Modal.Footer>
        </>
      ) : (
        <Form
          onSubmit={(event) => {
            void submit(event)
          }}
        >
          <Modal.Body>
            {error !== null && <Alert variant="danger">{error}</Alert>}
            <Form.Group controlId="upload-link-title" className="mb-3">
              <Form.Label>{t('uploadLinks.fieldTitle')}</Form.Label>
              <Form.Control
                value={title}
                maxLength={200}
                placeholder={t('uploadLinks.fieldTitlePlaceholder')}
                onChange={(e) => {
                  setTitle(e.target.value)
                }}
              />
            </Form.Group>
            <Form.Group controlId="upload-link-note" className="mb-3">
              <Form.Label>{t('uploadLinks.fieldNote')}</Form.Label>
              <Form.Control
                as="textarea"
                rows={2}
                value={note}
                maxLength={2000}
                onChange={(e) => {
                  setNote(e.target.value)
                }}
              />
              <Form.Text className="text-secondary">{t('uploadLinks.fieldNoteHint')}</Form.Text>
            </Form.Group>
            <p className="mb-2">{t('uploadLinks.fieldTargets')}</p>
            <UploadOrganize
              load={load}
              albums={albums}
              labels={labels}
              onAlbums={setAlbums}
              onLabels={setLabels}
              disabled={submitting}
              allowCreate={canCurate}
            />
            <Form.Group controlId="upload-link-days" className="mt-3">
              <Form.Label>{t('uploadLinks.fieldDays')}</Form.Label>
              <Form.Control
                type="number"
                min={1}
                max={maxDays}
                value={days}
                isInvalid={!daysValid}
                onChange={(e) => {
                  setDays(e.target.value)
                }}
              />
              <Form.Control.Feedback type="invalid">
                {t('uploadLinks.fieldDaysInvalid', { max: maxDays })}
              </Form.Control.Feedback>
            </Form.Group>
          </Modal.Body>
          <Modal.Footer>
            <Button variant="outline-secondary" onClick={onClose} disabled={submitting}>
              {t('uploadLinks.cancel')}
            </Button>
            <Button type="submit" variant="primary" disabled={!canSubmit}>
              {submitting && <Spinner animation="border" size="sm" className="me-2" />}
              {t('uploadLinks.create')}
            </Button>
          </Modal.Footer>
        </Form>
      )}
    </Modal>
  )
}

/**
 * A freshly created link: its address with copy and share, and a word that it
 * stays copyable from the list.
 */
function CreatedLink({ created }: { created: CreatedUploadLink }) {
  const { t } = useTranslation()
  return (
    <div data-testid="upload-link-created">
      <p>{t('uploadLinks.createdLead')}</p>
      <LinkAddress url={publicLinkURL(created.path)} title={created.link.title} />
      <p className="small text-secondary mb-0">{t('uploadLinks.copyLater')}</p>
    </div>
  )
}

/** Props of {@link LinkAddress}. */
interface LinkAddressProps {
  /** The absolute address of the public page. */
  url: string
  /** The link's title, offered to the share sheet. */
  title: string
  /** `sm` for the compact list cards. */
  size?: 'sm'
}

/**
 * A link's address in a read-only field, a copy button that confirms with
 * "Copied" and — where the device offers it — the system share sheet. The create
 * flow and every live card in the list use this one control.
 */
function LinkAddress({ url, title, size }: LinkAddressProps) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const canShare = typeof navigator.share === 'function'

  async function copy() {
    try {
      await navigator.clipboard.writeText(url)
      setCopied(true)
    } catch {
      // Clipboard refused: the link is in the field, ready to select by hand.
    }
  }

  async function share() {
    try {
      await navigator.share({ title, url })
    } catch {
      // Dismissed, or the share sheet failed: the field and copy button remain.
    }
  }

  return (
    <InputGroup className="mb-2" size={size}>
      <Form.Control
        readOnly
        value={url}
        aria-label={t('uploadLinks.linkLabel')}
        onFocus={(e) => {
          e.target.select()
        }}
      />
      <Button
        variant="outline-secondary"
        onClick={() => {
          void copy()
        }}
      >
        <Icon name={copied ? 'check-lg' : 'clipboard'} className="me-1" />
        {copied ? t('uploadLinks.copied') : t('uploadLinks.copy')}
      </Button>
      {canShare && (
        <Button
          variant="outline-secondary"
          onClick={() => {
            void share()
          }}
        >
          <Icon name="share" className="me-1" />
          {t('uploadLinks.share')}
        </Button>
      )}
    </InputGroup>
  )
}

/** Props of {@link RestoreCodeModal}. */
interface RestoreCodeModalProps {
  link: UploadLink
  onDone: (updated: UploadLink) => void
  onCancel: () => void
}

/**
 * Asks for the original code of a link whose code is not stored — the code
 * itself or the whole link that was sent out. The backend keeps it only when it
 * matches, so a typo is refused and changes nothing.
 */
function RestoreCodeModal({ link, onDone, onCancel }: RestoreCodeModalProps) {
  const { t } = useTranslation()
  const [value, setValue] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const code = codeFromInput(value)

  async function submit(event: SyntheticEvent) {
    event.preventDefault()
    if (code === '') {
      return
    }
    setSubmitting(true)
    setError(null)
    try {
      onDone(await restoreUploadLinkCode(link.uid, code))
    } catch (err: unknown) {
      setError(
        err instanceof ApiError && err.status === 422
          ? t('uploadLinks.restoreMismatch')
          : errorMessage(err),
      )
      setSubmitting(false)
    }
  }

  return (
    <Modal show onHide={onCancel} aria-labelledby="upload-link-restore-title">
      <Form
        onSubmit={(event) => {
          void submit(event)
        }}
      >
        <Modal.Header closeButton>
          <Modal.Title id="upload-link-restore-title">{t('uploadLinks.restoreTitle')}</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {error !== null && (
            <Alert variant="danger" data-testid="upload-link-restore-error">
              {error}
            </Alert>
          )}
          <p>{t('uploadLinks.restoreLead', { title: link.title })}</p>
          <Form.Group controlId="upload-link-restore-code">
            <Form.Label>{t('uploadLinks.restoreField')}</Form.Label>
            <Form.Control
              value={value}
              autoComplete="off"
              spellCheck={false}
              placeholder={t('uploadLinks.restorePlaceholder')}
              onChange={(e) => {
                setValue(e.target.value)
              }}
            />
          </Form.Group>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="outline-secondary" onClick={onCancel} disabled={submitting}>
            {t('uploadLinks.cancel')}
          </Button>
          <Button type="submit" variant="primary" disabled={submitting || code === ''}>
            {submitting && <Spinner animation="border" size="sm" className="me-2" />}
            {t('uploadLinks.restoreCode')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  )
}

/** Props of {@link ExtendLinkModal}. */
interface ExtendLinkModalProps {
  link: UploadLink
  defaultDays: number
  maxDays: number
  onDone: (updated: UploadLink) => void
  onCancel: () => void
}

/** Asks for how many days from today the link should stay valid. */
function ExtendLinkModal({ link, defaultDays, maxDays, onDone, onCancel }: ExtendLinkModalProps) {
  const { t } = useTranslation()
  const [days, setDays] = useState(String(defaultDays))
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const validDays = Number(days)
  const daysValid = Number.isInteger(validDays) && validDays >= 1 && validDays <= maxDays

  async function submit(event: SyntheticEvent) {
    event.preventDefault()
    if (!daysValid) {
      return
    }
    setSubmitting(true)
    try {
      onDone(await extendUploadLink(link.uid, validDays))
    } catch (err: unknown) {
      setError(errorMessage(err))
      setSubmitting(false)
    }
  }

  return (
    <Modal show onHide={onCancel} aria-labelledby="upload-link-extend-title">
      <Form
        onSubmit={(event) => {
          void submit(event)
        }}
      >
        <Modal.Header closeButton>
          <Modal.Title id="upload-link-extend-title">{t('uploadLinks.extendTitle')}</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {error !== null && <Alert variant="danger">{error}</Alert>}
          <Form.Group controlId="upload-link-extend-days">
            <Form.Label>{t('uploadLinks.extendDays')}</Form.Label>
            <Form.Control
              type="number"
              min={1}
              max={maxDays}
              value={days}
              isInvalid={!daysValid}
              onChange={(e) => {
                setDays(e.target.value)
              }}
            />
            <Form.Control.Feedback type="invalid">
              {t('uploadLinks.fieldDaysInvalid', { max: maxDays })}
            </Form.Control.Feedback>
          </Form.Group>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="outline-secondary" onClick={onCancel} disabled={submitting}>
            {t('uploadLinks.cancel')}
          </Button>
          <Button type="submit" variant="primary" disabled={submitting || !daysValid}>
            {t('uploadLinks.extend')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  )
}

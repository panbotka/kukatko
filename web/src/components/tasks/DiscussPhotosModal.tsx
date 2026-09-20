import { useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { discussionBody, photoRefsThatFit } from '../../lib/photoRefs'
import { createComment, taskSubject } from '../../services/comments'
import { thumbUrl } from '../../services/photos'
import Modal from '../Modal'
import { useToast } from '../toast/ToastContext'

/** How many of the selected photographs the dialog previews before "+N". */
export const DISCUSS_PREVIEW_MAX = 24

/** The rendition the preview draws. */
const PREVIEW_THUMB_SIZE = 'tile_100'

/** Props for {@link DiscussPhotosModal}. */
export interface DiscussPhotosModalProps {
  show: boolean
  /** The task whose thread the comment goes on. */
  taskUid: string
  /** The selected photographs, in selection order — the uids the comment lists. */
  photoUids: readonly string[]
  onClose: () => void
  /** Called once the comment landed; the caller clears the selection. */
  onPosted: () => void
}

/**
 * Puts a selection of photographs into a task's discussion: a preview of what
 * is selected, an optional message, and Send.
 *
 * The typical objection to a batch ("these three are wrong") used to mean
 * copying uids by hand into the box; this writes them. What it posts is **one
 * ordinary comment** — the message, a blank line, one uid per line, plain text
 * — so the thread stays the one record, the CLI reads it as a list, and the
 * web renders each uid as a link with a thumbnail (see `CommentBody`). Nothing
 * else is sent: no new endpoint, no new shape.
 *
 * Available to anybody who may comment, viewers included: pointing at pictures
 * is how a reviewer answers, and the thread has always been open to every role.
 *
 * A selection too large for one comment (the body limit is 2000 characters;
 * every uid costs 27 of them) is refused here, with the count that fits, rather
 * than sent and bounced. A failed post keeps the dialog open with the reason so
 * the message is not lost.
 */
export function DiscussPhotosModal({
  show,
  taskUid,
  photoUids,
  onClose,
  onPosted,
}: DiscussPhotosModalProps) {
  const { t } = useTranslation()
  const { show: toast } = useToast()
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState(false)

  // A fresh dialog every time it opens: the last message was sent (or given up).
  useEffect(() => {
    if (show) {
      setMessage('')
      setFailed(false)
    }
  }, [show])

  const fits = photoRefsThatFit(message)
  const tooMany = photoUids.length > fits
  const preview = photoUids.slice(0, DISCUSS_PREVIEW_MAX)
  const rest = photoUids.length - preview.length

  function send() {
    if (busy || tooMany || photoUids.length === 0) {
      return
    }
    setBusy(true)
    setFailed(false)
    createComment(taskSubject(taskUid), discussionBody(message, photoUids))
      .then(() => {
        toast({ variant: 'success', message: t('batch.discuss.sent', { count: photoUids.length }) })
        setBusy(false)
        onPosted()
      })
      .catch(() => {
        setFailed(true)
        setBusy(false)
      })
  }

  return (
    <Modal show={show} onHide={busy ? undefined : onClose} centered>
      <Modal.Header closeButton>
        <Modal.Title as="h2" className="h5">
          {t('batch.discuss.title')}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {failed && (
          <Alert variant="danger" className="py-2">
            {t('batch.discuss.failed')}
          </Alert>
        )}

        <ul
          className="kk-discuss-preview"
          aria-label={t('tasks.row.photos', { count: photoUids.length })}
        >
          {preview.map((uid) => (
            <li key={uid}>
              <img src={thumbUrl(uid, PREVIEW_THUMB_SIZE)} alt="" loading="lazy" decoding="async" />
            </li>
          ))}
          {rest > 0 && (
            <li className="kk-discuss-preview__more">{t('batch.discuss.more', { count: rest })}</li>
          )}
        </ul>

        <Form.Group>
          <Form.Label htmlFor="discuss-message">{t('batch.discuss.message')}</Form.Label>
          <Form.Control
            id="discuss-message"
            as="textarea"
            rows={3}
            value={message}
            placeholder={t('batch.discuss.placeholder')}
            autoFocus
            disabled={busy}
            onChange={(event) => {
              setMessage(event.target.value)
            }}
          />
        </Form.Group>

        {tooMany && (
          <Alert variant="warning" className="py-2 mt-3 mb-0">
            {t('batch.discuss.tooMany', { fits, count: photoUids.length })}
          </Alert>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" onClick={onClose} disabled={busy}>
          {t('confirmModal.cancel')}
        </Button>
        <Button
          variant="primary"
          onClick={send}
          disabled={busy || tooMany || photoUids.length === 0}
        >
          {busy && <Spinner animation="border" size="sm" className="me-1" aria-hidden="true" />}
          {busy ? t('batch.discuss.sending') : t('batch.discuss.send')}
        </Button>
      </Modal.Footer>
    </Modal>
  )
}

import { type ParseKeys } from 'i18next'
import { type SyntheticEvent, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { ConfirmModal } from '../ConfirmModal'
import { Icon } from '../Icon'
import { type CommentFailure, useComments } from '../../hooks/useComments'
import { MAX_COMMENT_LENGTH } from '../../services/comments'

import { CommentItem } from './CommentItem'

/** Props for {@link CommentsPanel}. */
export interface CommentsPanelProps {
  photoUid: string
  /**
   * The reader's own uid, or null when unknown. It decides which comments carry an
   * edit affordance — the author's own, and nobody else's.
   */
  currentUserUid: string | null
  /**
   * Whether the reader may remove other people's comments (admin and above). An
   * admin moderates the thread but never rewrites it: removing a remark is a
   * housekeeping act, editing one would put words in someone else's mouth.
   */
  canModerate: boolean
  /** Reports the thread's length up to the viewer chrome, so the badge stays true. */
  onCountChange?: (count: number) => void
}

/**
 * The message for each failed write. Written out rather than interpolated into a
 * key so every string the panel can show is greppable and type-checked.
 */
const FAILURE_MESSAGE: Record<CommentFailure, ParseKeys> = {
  throttled: 'photo.comments.throttled',
  forbidden: 'photo.comments.forbidden',
  failed: 'photo.comments.failed',
}

/**
 * The conversation around a photo: the thread, and the box to add to it.
 *
 * This is the social half of the archive. Most of what a family knows about an old
 * photograph — who the boy on the left is, which summer it was, that the barn burned
 * down the year after — is not metadata anybody will ever type into a form; it comes
 * out when someone recognises something and says so. So the thread is deliberately
 * cheap to join: **every signed-in role may post, viewers included** (the backend
 * guards the route with `RequireAuth`, not `RequireWrite`), because a read-only half
 * of the family locked out of the conversation is most of the family locked out.
 *
 * Comments read oldest first, like a conversation. Enter posts and Shift+Enter adds
 * a line, which is the convention every chat has taught people; the send button
 * stays for the reader who does not know it and for touch, where there is no Enter
 * to speak of.
 */
export function CommentsPanel({
  photoUid,
  currentUserUid,
  canModerate,
  onCountChange,
}: CommentsPanelProps) {
  const { t } = useTranslation()
  const { status, comments, count, busy, failure, post, edit, remove } = useComments(photoUid, {
    onCountChange,
  })
  const [draft, setDraft] = useState('')
  // The comment the reader has asked to delete, or null. One dialog serves the
  // whole thread — a modal per row would be a modal per comment in the DOM.
  const [pendingDelete, setPendingDelete] = useState<string | null>(null)

  const submit = async (event: SyntheticEvent): Promise<void> => {
    event.preventDefault()
    const body = draft.trim()
    if (body === '' || busy) {
      return
    }
    if (await post(body)) {
      setDraft('')
    }
  }

  const confirmDelete = async (): Promise<void> => {
    if (pendingDelete === null) {
      return
    }
    await remove(pendingDelete)
    setPendingDelete(null)
  }

  return (
    <div className="kk-comments">
      {/* The heading counts the thread once there is one ("3 komentáře"), which is
          both the section's name and the discoverability the badge on the toggle
          promised. i18next owns the plural form — Czech needs three of them. */}
      <p className="kk-text-eyebrow mb-2">
        {count > 0 ? t('photo.comments.count', { count }) : t('photo.comments.title')}
      </p>

      {status === 'loading' && (
        <div className="kk-comments__state">
          <Spinner animation="border" size="sm" role="status" />
          <span className="ms-2">{t('photo.comments.loading')}</span>
        </div>
      )}

      {status === 'error' && (
        <Alert variant="danger" className="py-2 px-3 mb-2">
          {t('photo.comments.loadFailed')}
        </Alert>
      )}

      {status === 'ready' && comments.length === 0 && (
        <p className="kk-comments__empty">{t('photo.comments.empty')}</p>
      )}

      {comments.length > 0 && (
        <ul className="kk-comments__list" aria-label={t('photo.comments.title')}>
          {comments.map((comment) => (
            <CommentItem
              key={comment.uid}
              comment={comment}
              canEdit={currentUserUid !== null && comment.author_uid === currentUserUid}
              canDelete={
                canModerate || (currentUserUid !== null && comment.author_uid === currentUserUid)
              }
              busy={busy}
              onEdit={(body) => edit(comment.uid, body)}
              onDelete={() => {
                setPendingDelete(comment.uid)
              }}
            />
          ))}
        </ul>
      )}

      {failure !== null && (
        <Alert variant="warning" className="py-2 px-3 mb-2">
          {t(FAILURE_MESSAGE[failure])}
        </Alert>
      )}

      {/* The composer. It stays put for every role — the deliberate exception to
          the read-only rule — and is the reason the sheet lifts with the on-screen
          keyboard on a phone (see `--kk-keyboard-inset` in viewer.css). */}
      <form
        className="kk-comments__composer"
        onSubmit={(event) => {
          void submit(event)
        }}
      >
        <label className="visually-hidden" htmlFor="kk-comment-new">
          {t('photo.comments.inputLabel')}
        </label>
        <textarea
          id="kk-comment-new"
          className="form-control form-control-sm"
          rows={2}
          maxLength={MAX_COMMENT_LENGTH}
          placeholder={t('photo.comments.placeholder')}
          value={draft}
          disabled={busy}
          onChange={(event) => {
            setDraft(event.target.value)
          }}
          onKeyDown={(event) => {
            // Enter sends, Shift+Enter breaks the line: the convention every chat
            // has already taught the reader, so nobody has to be told.
            if (event.key === 'Enter' && !event.shiftKey) {
              event.preventDefault()
              void submit(event)
            }
          }}
          onFocus={(event) => {
            // Inside the phone's bottom sheet the composer can start below the fold;
            // bringing it into view on focus means the reader types where they can
            // see what they are typing.
            event.currentTarget.scrollIntoView({ block: 'nearest' })
          }}
        />
        <Button
          type="submit"
          variant="primary"
          size="sm"
          className="kk-comments__send"
          disabled={busy || draft.trim() === ''}
          aria-label={t('photo.comments.submit')}
          title={t('photo.comments.submitHint')}
        >
          <Icon name="send" />
        </Button>
      </form>

      <ConfirmModal
        show={pendingDelete !== null}
        title={t('photo.comments.deleteTitle')}
        confirmLabel={t('photo.comments.delete')}
        busy={busy}
        onConfirm={() => {
          void confirmDelete()
        }}
        onCancel={() => {
          setPendingDelete(null)
        }}
      >
        {t('photo.comments.deleteBody')}
      </ConfirmModal>
    </div>
  )
}

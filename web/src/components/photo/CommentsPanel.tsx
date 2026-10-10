import { type ParseKeys } from 'i18next'
import { type SyntheticEvent, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { ConfirmModal } from '../ConfirmModal'
import { Icon } from '../Icon'
import { type CommentFailure, useComments } from '../../hooks/useComments'
import { type Comment, type CommentSubject, MAX_COMMENT_LENGTH } from '../../services/comments'
import { type TaskState } from '../../services/tasks'

import { CommentItem } from './CommentItem'

/** Props for {@link CommentsPanel}. */
export interface CommentsPanelProps {
  /**
   * What the thread hangs off: a photograph, or a task. The panel itself is the
   * same either way — one conversation, one composer — so the subject is all that
   * distinguishes the two.
   */
  subject: CommentSubject
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
  /** Reports the whole thread whenever it changes; see `UseCommentsOptions`. */
  onThreadChange?: (comments: Comment[]) => void
  /** Bumping it refetches the thread, for a comment posted outside the panel. */
  reloadKey?: string | number
  /**
   * Whether the panel writes its own heading. True by default — in the viewer
   * the thread stands on its own and has to name itself. A caller that already
   * titles the section around it (the task page puts the panel on a card headed
   * "Diskuse") passes false, so the reader is not told twice.
   */
  heading?: boolean
  /**
   * A second, primary send button that posts the comment *and* moves the task on
   * — "Odeslat a předat agentovi". Only the task page passes it, and only for a
   * writer on a task that is not already with the agent and not closed: the
   * panel draws it whenever it is given and decides nothing about who may.
   */
  handOver?: CommentHandOver
}

/** The task page's "send and hand over" action; see `CommentsPanelProps.handOver`. */
export interface CommentHandOver {
  /** The state the comment moves the task to, in the same transaction. */
  state: TaskState
  /** The button's visible label. */
  label: string
  /** Called once the comment and the move have both landed. */
  onDone: () => void
}

/**
 * Whether a key press in the composer is the hand-over shortcut,
 * Ctrl/Cmd+Shift+Enter. Plain Enter and Ctrl/Cmd+Enter send; Shift+Enter alone
 * breaks the line, so the hand-over needs both modifiers to be unmistakable.
 */
function isHandOverKey(event: {
  key: string
  shiftKey: boolean
  ctrlKey: boolean
  metaKey: boolean
}) {
  return event.key === 'Enter' && event.shiftKey && (event.ctrlKey || event.metaKey)
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
  subject,
  currentUserUid,
  canModerate,
  onCountChange,
  onThreadChange,
  reloadKey,
  heading = true,
  handOver,
}: CommentsPanelProps) {
  const { t } = useTranslation()
  const { status, comments, count, busy, failure, post, edit, remove } = useComments(subject, {
    onCountChange,
    onThreadChange,
    reloadKey,
  })
  const [draft, setDraft] = useState('')
  // The comment the reader has asked to delete, or null. One dialog serves the
  // whole thread — a modal per row would be a modal per comment in the DOM.
  const [pendingDelete, setPendingDelete] = useState<string | null>(null)

  // One send for both buttons. The hand-over is a single request — the comment
  // and the move commit together server-side — so a failure leaves the draft in
  // the box with nothing posted, and pressing the button again cannot double it.
  const submit = async (event: SyntheticEvent, moveTo?: CommentHandOver): Promise<void> => {
    event.preventDefault()
    const body = draft.trim()
    if (body === '' || busy) {
      return
    }
    if (await post(body, moveTo?.state)) {
      setDraft('')
      moveTo?.onDone()
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
      {heading && (
        <p className="kk-text-eyebrow mb-2">
          {count > 0 ? t('photo.comments.count', { count }) : t('photo.comments.title')}
        </p>
      )}

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
        className={
          handOver
            ? 'kk-comments__composer kk-comments__composer--stacked'
            : 'kk-comments__composer'
        }
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
            if (handOver && isHandOverKey(event)) {
              void submit(event, handOver)
              return
            }
            // Enter (and Ctrl/Cmd+Enter) sends, Shift+Enter breaks the line: the
            // convention every chat has already taught the reader.
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
        {handOver ? (
          // Two labelled buttons rather than an icon: the hand-over is the
          // primary action here and has to say what it does, and the plain send
          // beside it has to read as the lesser of the two. They wrap onto
          // separate lines on a narrow phone rather than squeezing.
          <div className="kk-comments__actions">
            <Button
              type="submit"
              variant="outline-primary"
              className="kk-comments__action"
              disabled={busy || draft.trim() === ''}
              title={t('photo.comments.submitHint')}
            >
              <Icon name="send" /> {t('photo.comments.send')}
            </Button>
            <Button
              type="button"
              variant="primary"
              className="kk-comments__action"
              disabled={busy || draft.trim() === ''}
              title={t('photo.comments.handOverHint')}
              onClick={(event) => {
                void submit(event, handOver)
              }}
            >
              <Icon name="robot" /> {handOver.label}
            </Button>
          </div>
        ) : (
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
        )}
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

import { type SyntheticEvent, useState } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { Icon } from '../Icon'
import { PersonAvatar } from '../PersonAvatar'
import { formatDateTimeMinutes } from '../../lib/format'
import { commentAnchorId } from '../../lib/mentions'
import { formatRelativeTime } from '../../lib/relativeTime'
import { type Comment, MAX_COMMENT_LENGTH } from '../../services/comments'

import { CommentBody } from './CommentBody'
import { MentionTextarea } from './MentionTextarea'
/** Props for {@link CommentItem}. */
export interface CommentItemProps {
  comment: Comment
  /** The reader's own uid, or null when unknown — a mention of them is drawn stronger. */
  currentUserUid: string | null
  /** Whether the reader wrote this comment and may rewrite it. */
  canEdit: boolean
  /** Whether the reader may remove it — its author, or an admin moderating. */
  canDelete: boolean
  /** True while any thread write is in flight, so the row's controls stand down. */
  busy: boolean
  /** Saves a rewritten body; resolves true when it landed (the row then closes). */
  onEdit: (body: string) => Promise<boolean>
  /** Asks for the comment to be removed — the thread confirms before it happens. */
  onDelete: () => void
}

/**
 * One comment in a photo's thread: who said it, how long ago, and what they said —
 * with the edit and delete affordances the reader is actually allowed to use.
 *
 * The body renders through {@link CommentBody}: sanitised Markdown, every
 * photo uid a link (and a thumbnail under the remark) — on a task's thread
 * carrying the task, so the viewer stays in it — and every `@`-mention a chip
 * resolved from the comment's own `mentions`. The row carries the
 * `comment-<uid>` id a notification's link scrolls to, and lights up while it
 * is the URL's target.
 *
 * The avatar beside it is {@link PersonAvatar}: whatever picture the author's
 * account resolves to — one they uploaded, a photo of the library they picked,
 * or the face of the person the account says they are — and the coloured initial
 * when none of those answers. That is where "linking an account publishes that
 * face" becomes visible, which is why the account page says so before the link is
 * made.
 *
 * Editing happens **in place**, in the same mention-aware textarea the composer
 * uses ({@link MentionTextarea}) — a modal for fixing a typo would hide the
 * conversation the remark belongs to. Deleting does
 * not happen here: the row only asks, and the thread above raises the confirmation,
 * so one dialog serves every row.
 */
export function CommentItem({
  comment,
  currentUserUid,
  canEdit,
  canDelete,
  busy,
  onEdit,
  onDelete,
}: CommentItemProps) {
  const { t, i18n } = useTranslation()
  const [draft, setDraft] = useState<string | null>(null)
  const editing = draft !== null

  // An account deleted since leaves its comments authorless (the backend keeps the
  // row and clears the author), so the thread still reads — with the fact stated,
  // rather than an empty name where a person used to be. An author who has left
  // the project is a former member: no name and no picture, so the project stops
  // seeing the account's live name.
  const former = comment.author_former === true
  const authorName = former
    ? t('formerMember')
    : comment.author_name === ''
      ? t('photo.comments.unknownAuthor')
      : comment.author_name

  const save = async (event: SyntheticEvent): Promise<void> => {
    event.preventDefault()
    const body = (draft ?? '').trim()
    if (body === '' || body === comment.body) {
      setDraft(null)
      return
    }
    if (await onEdit(body)) {
      setDraft(null)
    }
  }

  return (
    <li className="kk-comment" id={commentAnchorId(comment.uid)}>
      {/* Whatever the author's account resolves to; the coloured initial — the
          common case — when it resolves to nothing. An authorless comment (its
          account was deleted) names no user and goes straight to the letter. */}
      <PersonAvatar name={authorName} userUid={former ? undefined : comment.author_uid} />
      <div className="kk-comment__main">
        <p className="kk-comment__meta">
          <span className="kk-comment__author">{authorName}</span>
          <time
            className="kk-comment__time"
            dateTime={comment.created_at}
            title={formatDateTimeMinutes(comment.created_at, i18n.language)}
          >
            {formatRelativeTime(comment.created_at, i18n.language)}
          </time>
          {comment.edited_at !== undefined && (
            <span className="kk-comment__edited">{t('photo.comments.edited')}</span>
          )}
        </p>

        {editing ? (
          <form
            className="kk-comment__edit"
            onSubmit={(event) => {
              void save(event)
            }}
          >
            <label className="visually-hidden" htmlFor={`kk-comment-edit-${comment.uid}`}>
              {t('photo.comments.editLabel')}
            </label>
            <MentionTextarea
              id={`kk-comment-edit-${comment.uid}`}
              rows={3}
              maxLength={MAX_COMMENT_LENGTH}
              value={draft}
              autoFocus
              disabled={busy}
              onChange={setDraft}
              onKeyDown={(event) => {
                // Enter saves, Shift+Enter is a newline — the same bargain the
                // composer strikes, so editing feels like writing.
                if (event.key === 'Enter' && !event.shiftKey) {
                  event.preventDefault()
                  void save(event)
                }
                if (event.key === 'Escape') {
                  setDraft(null)
                }
              }}
            />
            <div className="kk-comment__edit-actions">
              <Button type="submit" size="sm" variant="primary" disabled={busy}>
                {t('photo.comments.save')}
              </Button>
              <Button
                type="button"
                size="sm"
                variant="link"
                disabled={busy}
                onClick={() => {
                  setDraft(null)
                }}
              >
                {t('photo.comments.cancel')}
              </Button>
            </div>
          </form>
        ) : (
          <CommentBody
            body={comment.body}
            taskUid={comment.task_uid}
            mentions={comment.mentions}
            currentUserUid={currentUserUid}
          />
        )}
      </div>

      {/* The row's affordances are two quiet icon buttons in a column of their
          own, beside the comment rather than under it: a thread of ten would
          otherwise end every remark with a row of controls. Same size, same muted
          colour; delete only turns red when it is aimed at (see viewer.css),
          and on a mouse the pair surfaces with the comment's hover or focus.
          Both carry `kukatko-tap-target-touch`, so each clears a 44px square on
          a coarse pointer, where they are always visible. */}
      {!editing && (canEdit || canDelete) && (
        <div className="kk-comment__actions">
          {canEdit && (
            <Button
              type="button"
              size="sm"
              variant="link"
              className="kk-comment__action kukatko-tap-target-touch"
              disabled={busy}
              aria-label={t('photo.comments.edit')}
              title={t('photo.comments.edit')}
              onClick={() => {
                setDraft(comment.body)
              }}
            >
              <Icon name="pencil" />
            </Button>
          )}
          {canDelete && (
            <Button
              type="button"
              size="sm"
              variant="link"
              className="kk-comment__action kk-comment__action--danger kukatko-tap-target-touch"
              disabled={busy}
              aria-label={t('photo.comments.delete')}
              title={t('photo.comments.delete')}
              onClick={onDelete}
            >
              <Icon name="trash" />
            </Button>
          )}
        </div>
      )}
    </li>
  )
}

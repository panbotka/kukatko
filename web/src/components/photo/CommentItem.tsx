import { type SyntheticEvent, useState } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { PersonAvatar } from '../PersonAvatar'
import { formatDateTimeMinutes } from '../../lib/format'
import { formatRelativeTime } from '../../lib/relativeTime'
import { MAX_COMMENT_LENGTH, type PhotoComment } from '../../services/comments'

/** Props for {@link CommentItem}. */
export interface CommentItemProps {
  comment: PhotoComment
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
 * The body is rendered as text (React escapes it), never as HTML or markdown: the
 * backend stores exactly what was typed and parses nothing, so the client must not
 * either. `white-space: pre-wrap` in the stylesheet keeps the writer's own line
 * breaks without inventing any other syntax.
 *
 * The avatar beside it is {@link PersonAvatar}: an account that has said which
 * person of the library it is shows that person's cover photo, and everything
 * else — no link, or a linked person with no cover photo — shows the coloured
 * initial. That is where "linking an account publishes that face" becomes
 * visible, which is why the account page says so before the link is made.
 *
 * Editing happens **in place**, in a textarea that replaces the body — a modal for
 * fixing a typo would hide the conversation the remark belongs to. Deleting does
 * not happen here: the row only asks, and the thread above raises the confirmation,
 * so one dialog serves every row.
 */
export function CommentItem({
  comment,
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
  // rather than an empty name where a person used to be.
  const authorName =
    comment.author_name === '' ? t('photo.comments.unknownAuthor') : comment.author_name

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
    <li className="kk-comment">
      {/* A face when the author's account has said which person of the library
          it is *and* that person has a cover photo; the coloured initial — the
          common case — otherwise. */}
      <PersonAvatar name={authorName} photoUid={comment.author_photo_uid} />
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
            <textarea
              id={`kk-comment-edit-${comment.uid}`}
              className="form-control form-control-sm"
              rows={3}
              maxLength={MAX_COMMENT_LENGTH}
              value={draft}
              autoFocus
              disabled={busy}
              onChange={(event) => {
                setDraft(event.target.value)
              }}
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
          <p className="kk-comment__body">{comment.body}</p>
        )}

        {!editing && (canEdit || canDelete) && (
          <div className="kk-comment__actions">
            {canEdit && (
              <Button
                type="button"
                size="sm"
                variant="link"
                className="kk-comment__action"
                disabled={busy}
                onClick={() => {
                  setDraft(comment.body)
                }}
              >
                {t('photo.comments.edit')}
              </Button>
            )}
            {canDelete && (
              <Button
                type="button"
                size="sm"
                variant="link"
                className="kk-comment__action kk-comment__action--danger"
                disabled={busy}
                onClick={onDelete}
              >
                {t('photo.comments.delete')}
              </Button>
            )}
          </div>
        )}
      </div>
    </li>
  )
}

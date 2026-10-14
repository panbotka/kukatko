import { useTranslation } from 'react-i18next'
import { Link, useInRouterContext } from 'react-router-dom'

import { useProjectPath } from '../project/ProjectContext'
import { type CommentMention } from '../services/comments'

import { Icon } from './Icon'

/** Props for {@link MentionChip}. */
export interface MentionChipProps {
  /** The mentioned account's uid, as the token carries it. */
  uid: string
  /** The name the token was written with — shown when the account is gone. */
  fallbackName: string
  /**
   * The account as the server resolved it when the comment was read: its
   * current name and linked person. Undefined when the account no longer
   * exists (or the caller resolved nothing).
   */
  mention: CommentMention | undefined
  /** Whether the mentioned account is the reader's own, which draws it stronger. */
  isMe: boolean
}

/** The page of the person an account is linked to (its own path, outside a project). */
function personPath(subjectUid: string): string {
  return `/people/${encodeURIComponent(subjectUid)}`
}

/**
 * One `@`-mention as the reader sees it: a small pill with the `@` glyph and
 * the account's **current** name — the server resolves the uid on every read,
 * so a renamed account reads under its new name in every old comment. An
 * account linked to a person of the library links to that person's page (the
 * one place Kukátko has for somebody); an unlinked account is the pill alone,
 * with the name as its tooltip. The reader's own mention is emphasised, so
 * "where am I named" jumps out of a long thread. A mention of an account that
 * no longer exists keeps the name the token was written with, muted, and
 * links nowhere; so does one of an account that has left the project, which
 * reads „Bývalý člen projektu" instead of a name. It never links to user
 * administration.
 */
export function MentionChip({ uid, fallbackName, mention, isMe }: MentionChipProps) {
  const { t } = useTranslation()
  const inRouter = useInRouterContext()
  const projectPath = useProjectPath()
  // An account that has left the project is a former member: the server sends
  // no name, and the pill says so, muted and linking nowhere.
  const former = mention?.former === true
  const name = former ? t('formerMember') : (mention?.name ?? fallbackName)
  const classes = [
    'kk-mention',
    isMe ? 'kk-mention--me' : undefined,
    mention === undefined || former ? 'kk-mention--gone' : undefined,
  ]
    .filter(Boolean)
    .join(' ')
  const title =
    mention === undefined || former
      ? t('photo.comments.mention.gone', { name })
      : isMe
        ? t('photo.comments.mention.you', { name })
        : t('photo.comments.mention.of', { name })
  const content = (
    <>
      <Icon name="at" className="kk-mention__icon" />
      {name}
    </>
  )
  const subjectUid = mention?.subject_uid ?? null
  if (subjectUid === null) {
    return (
      <span className={classes} title={title} data-mention-uid={uid}>
        {content}
      </span>
    )
  }
  if (inRouter) {
    return (
      <Link
        to={projectPath(personPath(subjectUid))}
        className={classes}
        title={title}
        data-mention-uid={uid}
      >
        {content}
      </Link>
    )
  }
  return (
    <a
      href={projectPath(personPath(subjectUid))}
      className={classes}
      title={title}
      data-mention-uid={uid}
    >
      {content}
    </a>
  )
}

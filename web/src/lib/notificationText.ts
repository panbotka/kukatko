import type { TFunction } from 'i18next'

import type { NotificationParams, NotificationRecord } from '../services/notifications'

/** What one notification says: its headline and the line under it. */
export interface NotificationText {
  title: string
  body: string
}

/**
 * Says a notification in the reader's language.
 *
 * The server renders a notification's text once, in the instance's language
 * (Czech), and stores it — that text is what the push and the e-mail carried.
 * Beside it, it stores the facts the text was rendered from (`params`), so the
 * inbox, the bell and the notification's own page render the same message from
 * kind + params here, in whatever language the reader uses. A notification
 * without params (stored before they existed), of a kind or a variant this
 * build does not know, or lacking a fact the wording needs falls back to the
 * stored text, which is never wrong — only, perhaps, in the other language.
 *
 * A comment mention's body is the comment's own words, in no language, so it
 * is always the stored one.
 */
export function notificationText(notification: NotificationRecord, t: TFunction): NotificationText {
  const stored = { title: notification.title, body: notification.body }
  const params = notification.params
  if (params === null || params === undefined) {
    return stored
  }
  return renderKind(notification.kind, params, stored, t) ?? stored
}

/** Renders the kind's text from params, or undefined when it cannot. */
function renderKind(
  kind: string,
  params: NotificationParams,
  stored: NotificationText,
  t: TFunction,
): NotificationText | undefined {
  const untitled = t('notification.text.untitled')
  const somebody = t('notification.text.somebody')
  const name = params.name ?? somebody
  const title = params.title ?? untitled
  const album = params.album ?? untitled
  switch (kind) {
    case 'tagged':
      return renderTagged(params, album, t)
    case 'registration_pending':
      if (params.name === undefined) {
        return undefined
      }
      return {
        title: t('notification.text.registrationPending.title'),
        body: t('notification.text.registrationPending.body', { name: params.name }),
      }
    case 'album_featured':
      return {
        title: t('notification.text.albumFeatured.title', { album }),
        body: t('notification.text.albumFeatured.body', { album }),
      }
    case 'task_assigned':
      return {
        title: t('notification.text.taskAssigned.title', { name, title }),
        body: t('notification.text.taskAssigned.body'),
      }
    case 'comment_mention':
      return renderMention(params, name, title, stored, t)
    default:
      return undefined
  }
}

/** The tagged kind's three variants: all photos, one album's, the rest. */
function renderTagged(
  params: NotificationParams,
  album: string,
  t: TFunction,
): NotificationText | undefined {
  const count = params.count
  if (count === undefined || count < 1) {
    return undefined
  }
  switch (params.variant ?? '') {
    case '':
      return {
        title: t('notification.text.tagged.title', { count }),
        body: t('notification.text.tagged.body', { count }),
      }
    case 'album':
      return {
        title: t('notification.text.taggedAlbum.title', { count, album }),
        body: t('notification.text.taggedAlbum.body', { count, album }),
      }
    case 'other':
      return {
        title: t('notification.text.taggedOther.title', { count }),
        body: t('notification.text.taggedOther.body', { count }),
      }
    default:
      return undefined
  }
}

/** The mention kind: the headline names the thread's place, the body is the comment's. */
function renderMention(
  params: NotificationParams,
  name: string,
  title: string,
  stored: NotificationText,
  t: TFunction,
): NotificationText | undefined {
  switch (params.place) {
    case 'photo':
      return {
        title:
          params.title === undefined
            ? t('notification.text.commentMention.photo', { name })
            : t('notification.text.commentMention.photoTitled', { name, title }),
        body: stored.body,
      }
    case 'task':
      return {
        title: t('notification.text.commentMention.task', { name, title }),
        body: stored.body,
      }
    case 'album':
      return {
        title: t('notification.text.commentMention.album', { name, title }),
        body: stored.body,
      }
    default:
      return undefined
  }
}

import { useTranslation } from 'react-i18next'

import { formatDateTimeMinutes } from '../../lib/format'
import { formatRelativeTime } from '../../lib/relativeTime'
import { type NotificationRecord } from '../../services/notifications'

/**
 * The body of one inbox entry, shared by the bell's dropdown and the inbox
 * page: an unread marker, the title, the line under it and how long ago it
 * came. It renders no link of its own — the caller wraps it in whatever is
 * clickable there (a dropdown item, a list row).
 *
 * The unread marker is a dot a sighted reader sees and a word a screen reader
 * hears; a read notification carries neither, and its title drops from bold.
 */
export function NotificationRow({ notification }: { notification: NotificationRecord }) {
  const { t, i18n } = useTranslation()
  const unread = notification.read_at === null
  return (
    <span className={`kk-inbox-row${unread ? ' kk-inbox-row--unread' : ''}`}>
      <span className="kk-inbox-row__dot" aria-hidden="true" />
      <span className="kk-inbox-row__text">
        <span className="kk-inbox-row__title">
          {unread && <span className="visually-hidden">{`${t('inbox.unread')}: `}</span>}
          {notification.title}
        </span>
        {notification.body !== '' && (
          <span className="kk-inbox-row__body small">{notification.body}</span>
        )}
        <time
          className="kk-inbox-row__time small"
          dateTime={notification.created_at}
          title={formatDateTimeMinutes(notification.created_at, i18n.language)}
        >
          {formatRelativeTime(notification.created_at, i18n.language)}
        </time>
      </span>
    </span>
  )
}

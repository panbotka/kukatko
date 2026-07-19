import { useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import { useTranslation } from 'react-i18next'

import { useAnnouncement } from '../hooks/useAnnouncement'
import { readDismissedAnnouncement, writeDismissedAnnouncement } from '../lib/announcementDismissal'

import { Icon, type IconName } from './Icon'

/** The react-bootstrap Alert variant per announcement level. */
const LEVEL_VARIANT = { info: 'info', warning: 'warning' } as const

/** The decorative leading icon per announcement level. */
const LEVEL_ICON: Record<'info' | 'warning', IconName> = {
  info: 'info-circle',
  warning: 'exclamation-triangle',
}

/**
 * The instance-wide announcement banner shown to every signed-in user at the top
 * of the content area. It polls the current message (see {@link useAnnouncement})
 * and renders it as a dismissible {@link Alert} whose variant follows the level
 * (info / warning).
 *
 * Dismissal is keyed on the message's `updated_at`, persisted in localStorage: a
 * user who dismisses a message stops seeing *that* message, but a newly published
 * one (a fresh `updated_at`) reappears. The banner renders nothing while loading,
 * when nothing is published, or once the current message has been dismissed.
 *
 * Note: routes rendered outside the app shell (the immersive photo viewer,
 * slideshow, review game and duplicate-compare views) do not include this banner.
 */
export function AnnouncementBanner() {
  const { t } = useTranslation()
  const announcement = useAnnouncement()
  const [dismissedAt, setDismissedAt] = useState<string>(() => readDismissedAnnouncement())

  if (!announcement || announcement.message === '') {
    return null
  }
  const updatedAt = announcement.updated_at ?? ''
  if (updatedAt !== '' && dismissedAt === updatedAt) {
    return null
  }

  const level = announcement.level === 'warning' ? 'warning' : 'info'

  return (
    <Alert
      variant={LEVEL_VARIANT[level]}
      dismissible
      closeLabel={t('announcement.dismiss')}
      className="d-flex align-items-start gap-2"
      onClose={() => {
        writeDismissedAnnouncement(updatedAt)
        setDismissedAt(updatedAt)
      }}
    >
      <Icon name={LEVEL_ICON[level]} className="mt-1 flex-shrink-0" />
      <span className="text-break">{announcement.message}</span>
    </Alert>
  )
}

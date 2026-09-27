import { useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../auth/AuthContext'
import { useAnnouncement } from '../hooks/useAnnouncement'
import {
  announcementDismissalToken,
  readDismissedAnnouncement,
  writeDismissedAnnouncement,
} from '../lib/announcementDismissal'

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
 * Dismissal is keyed on the message's `updated_at` (or, lacking one, the message
 * itself — see {@link announcementDismissalToken}), persisted in localStorage
 * under the signed-in account: a user who dismisses a message stops seeing
 * *that* message, but a newly published one reappears, and another account on
 * the same browser still sees it. The banner renders nothing while loading,
 * when nothing is published, or once the current message has been dismissed.
 *
 * Note: routes rendered outside the app shell (the immersive photo viewer,
 * slideshow, review game and duplicate-compare views) do not include this banner.
 */
export function AnnouncementBanner() {
  const { t } = useTranslation()
  const { user } = useAuth()
  const uid = user?.uid
  const announcement = useAnnouncement()
  // The dismissal made in this mount, so closing the banner sticks even where
  // storage cannot remember it. Tagged with its account: it never hides the
  // banner from whoever signs in next.
  const [dismissed, setDismissed] = useState<{ user: string | undefined; token: string } | null>(
    null,
  )

  if (!announcement || announcement.message === '') {
    return null
  }
  const token = announcementDismissalToken(announcement)
  const dismissedHere = dismissed !== null && dismissed.user === uid && dismissed.token === token
  if (dismissedHere || readDismissedAnnouncement(uid) === token) {
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
        writeDismissedAnnouncement(uid, token)
        setDismissed({ user: uid, token })
      }}
    >
      <Icon name={LEVEL_ICON[level]} className="mt-1 flex-shrink-0" />
      <span className="text-break">{announcement.message}</span>
    </Alert>
  )
}

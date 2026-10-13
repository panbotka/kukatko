import { useContext, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../auth/AuthContext'
import { useAnnouncement } from '../hooks/useAnnouncement'
import {
  announcementDismissalToken,
  readDismissedAnnouncement,
  writeDismissedAnnouncement,
} from '../lib/announcementDismissal'
import { ProjectContext } from '../project/ProjectContext'

import { Icon, type IconName } from './Icon'

/** The react-bootstrap Alert variant per announcement level. */
const LEVEL_VARIANT = { info: 'info', warning: 'warning' } as const

/** The decorative leading icon per announcement level. */
const LEVEL_ICON: Record<'info' | 'warning', IconName> = {
  info: 'info-circle',
  warning: 'exclamation-triangle',
}

/**
 * The project's announcement banner shown to every member at the top of the
 * content area. It polls the message of the project the app works in (see
 * {@link useAnnouncement}, keyed by the project's slug, so another project's
 * banner never shows) and renders it as a dismissible {@link Alert} whose
 * variant follows the level (info / warning).
 *
 * Dismissal is keyed on the message's `updated_at` (or, lacking one, the message
 * itself — see {@link announcementDismissalToken}), persisted in localStorage
 * under the signed-in account and the project: a user who dismisses a message
 * stops seeing *that* message, but a newly published one reappears, another
 * account on the same browser still sees it, and closing one project's banner
 * leaves every other project's alone. The banner renders nothing while loading,
 * when nothing is published, or once the current message has been dismissed.
 *
 * Note: routes rendered outside the app shell (the immersive photo viewer,
 * slideshow, review game and duplicate-compare views) do not include this banner.
 */
export function AnnouncementBanner() {
  const { t } = useTranslation()
  const { user } = useAuth()
  const uid = user?.uid
  // Read without useProject: the banner may render outside a ProjectProvider
  // (tests), where there is simply no project and so no banner.
  const slug = useContext(ProjectContext)?.project?.slug ?? null
  const announcement = useAnnouncement(slug)
  // The dismissal made in this mount, so closing the banner sticks even where
  // storage cannot remember it. Tagged with its account and project: it never
  // hides the banner from whoever signs in next, nor another project's.
  const [dismissed, setDismissed] = useState<{
    user: string | undefined
    slug: string
    token: string
  } | null>(null)

  if (slug === null || !announcement || announcement.message === '') {
    return null
  }
  const token = announcementDismissalToken(announcement)
  const dismissedHere =
    dismissed !== null &&
    dismissed.user === uid &&
    dismissed.slug === slug &&
    dismissed.token === token
  if (dismissedHere || readDismissedAnnouncement(uid, slug) === token) {
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
        writeDismissedAnnouncement(uid, slug, token)
        setDismissed({ user: uid, slug, token })
      }}
    >
      <Icon name={LEVEL_ICON[level]} className="mt-1 flex-shrink-0" />
      <span className="text-break">{announcement.message}</span>
    </Alert>
  )
}

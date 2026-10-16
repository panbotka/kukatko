import { useContext } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { avatarInitial, avatarTone } from '../lib/avatarIdentity'
import { LIBRARY_PATH } from '../lib/libraryView'
import { ProjectContext } from '../project/ProjectContext'
import { projectPathFor } from '../project/paths'
import { useLeaveProject } from '../project/useLeaveProject'

import { Icon } from './Icon'
import { PROJECTS_ITEM } from './navItems'

/**
 * Where the reader stands, at the leading edge of the bar: the current
 * project's mark — a small square with its initial, the chooser's stand-in on
 * the same colour drawn from the name — and the door to the project switch.
 *
 * **Clicking the mark switches.** When there is somewhere else to go
 * ({@link useLeaveProject}: a second enterable project, or the system
 * administrator) it leads to the chooser at `/projects` — the very destination
 * of the account menu's "Switch library" entry ({@link PROJECTS_ITEM}), the one
 * place a project is picked. An account that can enter only this project is
 * offered no switch, so the mark then links to the project's library the way a
 * brand link goes home, and its label names only the project: nothing promises
 * a chooser that would list a single project. No caret and no menu either way.
 *
 * **Only the mark in the bar.** The project's name is not drawn there — it took
 * width in a tight row whose "Library" item already leads home — but it stays
 * the link's accessible name and tooltip (`aria-label`/`title`). The left
 * sidebar has the room, so it passes `withName` and the name follows the mark.
 * Folded to its icon rail the sidebar passes `compact` instead: the mark alone
 * as in the bar, and outside a project a house glyph standing in for the
 * neutral title, which would not fit the rail's width (the title stays the
 * link's name and tooltip).
 *
 * **Outside a project** (an instance page, or a `/p/<slug>` the account may
 * not enter — see `useNeutralChrome`) the context's project is only the entry
 * rule's fallback, and naming it would claim the page for it. The bar then
 * wears the neutral app title instead, linking to `/`, where the entry rule
 * picks the project (or the chooser when it cannot). Outside a provider (an
 * isolated component test) and for an account with no project at all nothing
 * is rendered.
 */
export function ProjectIdentity({
  withName = false,
  compact = false,
}: {
  withName?: boolean
  compact?: boolean
}) {
  const { t } = useTranslation()
  const ctx = useContext(ProjectContext)
  const leave = useLeaveProject()
  if (ctx === null) {
    return null
  }
  if (!ctx.inAddress) {
    return (
      <Link
        to="/"
        title={t('projectIdentity.neutralTitle')}
        className="kukatko-tap-target kk-project-identity"
        data-testid="project-identity"
      >
        {compact ? (
          <>
            <Icon name="house" />
            <span className="visually-hidden">{t('projectIdentity.neutral')}</span>
          </>
        ) : (
          <span className="kk-project-identity__name">{t('projectIdentity.neutral')}</span>
        )}
      </Link>
    )
  }
  const current = ctx.project
  if (current === null) {
    return null
  }
  const label = leave.offered
    ? t('projectIdentity.switchTitle', { name: current.name })
    : current.name
  return (
    <Link
      to={leave.offered ? PROJECTS_ITEM.to : projectPathFor(current.slug, LIBRARY_PATH)}
      title={label}
      aria-label={label}
      className="kukatko-tap-target kk-project-identity"
      data-testid="project-identity"
    >
      <span
        className={`kk-avatar kk-avatar--sm kk-project-identity__mark kk-avatar--tone-${avatarTone(current.name)}`}
        aria-hidden="true"
      >
        {avatarInitial(current.name)}
      </span>
      {withName && <span className="kk-project-identity__name">{current.name}</span>}
    </Link>
  )
}

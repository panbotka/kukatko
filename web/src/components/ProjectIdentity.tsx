import { useContext } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { avatarInitial, avatarTone } from '../lib/avatarIdentity'
import { LIBRARY_PATH } from '../lib/libraryView'
import { ProjectContext } from '../project/ProjectContext'
import { projectPathFor } from '../project/paths'

/**
 * Where the reader stands, at the leading edge of the bar: the current
 * project as a calm identity — a small square with its initial (the chooser's
 * stand-in, on the same colour drawn from the name) and its name — that links
 * to the project's library, the way a brand link goes home.
 *
 * It is deliberately **not a switcher**: no caret, no menu, no other project's
 * name or badge. Switching is leaving — the account menu's "Switch library"
 * entry ({@link useLeaveProject}) leads to the chooser at `/projects`, the one
 * place a project is picked, and that is where the other projects' unread
 * counts are shown, one per tile.
 *
 * **Outside a project** (an instance page, or a `/p/<slug>` the account may
 * not enter — see `useNeutralChrome`) the context's project is only the entry
 * rule's fallback, and naming it would claim the page for it. The bar then
 * wears the neutral app title instead, linking to `/`, where the entry rule
 * picks the project (or the chooser when it cannot). Outside a provider (an
 * isolated component test) and for an account with no project at all nothing
 * is rendered.
 *
 * **Width.** The name is the one item of the bar that may give way: the
 * stylesheet lets it shrink and ellipsise (`.kk-project-identity`), with the
 * whole name in the `title`, so a long project name never pushes the row past
 * its container — see `docs/FRONTEND.md` for the measurements.
 */
export function ProjectIdentity() {
  const { t } = useTranslation()
  const ctx = useContext(ProjectContext)
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
        <span className="kk-project-identity__name">{t('projectIdentity.neutral')}</span>
      </Link>
    )
  }
  const current = ctx.project
  if (current === null) {
    return null
  }
  return (
    <Link
      to={projectPathFor(current.slug, LIBRARY_PATH)}
      title={t('projectIdentity.title', { name: current.name })}
      className="kukatko-tap-target kk-project-identity"
      data-testid="project-identity"
    >
      <span
        className={`kk-avatar kk-avatar--sm kk-project-identity__mark kk-avatar--tone-${avatarTone(current.name)}`}
        aria-hidden="true"
      >
        {avatarInitial(current.name)}
      </span>
      <span className="kk-project-identity__name">{current.name}</span>
    </Link>
  )
}

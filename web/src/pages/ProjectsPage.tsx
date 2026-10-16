import { useId, useState, type ReactNode } from 'react'
import Button from 'react-bootstrap/Button'
import Container from 'react-bootstrap/Container'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { EmptyState } from '../components/EmptyState'
import { Icon } from '../components/Icon'
import { SYSTEM_PROJECTS_PATH } from '../components/navItems'
import { ProjectUnreadBadge } from '../components/notifications/ProjectUnreadBadge'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { avatarInitial, avatarTone } from '../lib/avatarIdentity'
import { useProjectUnread } from '../notifications/NotificationInboxContext'
import { chooserSections } from '../project/chooserOrder'
import { useProject } from '../project/ProjectContext'
import { isEnterable, projectPathFor } from '../project/paths'
import type { ProjectMembership } from '../services/auth'

/** Where a maintainer manages the instance's projects. */
export const PROJECT_ADMIN_PATH = SYSTEM_PROJECTS_PATH

/**
 * `/projects` — the project chooser, a "who's watching" picker: one large
 * picture tile per membership, the library you step into.
 *
 * It is where `/` and every old address land when the entry rule has no single
 * project to pick (several projects and none remembered, or none at all), and
 * where the header switcher's „Všechny projekty…" leads. It is signed-in but
 * outside any project, so it has no project `Layout`; the ways out an account
 * needs whatever it belongs to — the account page, sign-out and, for a
 * maintainer, the project administration — sit in a quiet row above the tiles.
 *
 * Active projects come first, the most recently visited at the head, the rest
 * by name (`project/chooserOrder.ts`); deactivated ones (only a maintainer has
 * any) wait in a collapsed „Vypnuté projekty" section below. No membership at
 * all is an empty state asking for an invitation.
 */
export function ProjectsPage() {
  const { t, i18n } = useTranslation()
  const { projects } = useProject()
  const { isMaintainer, logout } = useAuth()
  const navigate = useNavigate()
  useDocumentTitle(t('projects.title'))
  const { active, inactive } = chooserSections(projects, i18n.language)

  async function handleLogout() {
    await logout()
    void navigate('/login', { replace: true })
  }

  return (
    <Container as="main" className="kukatko-main kk-chooser">
      <nav className="kk-chooser__actions" aria-label={t('projects.actions')}>
        {isMaintainer && (
          <Link
            to={PROJECT_ADMIN_PATH}
            title={t('projects.manageTitle')}
            className="kk-chooser__action kukatko-tap-target"
          >
            <Icon name="gear" />
            {t('projects.manage')}
          </Link>
        )}
        <Link to="/account" className="kk-chooser__action kukatko-tap-target">
          <Icon name="person-circle" />
          {t('projects.account')}
        </Link>
        <Button
          variant="link"
          className="kk-chooser__action kukatko-tap-target"
          onClick={() => {
            void handleLogout()
          }}
        >
          <Icon name="box-arrow-right" />
          {t('projects.logout')}
        </Button>
      </nav>
      <h1 className="kk-chooser__heading">{t('projects.heading')}</h1>
      {projects.length === 0 ? (
        <EmptyState icon={<Icon name="folder2-open" />} title={t('projects.empty')} />
      ) : (
        <>
          {active.length > 0 && (
            <ul className="kk-chooser__grid">
              {active.map((membership) => (
                <li key={membership.uid} className="kk-chooser__cell">
                  <ProjectTile membership={membership} />
                </li>
              ))}
            </ul>
          )}
          {inactive.length > 0 && (
            <details className="kk-chooser__off" data-testid="projects-inactive">
              <summary className="kk-chooser__off-summary">
                <Icon name="chevron-right" className="kk-chooser__off-chevron" />
                {t('projects.inactiveSection', { count: inactive.length })}
              </summary>
              <ul className="kk-chooser__grid kk-chooser__grid--off">
                {inactive.map((membership) => (
                  <li key={membership.uid} className="kk-chooser__cell">
                    <ProjectTile membership={membership} />
                  </li>
                ))}
              </ul>
            </details>
          )}
        </>
      )}
    </Container>
  )
}

/**
 * The picture of a project: its cover thumbnail when the server named one and
 * it loads, otherwise the project's initial on a colour drawn from its name —
 * the same stand-in a person without a picture gets.
 */
function ProjectPicture({ membership }: { membership: ProjectMembership }) {
  const cover = membership.cover_thumb_url ?? ''
  // The URL the image failed on, so a new cover after a refresh gets its try.
  const [failed, setFailed] = useState<string | null>(null)
  if (cover !== '' && failed !== cover) {
    return (
      <img
        className="kk-chooser__photo"
        src={cover}
        alt=""
        loading="lazy"
        decoding="async"
        data-testid="project-cover"
        onError={() => {
          setFailed(cover)
        }}
      />
    )
  }
  return (
    <span
      className={`kk-chooser__initial kk-avatar--tone-${avatarTone(membership.name)}`}
      data-testid="project-placeholder"
    >
      {avatarInitial(membership.name)}
    </span>
  )
}

/**
 * One membership on the chooser. An enterable one — approved, in an active
 * project — is a single link to the project's home: picture, name, the
 * optional one-line description and the account's role in it, the role kept
 * quiet. A membership still waiting for approval, or of a deactivated project,
 * is the same tile without the link, saying why.
 */
function ProjectTile({ membership }: { membership: ProjectMembership }) {
  const { t } = useTranslation()
  const unreadOf = useProjectUnread()
  const nameId = useId()
  const metaId = useId()
  const enterable = isEnterable(membership)
  // Why a tile cannot be entered: the project is off (whatever the membership),
  // else the membership still waits for a project admin.
  const blocked = !membership.active ? 'unavailable' : !membership.approved ? 'pending' : null
  const description = membership.description?.trim() ?? ''
  const role = t(`roles.${membership.role}`)

  const body: ReactNode = (
    <>
      <span className="kk-chooser__picture">
        <ProjectPicture membership={membership} />
        {/* Only an enterable project is ever counted (the server names no
            other), so the pill always sits on a tile the reader can open. */}
        <ProjectUnreadBadge count={unreadOf(membership.uid)} className="kk-chooser__unread" />
      </span>
      <span id={nameId} className="kk-chooser__name" title={membership.name}>
        {membership.name}
      </span>
      <span id={metaId} className="kk-chooser__meta">
        {description !== '' && (
          <span className="kk-chooser__description" title={description}>
            {description}
          </span>
        )}
        <span className="kk-chooser__role">
          {blocked === null
            ? role
            : t(blocked === 'pending' ? 'projects.pendingRole' : 'projects.unavailableRole', {
                role,
              })}
        </span>
      </span>
    </>
  )

  if (enterable) {
    return (
      <Link
        to={projectPathFor(membership.slug, '/')}
        className="kk-chooser__tile"
        aria-labelledby={nameId}
        aria-describedby={metaId}
        data-testid="project-card"
      >
        {body}
      </Link>
    )
  }
  return (
    <div
      className={`kk-chooser__tile kk-chooser__tile--blocked${
        blocked === 'unavailable' ? ' kk-chooser__tile--inactive' : ''
      }`}
      aria-disabled="true"
      title={t(blocked === 'pending' ? 'projects.pendingTitle' : 'projects.unavailableTitle')}
      data-testid="project-card"
    >
      {body}
    </div>
  )
}

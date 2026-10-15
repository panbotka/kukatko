import { useContext } from 'react'
import Badge from 'react-bootstrap/Badge'
import Dropdown from 'react-bootstrap/Dropdown'
import Nav from 'react-bootstrap/Nav'
import NavItem from 'react-bootstrap/NavItem'
import BsNavLink from 'react-bootstrap/NavLink'
import { useTranslation } from 'react-i18next'
import { Link, useLocation } from 'react-router-dom'

import { useProjectUnread } from '../notifications/NotificationInboxContext'
import { ProjectContext } from '../project/ProjectContext'
import { isEnterable, PROJECTS_PATH, routeAfterSwitch } from '../project/paths'
import type { ProjectMembership } from '../services/auth'

import { Icon } from './Icon'
import { PROJECTS_ITEM } from './navItems'
import { ProjectUnreadBadge } from './notifications/ProjectUnreadBadge'

/** The DOM id of the bar's switcher toggle (a dropdown needs one for ARIA). */
export const PROJECT_SWITCHER_ID = 'project-switcher'

/** Props of {@link ProjectSwitcher}. */
export interface ProjectSwitcherProps {
  /**
   * `bar` — the navbar's leading block: the project's name, a dropdown when the
   * account has more than one membership (`menu`), plain text otherwise. `drawer`
   * — the first section of the phone drawer: the name, then the other projects
   * and "All projects…" as ordinary drawer rows.
   */
  variant: 'bar' | 'drawer'
  /**
   * Whether the bar variant may open a menu. The shell passes `false` below its
   * `lg` breakpoint, where a navbar dropdown cannot float (react-bootstrap lays
   * it out in-flow) and the drawer's own section is where a phone switches.
   */
  menu?: boolean
  /** Called when a drawer row is followed, so the drawer can close. */
  onNavigate?: () => void
}

/**
 * The project the reader works in, and the way to another one.
 *
 * The current project is the {@link ProjectContext}'s; outside a provider (an
 * isolated component test) and for an account with no project at all nothing
 * is rendered. The other projects listed are the enterable ones — approved, in
 * an active project — each a link to {@link routeAfterSwitch}: the same page
 * when it is a list every project has, the destination's home otherwise. A link
 * rather than a click handler, so a project opens in a new tab like any page.
 * The last entry, "All projects…", is the chooser at `/projects`, which also
 * lists the memberships that cannot be entered and says why.
 *
 * **One membership is a fact, not a menu**: the name is plain text — no caret,
 * nothing to open — because a menu with only "All projects…" in it would be a
 * door to a page listing the very project you are in.
 *
 * **Outside a project** (an instance page, or a `/p/<slug>` the account may not
 * enter — see `useNeutralChrome`) there is no current project to name: the
 * context's is only the entry rule's fallback, and naming it would claim the
 * page for it. The switcher then becomes the way *into* a project — "Projekty",
 * with every enterable project (each opening at its home) and "All projects…"
 * — or, where no menu fits, a plain link to the chooser.
 *
 * **News elsewhere.** Every other project with unread notifications wears the
 * bell's red pill with its count ({@link ProjectUnreadBadge}) — in the menu, the
 * drawer's rows and the neutral list alike — because the bell speaks only for
 * the project the reader is in, and push and e-mail are off by default. In the
 * bar the name itself carries the other projects' total, so the news is seen
 * before the menu is opened (and on a phone, where the bar's name opens
 * nothing, before the drawer is).
 *
 * **Width.** In the bar the name is the one item that may give way: the
 * stylesheet lets it shrink and ellipsise (`.kk-project-switcher`), with the
 * whole name in the `title`, so a long project name never pushes the row past
 * its container — see `docs/FRONTEND.md` for the measurements.
 */
export function ProjectSwitcher({ variant, menu = true, onNavigate }: ProjectSwitcherProps) {
  const { t } = useTranslation()
  const ctx = useContext(ProjectContext)
  const { pathname } = useLocation()
  const unreadOf = useProjectUnread()

  if (ctx === null) {
    return null
  }
  if (!ctx.inAddress) {
    return (
      <NeutralSwitcher
        variant={variant}
        menu={menu}
        onNavigate={onNavigate}
        projects={ctx.projects.filter(isEnterable)}
        pathname={pathname}
      />
    )
  }
  const current = ctx.project
  if (current === null) {
    return null
  }
  const multiple = ctx.projects.length > 1
  const others = ctx.projects.filter((m) => m.slug !== current.slug && isEnterable(m))
  // The news waiting in the projects the reader is not in — the bell has the
  // current one's.
  const othersUnread = others.reduce((sum, m) => sum + unreadOf(m.uid), 0)
  // The page's own project: the address names it (an instance page took the
  // neutral branch above).
  const fromSlug = current.slug

  /** The address `membership`'s project opens at from where the reader stands. */
  function targetOf(membership: ProjectMembership): string {
    return routeAfterSwitch(pathname, fromSlug, membership.slug)
  }

  /** The other projects' unread total on the bar's name, saying whose it is. */
  function othersBadge(count: number) {
    return (
      <ProjectUnreadBadge
        count={count}
        label={t('projectSwitcher.othersUnread', { count })}
        className="kk-project-switcher__unread"
      />
    )
  }

  /** The role the account holds in `membership`'s project, as a badge. */
  function roleBadge(membership: ProjectMembership) {
    return (
      <Badge bg="secondary" className="kk-project-switcher__role">
        {t(`roles.${membership.role}`)}
      </Badge>
    )
  }

  if (variant === 'drawer') {
    return (
      <section className="kk-navdrawer__section" aria-labelledby="kk-navdrawer-project">
        <h2 id="kk-navdrawer-project" className="kk-navdrawer__heading">
          {t('projectSwitcher.heading')}
        </h2>
        <p className="kk-navdrawer__project" title={current.name}>
          {current.name}
        </p>
        {multiple &&
          others.map((membership) => (
            <Link
              key={membership.uid}
              to={targetOf(membership)}
              title={t('projectSwitcher.switchTo', { name: membership.name })}
              className="kk-navdrawer__link"
              onClick={onNavigate}
            >
              <Icon name="folder2-open" className="kk-navdrawer__icon" />
              <span className="kk-navdrawer__label">{membership.name}</span>
              <ProjectUnreadBadge count={unreadOf(membership.uid)} />
              {roleBadge(membership)}
            </Link>
          ))}
        {multiple && (
          <Link
            to={PROJECTS_PATH}
            title={t(PROJECTS_ITEM.titleKey)}
            className="kk-navdrawer__link"
            onClick={onNavigate}
          >
            <Icon name={PROJECTS_ITEM.icon} className="kk-navdrawer__icon" />
            <span className="kk-navdrawer__label">{t(PROJECTS_ITEM.labelKey)}</span>
          </Link>
        )}
      </section>
    )
  }

  if (!multiple || !menu) {
    return (
      <span
        className="navbar-text kk-project-switcher kk-project-switcher--static"
        title={t('projectSwitcher.current', { name: current.name })}
        data-testid="project-switcher"
      >
        <span className="kk-project-switcher__name">{current.name}</span>
        {multiple && othersBadge(othersUnread)}
      </span>
    )
  }

  return (
    <Nav className="kk-project-switcher" data-testid="project-switcher">
      <Dropdown as={NavItem} className="kk-project-switcher__item">
        <Dropdown.Toggle
          as={BsNavLink}
          id={PROJECT_SWITCHER_ID}
          title={t('projectSwitcher.title', { name: current.name })}
          className="kukatko-tap-target kk-project-switcher__toggle"
        >
          <span className="kk-project-switcher__name">{current.name}</span>
          {othersBadge(othersUnread)}
        </Dropdown.Toggle>
        <Dropdown.Menu>
          {others.map((membership) => (
            <Dropdown.Item
              key={membership.uid}
              as={Link}
              to={targetOf(membership)}
              title={t('projectSwitcher.switchTo', { name: membership.name })}
              className="kukatko-tap-target d-flex align-items-center gap-2"
            >
              <span className="flex-grow-1">{membership.name}</span>
              <ProjectUnreadBadge count={unreadOf(membership.uid)} />
              {roleBadge(membership)}
            </Dropdown.Item>
          ))}
          {others.length > 0 && <Dropdown.Divider />}
          <Dropdown.Item
            as={Link}
            to={PROJECTS_PATH}
            title={t(PROJECTS_ITEM.titleKey)}
            className="kukatko-tap-target d-flex align-items-center gap-2"
          >
            <Icon name={PROJECTS_ITEM.icon} />
            {t(PROJECTS_ITEM.labelKey)}
          </Dropdown.Item>
        </Dropdown.Menu>
      </Dropdown>
    </Nav>
  )
}

/** Props of {@link NeutralSwitcher}: the switcher's own, plus what it lists. */
interface NeutralSwitcherProps extends ProjectSwitcherProps {
  /** The projects the account may enter. */
  projects: ProjectMembership[]
  /** Where the reader stands (every destination is a project's home from here). */
  pathname: string
}

/**
 * The switcher of the neutral shell: no project's name, just the way into one.
 * See {@link ProjectSwitcher}.
 */
function NeutralSwitcher({
  variant,
  menu = true,
  onNavigate,
  projects,
  pathname,
}: NeutralSwitcherProps) {
  const { t } = useTranslation()
  const unreadOf = useProjectUnread()
  const total = projects.reduce((sum, m) => sum + unreadOf(m.uid), 0)

  /** The home of `membership`'s project. */
  function targetOf(membership: ProjectMembership): string {
    return routeAfterSwitch(pathname, '', membership.slug)
  }

  if (variant === 'drawer') {
    return (
      <section className="kk-navdrawer__section" aria-labelledby="kk-navdrawer-project">
        <h2 id="kk-navdrawer-project" className="kk-navdrawer__heading">
          {t('projectSwitcher.choose')}
        </h2>
        {projects.map((membership) => (
          <Link
            key={membership.uid}
            to={targetOf(membership)}
            title={t('projectSwitcher.switchTo', { name: membership.name })}
            className="kk-navdrawer__link"
            onClick={onNavigate}
          >
            <Icon name="folder2-open" className="kk-navdrawer__icon" />
            <span className="kk-navdrawer__label">{membership.name}</span>
            <ProjectUnreadBadge count={unreadOf(membership.uid)} />
          </Link>
        ))}
        <Link
          to={PROJECTS_PATH}
          title={t(PROJECTS_ITEM.titleKey)}
          className="kk-navdrawer__link"
          onClick={onNavigate}
        >
          <Icon name={PROJECTS_ITEM.icon} className="kk-navdrawer__icon" />
          <span className="kk-navdrawer__label">{t(PROJECTS_ITEM.labelKey)}</span>
        </Link>
      </section>
    )
  }

  if (!menu || projects.length === 0) {
    return (
      <Nav className="kk-project-switcher" data-testid="project-switcher">
        <Nav.Link
          as={Link}
          to={PROJECTS_PATH}
          title={t(PROJECTS_ITEM.titleKey)}
          className="kukatko-tap-target kk-project-switcher__toggle"
        >
          <span className="kk-project-switcher__name">{t('projectSwitcher.choose')}</span>
          <ProjectUnreadBadge
            count={total}
            label={t('projectSwitcher.othersUnread', { count: total })}
            className="kk-project-switcher__unread"
          />
        </Nav.Link>
      </Nav>
    )
  }

  return (
    <Nav className="kk-project-switcher" data-testid="project-switcher">
      <Dropdown as={NavItem} className="kk-project-switcher__item">
        <Dropdown.Toggle
          as={BsNavLink}
          id={PROJECT_SWITCHER_ID}
          title={t('projectSwitcher.chooseTitle')}
          className="kukatko-tap-target kk-project-switcher__toggle"
        >
          <span className="kk-project-switcher__name">{t('projectSwitcher.choose')}</span>
          <ProjectUnreadBadge
            count={total}
            label={t('projectSwitcher.othersUnread', { count: total })}
            className="kk-project-switcher__unread"
          />
        </Dropdown.Toggle>
        <Dropdown.Menu>
          {projects.map((membership) => (
            <Dropdown.Item
              key={membership.uid}
              as={Link}
              to={targetOf(membership)}
              title={t('projectSwitcher.switchTo', { name: membership.name })}
              className="kukatko-tap-target d-flex align-items-center gap-2"
            >
              <span className="flex-grow-1">{membership.name}</span>
              <ProjectUnreadBadge count={unreadOf(membership.uid)} />
            </Dropdown.Item>
          ))}
          <Dropdown.Divider />
          <Dropdown.Item
            as={Link}
            to={PROJECTS_PATH}
            title={t(PROJECTS_ITEM.titleKey)}
            className="kukatko-tap-target d-flex align-items-center gap-2"
          >
            <Icon name={PROJECTS_ITEM.icon} />
            {t(PROJECTS_ITEM.labelKey)}
          </Dropdown.Item>
        </Dropdown.Menu>
      </Dropdown>
    </Nav>
  )
}

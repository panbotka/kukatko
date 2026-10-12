import { useContext } from 'react'
import Badge from 'react-bootstrap/Badge'
import Dropdown from 'react-bootstrap/Dropdown'
import Nav from 'react-bootstrap/Nav'
import NavItem from 'react-bootstrap/NavItem'
import BsNavLink from 'react-bootstrap/NavLink'
import { useTranslation } from 'react-i18next'
import { Link, useLocation } from 'react-router-dom'

import { ProjectContext } from '../project/ProjectContext'
import { isEnterable, PROJECTS_PATH, routeAfterSwitch } from '../project/paths'
import type { ProjectMembership } from '../services/auth'

import { Icon } from './Icon'
import { PROJECTS_ITEM } from './navItems'

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
 * **Width.** In the bar the name is the one item that may give way: the
 * stylesheet lets it shrink and ellipsise (`.kk-project-switcher`), with the
 * whole name in the `title`, so a long project name never pushes the row past
 * its container — see `docs/FRONTEND.md` for the measurements.
 */
export function ProjectSwitcher({ variant, menu = true, onNavigate }: ProjectSwitcherProps) {
  const { t } = useTranslation()
  const ctx = useContext(ProjectContext)
  const { pathname } = useLocation()

  const current = ctx?.project ?? null
  if (ctx === null || current === null) {
    return null
  }
  const multiple = ctx.projects.length > 1
  const others = ctx.projects.filter((m) => m.slug !== current.slug && isEnterable(m))
  // The page's own project when the address names one; from an instance page
  // (`/account`) every destination is its home.
  const fromSlug = ctx.inAddress ? current.slug : ''

  /** The address `membership`'s project opens at from where the reader stands. */
  function targetOf(membership: ProjectMembership): string {
    return routeAfterSwitch(pathname, fromSlug, membership.slug)
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

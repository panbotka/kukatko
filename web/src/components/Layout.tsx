import { useEffect, useState } from 'react'
import Container from 'react-bootstrap/Container'
import Dropdown from 'react-bootstrap/Dropdown'
import Nav from 'react-bootstrap/Nav'
import Navbar from 'react-bootstrap/Navbar'
import NavDropdown from 'react-bootstrap/NavDropdown'
import NavItem from 'react-bootstrap/NavItem'
import BsNavLink from 'react-bootstrap/NavLink'
import { useTranslation } from 'react-i18next'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { useIsNarrowViewport } from '../hooks/useIsNarrowViewport'
import { LIBRARY_PATH } from '../lib/libraryView'

import { AnnouncementBanner } from './AnnouncementBanner'
import { Footer } from './Footer'
import { Icon } from './Icon'
import { JobQueueBadges } from './JobQueueBadges'
import { KeyboardShortcutsHelp } from './KeyboardShortcutsHelp'
import { MOBILE_MENU_ID, MobileNavDrawer } from './MobileNavDrawer'
import { MobileTabBar } from './MobileTabBar'
import {
  ACCOUNT_ITEM,
  BROWSE_GROUP,
  GOVERNANCE_GROUP,
  HELP_ITEM,
  LEADERBOARD_ITEM,
  type NavEntry,
  type NavGroup,
  OPERATIONS_GROUP,
  pathMatches,
  PRIMARY_ITEMS,
  REVIEW_ITEM,
  TOOLS_GROUP,
  UPLOAD_ITEM,
} from './navItems'
import { SearchCommand } from './search/SearchCommand'

/**
 * Application shell: a responsive top navbar (navigation and the
 * signed-in user menu) above the routed page content, and the global
 * {@link Footer} below it.
 *
 * The bar carries a deliberate hierarchy rather than one flat row of equals. The
 * everyday loop leads: **Knihovna**, **Alba**, **Štítky**, the "Procházet" browse
 * dropdown, the "Třídění" review game, and — as the one filled call-to-action —
 * **Nahrát**. A thin divider then sets off the quieter power-user and admin
 * cluster: the "Nástroje" tools dropdown (which now also holds the expand tool),
 * the maintainer-only "Provoz" operations dropdown (import, maintenance, system),
 * and the admin-or-higher "Správa" governance dropdown (users, audit). The
 * role-gated groups are hidden entirely from roles that cannot use any of their
 * children, and the divider only appears when at least one item follows it. Leading the bar — on
 * every viewport, outside the collapse — is the brand: the binoculars mark, joined by the „Kukátko"
 * wordmark wherever the row has the width for it, linking to the library at the site root. It is
 * what answers "which app is this" on a phone whose whole navigation is otherwise folded away, and
 * it is the one-tap way back to the start from anywhere. Beside it sits the
 * global {@link SearchCommand} — a field-shaped trigger that opens a keyboard-first
 * command palette (`/` or Cmd/Ctrl-K), kept outside the collapse so it stays
 * visible on a phone while the nav folds into the hamburger. The language switcher
 * is not in the bar: this instance is Czech, so the setting sits on the account
 * page rather than spending prime bar space. Every entry pairs an icon (for daily
 * recognition) with a `title` describing the action it performs. The items
 * themselves live in `navItems.ts`, so the phone menu below cannot drift from the
 * bar's set or its role gating.
 *
 * Below the `md` breakpoint the shell swaps that inline bar for two thumb-level
 * navigations. The {@link MobileTabBar} pins the everyday destinations to the
 * bottom edge so a phone user reaches them without opening the hamburger first,
 * and the hamburger opens the {@link MobileNavDrawer} — a proper Offcanvas panel
 * of labelled sections — instead of expanding every dropdown inline into one
 * cramped nested list. Both are decided in JS ({@link useIsNarrowViewport}), and
 * the drawer replaces the `Navbar.Collapse` rather than joining it, so neither
 * breakpoint ever carries two copies of the same links.
 */
export function Layout() {
  const { t } = useTranslation()
  const { user, canWrite, isAdmin, isMaintainer, logout } = useAuth()
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const narrow = useIsNarrowViewport()
  // The mobile navbar is controlled so it can be closed programmatically. Below
  // the `md` breakpoint the nav folds into a hamburger; react-bootstrap's
  // `collapseOnSelect` only collapses on a fired select event, which this bar's
  // mix of bare `NavLink`s and raw `Dropdown` items does not reliably emit — so
  // the menu stayed open over the page it had just navigated to.
  const [expanded, setExpanded] = useState(false)

  // Close the collapsed menu on every navigation, whatever control was tapped
  // (top-level link, group dropdown item, or user menu item all change the
  // path). On `md`+ the collapse is always shown, so this is a no-op there.
  useEffect(() => {
    setExpanded(false)
  }, [pathname])

  // A viewport that grows past the breakpoint (rotation, a resized window) drops
  // the drawer for the inline bar; leaving the state open would then mean the
  // desktop bar came back with an invisible menu still "expanded" behind it.
  useEffect(() => {
    if (!narrow) {
      setExpanded(false)
    }
  }, [narrow])

  function closeMenu() {
    setExpanded(false)
  }

  async function handleLogout() {
    // Logout dismisses via a handler, not a route change, so close explicitly.
    setExpanded(false)
    await logout()
    void navigate('/login', { replace: true })
  }

  /** True when any route in `items` is the current location. */
  function groupActive(items: NavEntry[]): boolean {
    return items.some((item) => pathMatches(pathname, item.to))
  }

  /**
   * Renders a top-level nav link: icon, visible label, action tooltip. The root
   * entry (the library) is matched exactly — without `end` its highlight would
   * be a prefix match and light up on every route. Passing `cta` styles the link
   * as the bar's single filled call-to-action (used for Upload).
   */
  function renderLink(entry: NavEntry, { cta = false }: { cta?: boolean } = {}) {
    return (
      <Nav.Link
        key={entry.to}
        as={NavLink}
        to={entry.to}
        end={entry.to === LIBRARY_PATH}
        title={t(entry.titleKey)}
        className={`kukatko-tap-target d-flex align-items-center gap-2${
          cta ? ' kukatko-nav-cta' : ''
        }`}
      >
        <Icon name={entry.icon} />
        {t(entry.labelKey)}
      </Nav.Link>
    )
  }

  /**
   * Renders a grouped dropdown. It is assembled from `Dropdown` rather than
   * `NavDropdown` because the latter spends the `title` prop on the toggle's
   * visible content, leaving no way to also set the `title` tooltip attribute.
   */
  function renderGroup(group: NavGroup) {
    return (
      <Dropdown as={NavItem}>
        <Dropdown.Toggle
          as={BsNavLink}
          id={group.id}
          active={groupActive(group.items)}
          title={t(group.titleKey)}
          className="kukatko-tap-target d-flex align-items-center gap-2"
        >
          <Icon name={group.icon} />
          {t(group.labelKey)}
        </Dropdown.Toggle>
        <Dropdown.Menu>
          {group.items.map((item) => (
            <Dropdown.Item
              key={item.to}
              as={NavLink}
              to={item.to}
              title={t(item.titleKey)}
              className="kukatko-tap-target d-flex align-items-center gap-2"
            >
              <Icon name={item.icon} />
              {t(item.labelKey)}
            </Dropdown.Item>
          ))}
        </Dropdown.Menu>
      </Dropdown>
    )
  }

  return (
    <>
      <Navbar
        expand="md"
        variant="dark"
        sticky="top"
        expanded={expanded}
        onToggle={(next) => {
          setExpanded(next)
        }}
        className="kukatko-navbar"
      >
        <Container>
          {/* The app's identity and its one-tap way home, outside the collapse so
              it is the first thing on every viewport. The mark is always there;
              the wordmark shows only where measurement says it fits, which is two
              separate bands rather than one — hence the four display utilities:
              hidden below `sm` (a 320px row is brand + search + hamburger and
              nothing else), shown `sm`–`md` (the phone/tablet bar, where the nav
              is folded into the drawer and there is room to spare), hidden again
              from `md` (the inline bar unfolds and the nav is what is starved for
              width — the wordmark alone pushes a 1024px bar into a horizontal
              scrollbar), and back from `xxl`, where even a full bar has slack.
              Because the text is therefore display-hidden most of the time, the
              link's accessible name is set explicitly rather than left to it. */}
          <Navbar.Brand
            as={Link}
            to={LIBRARY_PATH}
            title={t('nav.titles.home')}
            aria-label={t('nav.brand')}
            className="kukatko-navbar-brand"
          >
            <Icon name="binoculars-fill" className="kukatko-navbar-brand__mark" />
            <span className="kukatko-navbar-brand__wordmark d-none d-sm-inline d-md-none d-xxl-inline">
              {t('app.name')}
            </span>
          </Navbar.Brand>
          {/* Search leads the bar and stays outside the collapse, so it is always
              visible — on a phone it fills the row between the brand and the
              hamburger while the nav folds away. */}
          <SearchCommand />
          {/* The inline bar is the `md`+ navigation only: on a phone the same
              items are the drawer's, so rendering both would duplicate every
              link in the DOM. */}
          {!narrow && (
            <Navbar.Collapse id={MOBILE_MENU_ID}>
              <Nav className="me-auto">
                {/* The everyday loop, loudest first. Library (the homepage), Albums
                    and Labels are the always-visible entry points. */}
                {PRIMARY_ITEMS.map((entry) => renderLink(entry))}
                {/* The remaining browse destinations, one level down. */}
                {renderGroup(BROWSE_GROUP)}
                {/* The review game: editors only, and kept in plain sight. */}
                {canWrite && renderLink(REVIEW_ITEM)}
                {/* The review game's scoreboard: visible to every signed-in role. */}
                {renderLink(LEADERBOARD_ITEM)}
                {/* Adding photos is the loop's payoff: the bar's one filled CTA,
                    hidden from viewers. */}
                {canWrite && renderLink(UPLOAD_ITEM, { cta: true })}

                {/* A divider fences off the quieter power-user / admin cluster, but
                    only when the current role actually has something below it. */}
                {(canWrite || isAdmin) && (
                  <div className="kukatko-nav-divider" aria-hidden="true" />
                )}

                {/* Editor-only tools (expand, faces, duplicates, …); hidden from
                    viewers. */}
                {canWrite && renderGroup(TOOLS_GROUP)}
                {/* Maintainer-only operations (import, maintenance, system). */}
                {isMaintainer && renderGroup(OPERATIONS_GROUP)}
                {/* Governance (users, audit); admin or higher. */}
                {isAdmin && renderGroup(GOVERNANCE_GROUP)}
              </Nav>
              <Nav className="align-items-center">
                <KeyboardShortcutsHelp />
              </Nav>
              {user && (
                <Nav className="ms-md-3">
                  <NavDropdown
                    align="end"
                    title={user.display_name || user.username}
                    id="user-menu"
                  >
                    <NavDropdown.Item
                      as={Link}
                      to={ACCOUNT_ITEM.to}
                      title={t(ACCOUNT_ITEM.titleKey)}
                      className="d-flex align-items-center gap-2"
                    >
                      <Icon name={ACCOUNT_ITEM.icon} />
                      {t(ACCOUNT_ITEM.labelKey)}
                    </NavDropdown.Item>
                    <NavDropdown.Item
                      as={Link}
                      to={HELP_ITEM.to}
                      title={t(HELP_ITEM.titleKey)}
                      className="d-flex align-items-center gap-2"
                    >
                      <Icon name={HELP_ITEM.icon} />
                      {t(HELP_ITEM.labelKey)}
                    </NavDropdown.Item>
                    <NavDropdown.Divider />
                    <NavDropdown.Item
                      title={t('nav.titles.logout')}
                      className="d-flex align-items-center gap-2"
                      onClick={() => {
                        void handleLogout()
                      }}
                    >
                      <Icon name="box-arrow-right" />
                      {t('nav.logout')}
                    </NavDropdown.Item>
                  </NavDropdown>
                </Nav>
              )}
            </Navbar.Collapse>
          )}
          {/* The hamburger closes the phone row (`[brand] [search] [hamburger]`)
              rather than opening it: the brand is the orientation cue and belongs
              first, and a menu button sits better under the thumb on the trailing
              edge. It is `display: none` on `md`+, so the desktop bar is unmoved
              by where it stands in the DOM. */}
          <Navbar.Toggle aria-controls={MOBILE_MENU_ID} />
        </Container>
      </Navbar>
      {/* Phone only: the hamburger opens a real drawer of labelled sections
          rather than expanding the whole nav inline into the bar. */}
      {narrow && (
        <MobileNavDrawer
          show={expanded}
          onHide={closeMenu}
          onLogout={() => {
            void handleLogout()
          }}
        />
      )}
      <Container as="main" className="py-4 kukatko-main">
        <AnnouncementBanner />
        <Outlet />
      </Container>
      <Footer>
        <JobQueueBadges />
      </Footer>
      {/* Phone only: the everyday destinations as a fixed bottom strip, so they
          do not cost a hamburger open-then-tap. Renders nothing on `md`+, where
          the top bar above is already the whole navigation. */}
      <MobileTabBar />
    </>
  )
}

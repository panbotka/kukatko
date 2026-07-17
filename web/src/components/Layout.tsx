import type { ParseKeys } from 'i18next'
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
import { LIBRARY_PATH } from '../lib/libraryView'

import { Footer } from './Footer'
import { Icon, type IconName } from './Icon'
import { JobQueueBadges } from './JobQueueBadges'
import { KeyboardShortcutsHelp } from './KeyboardShortcutsHelp'

/**
 * A single navigable destination. `titleKey` names the *action* the entry
 * performs ("Show the albums"), not the destination noun — it becomes the
 * `title` tooltip, so it should tell a first-time user what clicking does while
 * the short visible label plus `icon` carry recognition for the daily users.
 */
interface NavEntry {
  to: string
  labelKey: ParseKeys
  titleKey: ParseKeys
  icon: IconName
}

/** A dropdown of related destinations, behind a labelled, icon-bearing toggle. */
interface NavGroup {
  id: string
  labelKey: ParseKeys
  titleKey: ParseKeys
  icon: IconName
  items: NavEntry[]
}

/**
 * The always-visible destinations, in the order the library is actually browsed:
 * everything, then by album, then by label. Available to every signed-in role.
 * The library is the homepage, so its entry points to the site root.
 */
const PRIMARY_ITEMS: NavEntry[] = [
  { to: LIBRARY_PATH, labelKey: 'nav.library', titleKey: 'nav.titles.library', icon: 'images' },
  { to: '/albums', labelKey: 'nav.albums', titleKey: 'nav.titles.albums', icon: 'collection' },
  { to: '/labels', labelKey: 'nav.labels', titleKey: 'nav.titles.labels', icon: 'tags' },
]

/** The "Procházet" (Browse) group: the less-travelled ways into the library. */
const BROWSE_GROUP: NavGroup = {
  id: 'nav-browse',
  labelKey: 'nav.browse',
  titleKey: 'nav.titles.browse',
  icon: 'compass',
  items: [
    {
      to: '/favorites',
      labelKey: 'nav.favorites',
      titleKey: 'nav.titles.favorites',
      icon: 'heart',
    },
    { to: '/people', labelKey: 'nav.people', titleKey: 'nav.titles.people', icon: 'people' },
    { to: '/places', labelKey: 'nav.places', titleKey: 'nav.titles.places', icon: 'geo-alt' },
    { to: '/map', labelKey: 'nav.map', titleKey: 'nav.titles.map', icon: 'map' },
  ],
}

/**
 * The editor-only "Nástroje" (Tools) group, gated behind `canWrite`. It gathers
 * the power-user curation tools that a day-to-day browser rarely reaches for —
 * starting with "Rozšířit" (expand), which grows an album or label with similar
 * photos. Keeping expand here, rather than shouting for attention next to Alba /
 * Štítky, is the whole point of Part 3: the everyday loop stays uncluttered while
 * the tools remain one visible dropdown away.
 */
const TOOLS_GROUP: NavGroup = {
  id: 'nav-tools',
  labelKey: 'nav.tools',
  titleKey: 'nav.titles.tools',
  icon: 'tools',
  items: [
    { to: '/expand', labelKey: 'nav.expand', titleKey: 'nav.titles.expand', icon: 'magic' },
    {
      to: '/faces',
      labelKey: 'nav.faceSearch',
      titleKey: 'nav.titles.faceSearch',
      icon: 'person-bounding-box',
    },
    {
      to: '/recognition',
      labelKey: 'nav.recognition',
      titleKey: 'nav.titles.recognition',
      icon: 'person-check',
    },
    {
      to: '/outliers',
      labelKey: 'nav.outliers',
      titleKey: 'nav.titles.outliers',
      icon: 'exclamation-triangle',
    },
    {
      to: '/duplicates',
      labelKey: 'nav.duplicates',
      titleKey: 'nav.titles.duplicates',
      icon: 'files',
    },
    { to: '/trash', labelKey: 'nav.trash', titleKey: 'nav.titles.trash', icon: 'trash' },
  ],
}

/** The admin-only "Správa" (Admin) group, gated behind `isAdmin`. */
const ADMIN_GROUP: NavGroup = {
  id: 'nav-admin',
  labelKey: 'nav.admin',
  titleKey: 'nav.titles.admin',
  icon: 'sliders',
  items: [
    {
      to: '/maintenance',
      labelKey: 'nav.maintenance',
      titleKey: 'nav.titles.maintenance',
      icon: 'wrench-adjustable',
    },
    { to: '/system', labelKey: 'nav.system', titleKey: 'nav.titles.system', icon: 'activity' },
    { to: '/users', labelKey: 'nav.users', titleKey: 'nav.titles.users', icon: 'person-gear' },
    { to: '/audit', labelKey: 'nav.audit', titleKey: 'nav.titles.audit', icon: 'clock-history' },
  ],
}

/**
 * The write-gated review game. Top-level rather than buried in "Nástroje":
 * tidying the library one question at a time is the app's most-used curation
 * loop, and a game nobody can find is a game nobody plays.
 */
const REVIEW_ITEM: NavEntry = {
  to: '/review',
  labelKey: 'nav.review',
  titleKey: 'nav.titles.review',
  icon: 'ui-checks',
}

/**
 * The write-gated upload entry. Adding photos is the everyday loop's payoff, so
 * it is not just top-level but the bar's one call-to-action: rendered as a filled
 * pill (see `renderLink`'s `cta` option) so a non-technical user's eye lands on
 * "add photos" instead of treating it as just another link beside Import.
 */
const UPLOAD_ITEM: NavEntry = {
  to: '/upload',
  labelKey: 'nav.upload',
  titleKey: 'nav.titles.upload',
  icon: 'cloud-arrow-up',
}

/**
 * The import trigger, kept top-level and gated behind `canImport` rather than
 * `isAdmin`: the ai agent may import without being an administrator, so it lives
 * outside the admin-only group.
 */
const IMPORT_ITEM: NavEntry = {
  to: '/import',
  labelKey: 'nav.import',
  titleKey: 'nav.titles.import',
  icon: 'box-arrow-in-down',
}

/**
 * Reports whether `pathname` matches the given nav route, treating a route as
 * active for its detail sub-paths too (e.g. `/albums/ab12` activates `/albums`).
 * Used to light up the parent dropdown when any of its children is current.
 */
function pathMatches(pathname: string, route: string): boolean {
  return pathname === route || pathname.startsWith(`${route}/`)
}

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
 * the "Import" trigger, and the "Správa" admin dropdown. The role-gated groups
 * are hidden entirely from roles that cannot use any of their children, and the
 * divider only appears when at least one item follows it. Searching is not in the
 * bar — it lives on the library page and on `/search`. Neither is the language
 * switcher: this instance is Czech, so the setting sits on the account page rather
 * than spending prime bar space. Every entry pairs an icon (for daily recognition)
 * with a `title` describing the action it performs.
 */
export function Layout() {
  const { t } = useTranslation()
  const { user, canWrite, isAdmin, canImport, logout } = useAuth()
  const navigate = useNavigate()
  const { pathname } = useLocation()

  async function handleLogout() {
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
        bg="dark"
        variant="dark"
        sticky="top"
        collapseOnSelect
        className="kukatko-navbar"
      >
        <Container>
          <Navbar.Toggle aria-controls="main-navbar" />
          <Navbar.Collapse id="main-navbar">
            <Nav className="me-auto">
              {/* The everyday loop, loudest first. Library (the homepage), Albums
                  and Labels are the always-visible entry points. */}
              {PRIMARY_ITEMS.map((entry) => renderLink(entry))}
              {/* The remaining browse destinations, one level down. */}
              {renderGroup(BROWSE_GROUP)}
              {/* The review game: editors only, and kept in plain sight. */}
              {canWrite && renderLink(REVIEW_ITEM)}
              {/* Adding photos is the loop's payoff: the bar's one filled CTA,
                  hidden from viewers. */}
              {canWrite && renderLink(UPLOAD_ITEM, { cta: true })}

              {/* A divider fences off the quieter power-user / admin cluster, but
                  only when the current role actually has something below it. */}
              {(canWrite || canImport || isAdmin) && (
                <div className="kukatko-nav-divider" aria-hidden="true" />
              )}

              {/* Editor-only tools (expand, faces, duplicates, …); hidden from
                  viewers. */}
              {canWrite && renderGroup(TOOLS_GROUP)}
              {/* Import is reachable by admins and the ai agent. */}
              {canImport && renderLink(IMPORT_ITEM)}
              {/* Admin-only administration; hidden from non-admins. */}
              {isAdmin && renderGroup(ADMIN_GROUP)}
            </Nav>
            <Nav className="align-items-center">
              <KeyboardShortcutsHelp />
            </Nav>
            {user && (
              <Nav className="ms-md-3">
                <NavDropdown align="end" title={user.display_name || user.username} id="user-menu">
                  <NavDropdown.Item
                    as={Link}
                    to="/account"
                    title={t('nav.titles.account')}
                    className="d-flex align-items-center gap-2"
                  >
                    <Icon name="person-circle" />
                    {t('nav.account')}
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
        </Container>
      </Navbar>
      <Container as="main" className="py-4 kukatko-main">
        <Outlet />
      </Container>
      <Footer>
        <JobQueueBadges />
      </Footer>
    </>
  )
}

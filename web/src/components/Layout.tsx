import type { ParseKeys } from 'i18next'
import Container from 'react-bootstrap/Container'
import Nav from 'react-bootstrap/Nav'
import Navbar from 'react-bootstrap/Navbar'
import NavDropdown from 'react-bootstrap/NavDropdown'
import { useTranslation } from 'react-i18next'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'

import { SavedSearchesMenu } from './savedsearch/SavedSearchesMenu'
import { KeyboardShortcutsHelp } from './KeyboardShortcutsHelp'
import { LanguageSwitcher } from './LanguageSwitcher'
import { NavbarSearch } from './NavbarSearch'

/** A single navigable destination inside a grouped navbar dropdown. */
interface NavGroupItem {
  to: string
  labelKey: ParseKeys
}

/**
 * The "Procházet" (Browse) group: the everyday library destinations. These are
 * available to every signed-in role, so the whole group is always rendered.
 */
const BROWSE_ITEMS: NavGroupItem[] = [
  { to: '/library', labelKey: 'nav.library' },
  { to: '/favorites', labelKey: 'nav.favorites' },
  { to: '/albums', labelKey: 'nav.albums' },
  { to: '/labels', labelKey: 'nav.labels' },
  { to: '/people', labelKey: 'nav.people' },
  { to: '/places', labelKey: 'nav.places' },
  { to: '/map', labelKey: 'nav.map' },
]

/** The editor-only "Nástroje" (Tools) group, gated behind `canWrite`. */
const TOOLS_ITEMS: NavGroupItem[] = [
  { to: '/duplicates', labelKey: 'nav.duplicates' },
  { to: '/trash', labelKey: 'nav.trash' },
]

/** The admin-only "Správa" (Admin) group, gated behind `isAdmin`. */
const ADMIN_ITEMS: NavGroupItem[] = [
  { to: '/import', labelKey: 'nav.import' },
  { to: '/maintenance', labelKey: 'nav.maintenance' },
  { to: '/system', labelKey: 'nav.system' },
]

/**
 * Reports whether `pathname` matches the given nav route, treating a route as
 * active for its detail sub-paths too (e.g. `/albums/ab12` activates `/albums`).
 * Used to light up the parent dropdown when any of its children is current.
 */
function pathMatches(pathname: string, route: string): boolean {
  return pathname === route || pathname.startsWith(`${route}/`)
}

/**
 * Application shell: a responsive top navbar (brand, grouped navigation
 * dropdowns, language switcher, and the signed-in user menu) above the routed
 * page content. Related destinations are collapsed into "Procházet" (Browse),
 * an editor-only "Nástroje" (Tools) and an admin-only "Správa" (Admin) menu so
 * the bar stays scannable; role-gated groups are hidden entirely from roles
 * that cannot use any of their children.
 */
export function Layout() {
  const { t } = useTranslation()
  const { user, canWrite, isAdmin, logout } = useAuth()
  const navigate = useNavigate()
  const { pathname } = useLocation()

  async function handleLogout() {
    await logout()
    void navigate('/login', { replace: true })
  }

  /** True when any route in `items` is the current location. */
  function groupActive(items: NavGroupItem[]): boolean {
    return items.some((item) => pathMatches(pathname, item.to))
  }

  /** Renders a grouped dropdown of role-agnostic nav destinations. */
  function renderGroup(id: string, titleKey: ParseKeys, items: NavGroupItem[]) {
    return (
      <NavDropdown title={t(titleKey)} id={id} active={groupActive(items)}>
        {items.map((item) => (
          <NavDropdown.Item
            key={item.to}
            as={NavLink}
            to={item.to}
            className="kukatko-tap-target d-flex align-items-center"
          >
            {t(item.labelKey)}
          </NavDropdown.Item>
        ))}
      </NavDropdown>
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
          <Navbar.Brand as={Link} to="/">
            {t('app.name')}
          </Navbar.Brand>
          <Navbar.Toggle aria-controls="main-navbar" />
          <Navbar.Collapse id="main-navbar">
            <Nav className="me-auto">
              {/* Home stays reachable via the brand link; Browse groups the
                  everyday library destinations for all roles. */}
              {renderGroup('nav-browse', 'nav.browse', BROWSE_ITEMS)}
              {/* Search and Upload stay prominent as top-level entries. */}
              <Nav.Link as={NavLink} to="/search">
                {t('nav.search')}
              </Nav.Link>
              {/* Upload is a write action: hidden from viewers. */}
              {canWrite && (
                <Nav.Link as={NavLink} to="/upload">
                  {t('nav.upload')}
                </Nav.Link>
              )}
              {/* Editor-only tools; the whole group is hidden from viewers. */}
              {canWrite && renderGroup('nav-tools', 'nav.tools', TOOLS_ITEMS)}
              {/* Admin-only administration; hidden from non-admins. */}
              {isAdmin && renderGroup('nav-admin', 'nav.admin', ADMIN_ITEMS)}
            </Nav>
            <NavbarSearch />
            <Nav>
              <SavedSearchesMenu />
            </Nav>
            <Nav className="align-items-center">
              <KeyboardShortcutsHelp />
            </Nav>
            <LanguageSwitcher />
            {user && (
              <Nav className="ms-md-3">
                <NavDropdown align="end" title={user.display_name || user.username} id="user-menu">
                  <NavDropdown.Item as={Link} to="/account">
                    {t('nav.account')}
                  </NavDropdown.Item>
                  <NavDropdown.Divider />
                  <NavDropdown.Item
                    onClick={() => {
                      void handleLogout()
                    }}
                  >
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
    </>
  )
}

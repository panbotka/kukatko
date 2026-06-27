import Container from 'react-bootstrap/Container'
import Nav from 'react-bootstrap/Nav'
import Navbar from 'react-bootstrap/Navbar'
import NavDropdown from 'react-bootstrap/NavDropdown'
import { useTranslation } from 'react-i18next'
import { Link, NavLink, Outlet, useNavigate } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'

import { LanguageSwitcher } from './LanguageSwitcher'
import { NavbarSearch } from './NavbarSearch'

/**
 * Application shell: a responsive top navbar (brand, navigation, language
 * switcher, and the signed-in user menu) above the routed page content.
 * Write-only navigation is hidden from viewers; disabled nav items are
 * placeholders for milestones not yet implemented.
 */
export function Layout() {
  const { t } = useTranslation()
  const { user, canWrite, isAdmin, logout } = useAuth()
  const navigate = useNavigate()

  async function handleLogout() {
    await logout()
    void navigate('/login', { replace: true })
  }

  return (
    <>
      <Navbar expand="md" bg="dark" variant="dark" sticky="top" collapseOnSelect>
        <Container>
          <Navbar.Brand as={Link} to="/">
            {t('app.name')}
          </Navbar.Brand>
          <Navbar.Toggle aria-controls="main-navbar" />
          <Navbar.Collapse id="main-navbar">
            <Nav className="me-auto">
              <Nav.Link as={NavLink} to="/" end>
                {t('nav.home')}
              </Nav.Link>
              <Nav.Link as={NavLink} to="/library">
                {t('nav.library')}
              </Nav.Link>
              <Nav.Link as={NavLink} to="/favorites">
                {t('nav.favorites')}
              </Nav.Link>
              <Nav.Link as={NavLink} to="/albums">
                {t('nav.albums')}
              </Nav.Link>
              <Nav.Link as={NavLink} to="/labels">
                {t('nav.labels')}
              </Nav.Link>
              <Nav.Link as={NavLink} to="/search">
                {t('nav.search')}
              </Nav.Link>
              <Nav.Link as={NavLink} to="/people">
                {t('nav.people')}
              </Nav.Link>
              <Nav.Link as={NavLink} to="/map">
                {t('nav.map')}
              </Nav.Link>
              {/* Write actions are gated by role: hidden from viewers. */}
              {canWrite && (
                <Nav.Link as={NavLink} to="/upload">
                  {t('nav.upload')}
                </Nav.Link>
              )}
              {/* Import/migration administration is admin-only. */}
              {isAdmin && (
                <Nav.Link as={NavLink} to="/import">
                  {t('nav.import')}
                </Nav.Link>
              )}
            </Nav>
            <NavbarSearch />
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
      <Container as="main" className="py-4">
        <Outlet />
      </Container>
    </>
  )
}

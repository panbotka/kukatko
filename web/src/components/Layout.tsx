import Container from 'react-bootstrap/Container'
import Nav from 'react-bootstrap/Nav'
import Navbar from 'react-bootstrap/Navbar'
import { useTranslation } from 'react-i18next'
import { Link, NavLink, Outlet } from 'react-router-dom'

import { LanguageSwitcher } from './LanguageSwitcher'

/**
 * Application shell: a responsive top navbar (brand, navigation placeholders,
 * language switcher) above the routed page content. Disabled nav items are
 * placeholders for milestones not yet implemented.
 */
export function Layout() {
  const { t } = useTranslation()

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
              <Nav.Link as={NavLink} to="/library" disabled>
                {t('nav.library')}
              </Nav.Link>
              <Nav.Link as={NavLink} to="/map" disabled>
                {t('nav.map')}
              </Nav.Link>
            </Nav>
            <LanguageSwitcher />
          </Navbar.Collapse>
        </Container>
      </Navbar>
      <Container as="main" className="py-4">
        <Outlet />
      </Container>
    </>
  )
}

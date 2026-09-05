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
import { useCapabilities } from '../capabilities/CapabilitiesContext'
import { useIsNarrowViewport } from '../hooks/useIsNarrowViewport'
import { LIBRARY_PATH } from '../lib/libraryView'
import { formatVersion } from '../lib/version'

import { AnnouncementBanner } from './AnnouncementBanner'
import { Footer } from './Footer'
import { Icon } from './Icon'
import { JobQueueBadges } from './JobQueueBadges'
import { KeyboardShortcutsHelp } from './KeyboardShortcutsHelp'
import { MOBILE_MENU_ID, MobileNavDrawer } from './MobileNavDrawer'
import { MobileTabBar } from './MobileTabBar'
import {
  ACCOUNT_ITEM,
  adminItems,
  BROWSE_GROUP,
  HELP_ITEM,
  myPhotosItem,
  type NavEntry,
  type NavGroup,
  pathMatches,
  PRIMARY_ITEMS,
  REVIEW_ITEM,
  STATS_ITEM,
  TOOLS_GROUP,
  UPLOAD_ITEM,
} from './navItems'
import { SearchCommand } from './search/SearchCommand'
import { WelcomeModal } from './welcome/WelcomeModal'

/**
 * Application shell: a responsive top navbar (navigation and the
 * signed-in user menu) above the routed page content, and the global
 * {@link Footer} below it.
 *
 * The bar carries a deliberate hierarchy rather than one flat row of equals. The
 * everyday loop leads: **Knihovna**, **Alba**, **Štítky**, **Hledání**, the
 * "Procházet" browse dropdown, the "Třídění" review game, and — as the one filled
 * call-to-action — **Nahrát**. A thin divider then sets off the quieter power-user
 * cluster: the "Nástroje" tools dropdown (which now also holds the expand tool).
 * It is hidden entirely from roles that cannot use any of its children, and the
 * divider only appears when at least one item follows it.
 *
 * **Administration is not in the bar at all.** What used to be two more dropdowns
 * here — the maintainer-only "Provoz" (import, maintenance, system) and the
 * admin-or-higher "Správa" (users, audit) — is one "Správa" section inside the
 * user menu below, between the account block and sign-out. Those destinations
 * belong to the signed-in identity rather than to browsing the library, and the
 * two toggles were spending the row's scarcest resource: with both of them a
 * maintainer's inline bar overran its container below 1400px. The role gating is
 * unchanged and stays **per item** (see `adminItems`), so an admin who is not a
 * maintainer still reaches only users and audit.
 *
 * The bar deliberately carries **no logo and no wordmark**. Width is the scarce
 * resource here, not identity: for an editor or a maintainer the inline row of
 * items already ran past the container well into desktop widths, and the „Kukátko"
 * wordmark was only kept alive by four stacked display utilities because it fit
 * nowhere. Dropping the brand buys back the whole leading block. The way home
 * survives it on every viewport and does not depend on a logo: on `md`+ the first
 * item of the bar is **Knihovna**, the library at the site root, labelled and
 * `end`-matched; below `md` the same destination is the leading tab of the
 * {@link MobileTabBar}, permanently under the thumb. Both are one tap, exactly as
 * the mark was.
 *
 * Leading the bar — outside the collapse, so it survives the nav folding into the
 * hamburger — is the global {@link SearchCommand}: a compact icon button, since
 * all it does is open the command palette (`/` or Cmd/Ctrl-K). On a phone CSS
 * pairs it with the hamburger at the trailing edge rather than leaving it alone
 * on the left of a row the brand used to open. That circle is the *shortcut*, not
 * the entrance: it carries no label and states its chords only in a `title`, so
 * the labelled **Hledání** item beside Štítky is what actually makes search
 * findable — the bare glyph was the whole way in, and a phone never hovers.
 * Because the swap trades **Žebříček** (now inside "Procházet") for **Hledání**,
 * the widest role's bar comes out marginally *narrower* than before, which is what
 * keeps this change clear of the inline row's long-standing overflow.
 *
 * The language switcher is not in the bar: this instance is Czech, so the setting
 * sits on the account page rather than
 * spending prime bar space. Every entry pairs an icon (for daily recognition) with
 * a `title` describing the action it performs. The items themselves live in
 * `navItems.ts`, so the phone menu below cannot drift from the bar's set or its
 * role gating.
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
  // The build the server runs, printed at the foot of the user menu. It rides
  // along on the capabilities the shell already holds, so opening the menu costs
  // no request; `null` (nothing loaded yet, or the call failed) shows nothing.
  const version = formatVersion(useCapabilities().version)
  // The mobile navbar is controlled so it can be closed programmatically. Below
  // the `md` breakpoint the nav folds into a hamburger; react-bootstrap's
  // `collapseOnSelect` only collapses on a fired select event, which this bar's
  // mix of bare `NavLink`s and raw `Dropdown` items does not reliably emit — so
  // the menu stayed open over the page it had just navigated to.
  const [expanded, setExpanded] = useState(false)
  // "My photos", offered only to an account that has said which person of the
  // library it is. An entry that leads nowhere is worse than no entry, so an
  // unlinked account simply does not get one.
  const myPhotos = myPhotosItem(user?.subject_uid)
  // The user menu's "Správa" section, filtered per item rather than as a whole:
  // empty for a viewer or an editor, two entries for an admin, all five for a
  // maintainer.
  const admin = adminItems({ isAdmin, isMaintainer })

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
   * Renders one entry of the user menu: icon, label, action tooltip. A plain
   * `Link` rather than a `NavLink` — the menu is closed while you are on the page
   * it points at, so an active highlight in there would never be seen.
   */
  function renderMenuItem(entry: NavEntry) {
    return (
      <NavDropdown.Item
        key={entry.to}
        as={Link}
        to={entry.to}
        title={t(entry.titleKey)}
        className="d-flex align-items-center gap-2"
      >
        <Icon name={entry.icon} />
        {t(entry.labelKey)}
      </NavDropdown.Item>
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
          {/* Search leads the bar and stays outside the collapse, so it is always
              visible — on a phone it is one half of the row while the nav folds
              away into the drawer (CSS pairs it with the hamburger there). It is
              a bare icon button: it opens a dialog, so it does not need the width
              of something you can type into. */}
          <SearchCommand />
          {/* The inline bar is the `md`+ navigation only: on a phone the same
              items are the drawer's, so rendering both would duplicate every
              link in the DOM. */}
          {!narrow && (
            <Navbar.Collapse id={MOBILE_MENU_ID}>
              <Nav className="me-auto">
                {/* The everyday loop, loudest first. Library (the homepage), Albums,
                    Labels and Search are the always-visible entry points. */}
                {PRIMARY_ITEMS.map((entry) => renderLink(entry))}
                {/* The remaining browse destinations — saved searches and the
                    leaderboard among them — one level down. */}
                {renderGroup(BROWSE_GROUP)}
                {/* The review game: editors only, and kept in plain sight. */}
                {canWrite && renderLink(REVIEW_ITEM)}
                {/* Adding photos is the loop's payoff: the bar's one filled CTA,
                    hidden from viewers. */}
                {canWrite && renderLink(UPLOAD_ITEM, { cta: true })}

                {/* A divider fences off the quieter power-user cluster, but only
                    when the current role actually has something below it. */}
                {canWrite && <div className="kukatko-nav-divider" aria-hidden="true" />}

                {/* Editor-only tools (expand, faces, duplicates, …); hidden from
                    viewers. The administration that used to follow it here now
                    hangs off the user menu instead. */}
                {canWrite && renderGroup(TOOLS_GROUP)}
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
                    {renderMenuItem(ACCOUNT_ITEM)}
                    {/* The photos the signed-in person is on. It hangs off the
                        user menu because it is a fact about them, not another
                        way of browsing the library — and because the bar has no
                        room left. */}
                    {myPhotos && renderMenuItem(myPhotos)}
                    {/* The library statistics: open to every signed-in role, so
                        they hang off the user menu rather than a gated group. */}
                    {renderMenuItem(STATS_ITEM)}
                    {renderMenuItem(HELP_ITEM)}
                    {/* The build, as a plain line of text closing the account
                        block: an `ItemText` is not a menu item, so it neither
                        takes focus when arrowing through the menu nor invites a
                        click. The full form — with the commit — lives on the
                        help page. */}
                    {version && (
                      <NavDropdown.ItemText
                        className="kk-menu-version"
                        title={t('nav.versionTitle')}
                      >
                        {version}
                      </NavDropdown.ItemText>
                    )}
                    {/* Administration, once two dropdowns of its own in the bar:
                        one labelled section between the account block and
                        sign-out. Rendered only for the entries the role actually
                        clears, so a viewer sees neither heading nor divider. */}
                    {admin.length > 0 && (
                      <>
                        <NavDropdown.Divider />
                        <NavDropdown.Header className="kk-menu-section">
                          {t('nav.admin')}
                        </NavDropdown.Header>
                        {admin.map((entry) => renderMenuItem(entry))}
                      </>
                    )}
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
          {/* The hamburger closes the phone row (`[search] [hamburger]`): a menu
              button sits best under the thumb on the trailing edge. It is
              `display: none` on `md`+, so the desktop bar is unmoved by where it
              stands in the DOM. */}
          <Navbar.Toggle aria-controls={MOBILE_MENU_ID} label={t('nav.openMenu')} />
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
      {/* Everything that lives in normal flow — the routed page and the footer —
          shares one column, so the fixed timeline rail can be given a lane out of
          the page's own width instead of lying across its right edge (see
          `.kukatko-page` in `app.css`; the reservation follows the rail being
          mounted, so a page without one is untouched). The navbar stays outside
          it: the rail comes to rest below the bar and never reaches it. */}
      <div className="kukatko-page">
        {/* A phone pays for its top padding out of the first screen — on the
            library that gap sits directly above the photographs — so it gets one
            step less of it and `md`+ keeps the full 1.5rem. */}
        <Container as="main" className="pt-3 pt-md-4 pb-4 kukatko-main">
          <AnnouncementBanner />
          <Outlet />
        </Container>
        <Footer>
          <JobQueueBadges />
        </Footer>
      </div>
      {/* Phone only: the everyday destinations as a fixed bottom strip, so they
          do not cost a hamburger open-then-tap. Renders nothing on `md`+, where
          the top bar above is already the whole navigation. */}
      <MobileTabBar />
      {/* Shown once, to an account that has never seen it, over whatever it
          landed on. It renders nothing — and asks the backend nothing — for
          everybody else, which is almost every page load. */}
      <WelcomeModal />
    </>
  )
}

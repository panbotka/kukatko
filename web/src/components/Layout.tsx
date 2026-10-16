import { Fragment, type ReactNode, useCallback, useEffect, useState } from 'react'
import Container from 'react-bootstrap/Container'
import Dropdown from 'react-bootstrap/Dropdown'
import Nav from 'react-bootstrap/Nav'
import Navbar from 'react-bootstrap/Navbar'
import NavItem from 'react-bootstrap/NavItem'
import BsNavLink from 'react-bootstrap/NavLink'
import { useTranslation } from 'react-i18next'
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { useIsNavDrawerViewport } from '../hooks/useIsNarrowViewport'
import { useProjectFooter } from '../hooks/useProjectFooter'
import { LIBRARY_PATH } from '../lib/libraryView'
import { useNavPosition } from '../lib/navPosition'
import { useNavSidebarCollapsed } from '../lib/navSidebarCollapsed'
import { useNeutralChrome, useProjectPath, useProjectSlug } from '../project/ProjectContext'
import { stripProjectPrefix } from '../project/paths'
import { useLeaveProject } from '../project/useLeaveProject'
import { useWaitingOnMe } from '../tasks/TaskSummaryContext'

import { AnnouncementBanner } from './AnnouncementBanner'
import { DeactivatedProjectBanner } from './DeactivatedProjectBanner'
import { Footer } from './Footer'
import { Icon } from './Icon'
import { JobQueueStatus } from './JobQueueStatus'
import { KeyboardShortcutsHelp } from './KeyboardShortcutsHelp'
import { MOBILE_MENU_ID, MobileNavDrawer } from './MobileNavDrawer'
import { MobileTabBar } from './MobileTabBar'
import { NotificationBell } from './notifications/NotificationBell'
import { ProjectUnreadBadge } from './notifications/ProjectUnreadBadge'
import { NavSidebar } from './NavSidebar'
import { desktopNav, type NavEntry, type NavGroup, pathMatches, TASKS_PATH } from './navItems'
import { ProjectIdentity } from './ProjectIdentity'
import { PushPrompt } from './push/PushPrompt'
import { SearchCommand } from './search/SearchCommand'
import { WaitingBadge } from './tasks/WaitingBadge'
import { UserMenu } from './UserMenu'
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
 * survives it on every viewport and does not depend on a logo: on `lg`+ the first
 * item of the bar is **Knihovna**, the library at the site root, labelled and
 * `end`-matched; below `lg` the same destination is the leading tab of the
 * {@link MobileTabBar}, permanently under the thumb. Both are one tap, exactly as
 * the mark was.
 *
 * Leading the bar is the global {@link SearchCommand}: a compact icon button,
 * since all it does is open the command palette (`/` or Cmd/Ctrl-K). That circle
 * is the *shortcut*, not the entrance: it carries no label and states its chords
 * only in a `title`, so the labelled **Hledání** item beside Štítky is what
 * actually makes search findable — the bare glyph was the whole way in.
 *
 * **On a phone the palette has one door, and it is not the magnifier.** The
 * shell owns the palette's open state so the {@link MobileTabBar}'s „Hledat" tab
 * can open it; below `lg` the magnifier is therefore not rendered at all and the
 * top row is the hamburger alone. Two controls with the same glyph and nearly the
 * same word used to sit two thumbs apart and lead to two different searches (the
 * palette up top, the `/search` page down below). The `/search` page is still
 * the drawer's „Hledání" row, and the `/` and Ctrl-K chords stay bound on every
 * viewport.
 * Because the swap trades **Žebříček** (now inside "Procházet") for **Hledání**,
 * the widest role's bar comes out marginally *narrower* than before, which is what
 * keeps this change clear of the inline row's long-standing overflow.
 *
 * **The bar tells you where you stand with the work queue.** The Úkoly entry
 * lives inside „Procházet", where a count would be invisible until the dropdown
 * is opened, so the number of tasks waiting on the reader ({@link WaitingBadge},
 * from `TaskSummaryProvider`) is drawn on the *toggle* as well as on the entry;
 * on a phone the same count sits on the corner of the hamburger, for the same
 * reason. Nothing is drawn for zero, so the bar's width — measured, not guessed:
 * see `docs/FRONTEND.md` — only pays for the badge while there is a summons.
 *
 * The language switcher is not in the bar: this instance is Czech, so the setting
 * sits on the account page rather than
 * spending prime bar space. Every entry pairs an icon (for daily recognition) with
 * a `title` describing the action it performs. The items themselves live in
 * `navItems.ts`, so the phone menu below cannot drift from the bar's set or its
 * role gating.
 *
 * Below the `lg` breakpoint the shell swaps that inline bar for two thumb-level
 * navigations. The {@link MobileTabBar} pins the everyday destinations to the
 * bottom edge so a phone user reaches them without opening the hamburger first,
 * and the hamburger opens the {@link MobileNavDrawer} — a proper Offcanvas panel
 * of labelled sections — instead of expanding every dropdown inline into one
 * cramped nested list. Both are decided in JS
 * ({@link useIsNavDrawerViewport}), and the drawer replaces the
 * `Navbar.Collapse` rather than joining it, so neither breakpoint ever carries
 * two copies of the same links.
 *
 * **That boundary is `lg`, not the app's phone breakpoint.** A tablet held
 * upright (768–991px) was given the inline bar and could not hold it: the
 * `.container` is 696px wide there while a maintainer's row of items needs some
 * 960px, so the trailing end — the shortcuts button and the whole user menu,
 * sign-out and account settings included — hung outside the viewport and the
 * page grew a horizontal scrollbar (measured: at 834px the bar ended at 1043px).
 * Scrapping the row down to fit would have bought one release: the widest role's
 * bar is a moving target and the user menu wears the account's display name, so
 * the band would return with the next item. The drawer does not care how many
 * items there are, and on a touch screen it is the better navigation anyway.
 *
 * **Switching projects is leaving one** (the way a streaming service's
 * "who's watching" works): the bar leads with the current project's letter
 * mark ({@link ProjectIdentity} — no name, no menu), and the mark and the user
 * menu's one "Switch library" entry both lead to the chooser at `/projects`,
 * the one place a project is picked. Both are offered only when there is
 * somewhere else to go ({@link useLeaveProject}) — otherwise the mark is a
 * link to the project's library; the unread notifications
 * waiting in the other projects ride on it and on the avatar that opens the
 * menu (on a phone, on the hamburger), and the chooser's tiles break them
 * down per project.
 *
 * **Outside a project the shell is neutral** ({@link useNeutralChrome}): on an
 * instance page (`/account`, `/system…`, `/maintenance`) and on a `/p/<slug>`
 * the account may not enter, the context's project is only the entry rule's
 * fallback, so nothing of it is drawn — no project name (the bar wears the
 * neutral app title, a link to `/`), no library navigation, search, bell or tab bar,
 * no announcement banner and no welcome. What stays is what belongs to the
 * account and the instance: the user menu with the account, the instance's own
 * administration and sign-out. A maintainer inside a deactivated project gets
 * the full shell plus {@link DeactivatedProjectBanner}.
 *
 * **Top or left is the account's choice** (`lib/navPosition.ts`, set on the
 * account page; the top bar is the default). From `lg` up an account that chose
 * the left gets {@link NavSidebar} instead of this bar — never both — and the
 * shell gives the sidebar a lane of its own (`.kukatko-shell--sidebar`), so the
 * page, its sticky toolbars and the fixed chrome that centres on the content
 * keep clear of it. Both layouts render one {@link desktopNav} model, which is
 * what keeps their destinations, order and role gating identical; the user menu
 * is one {@link UserMenu} in either. The sidebar folds to an icon rail
 * (`lib/navSidebarCollapsed.ts`, per account too); the lane then shrinks to the
 * rail (`.kukatko-shell--sidebar-collapsed`). Below `lg` the choice is not read
 * at all: the tab bar and the drawer are the phone's navigation whatever the
 * desktop wears.
 */
export function Layout({ children }: { children?: ReactNode }) {
  const { t } = useTranslation()
  const { user, canCurate, canWrite, isAdmin, isMaintainer, logout } = useAuth()
  const navigate = useNavigate()
  const { pathname } = useLocation()
  // The nav entries are the pages' own paths; this puts each into the current
  // project (an instance page such as the account passes through unchanged).
  const projectPath = useProjectPath()
  // No project's chrome on a page that belongs to none (see above).
  const neutral = useNeutralChrome()
  // The project's own footer line; none in the neutral shell, which belongs to
  // no project, and none signed out.
  const slug = useProjectSlug()
  const footerText = useProjectFooter(neutral || user === null ? null : slug)
  const drawerNav = useIsNavDrawerViewport()
  // The global search palette, opened by the top bar's magnifier on `lg`+ and by
  // the tab bar's search tab below it. Plain state, never the URL: opening and
  // closing it adds no history entry.
  const [paletteOpen, setPaletteOpen] = useState(false)
  const openPalette = useCallback(() => {
    setPaletteOpen(true)
  }, [])
  // How many tasks wait on the reader — the one count the bar carries. Zero
  // (or unknown) draws nothing.
  const waitingOnMe = useWaitingOnMe()
  // The mobile navbar is controlled so it can be closed programmatically. Below
  // the `lg` breakpoint the nav folds into a hamburger; react-bootstrap's
  // `collapseOnSelect` only collapses on a fired select event, which this bar's
  // mix of bare `NavLink`s and raw `Dropdown` items does not reliably emit — so
  // the menu stayed open over the page it had just navigated to.
  const [expanded, setExpanded] = useState(false)
  // The notification prompt waits for the first-run welcome to settle (never
  // needed, or closed), so a freshly signed-in person meets one dialog at a time.
  const [welcomeSettled, setWelcomeSettled] = useState(false)
  const settleWelcome = useCallback(() => {
    setWelcomeSettled(true)
  }, [])
  // Everything the desktop navigation offers this account, in either layout:
  // the main run (with the review game for a curator, the tools per item by
  // role, no dropdown at all for a viewer), the account block ("my photos" only
  // for an account linked to a person — an entry that leads nowhere is worse
  // than none) and the "Správa" section, filtered per item. The top bar and the
  // sidebar both render exactly this, so neither can offer what the other lacks.
  const nav = desktopNav({
    canCurate,
    canWrite,
    isAdmin,
    isMaintainer,
    neutral,
    subjectUid: user?.subject_uid,
  })
  // Where this account wants the desktop navigation. A phone ignores it — the
  // tab bar and the drawer are the only navigation below `lg` — and so does a
  // signed-out screen, which has no account to ask.
  const [navPosition] = useNavPosition()
  const sidebar = !drawerNav && user !== null && navPosition === 'left'
  // Whether that sidebar is folded to its icon rail — also the account's,
  // read only while there is a sidebar, so the top bar never sees it and the
  // left brings it back as it was left.
  const [sidebarCollapsed, setSidebarCollapsed] = useNavSidebarCollapsed()
  const rail = sidebar && sidebarCollapsed
  // The tasks count belongs to the project; a neutral shell wears none.
  const waiting = neutral ? 0 : waitingOnMe
  // "Switch library" and the news waiting behind it, in the other projects.
  const leave = useLeaveProject()
  const othersUnread = leave.offered ? leave.othersUnread : 0

  // Close the collapsed menu on every navigation, whatever control was tapped
  // (top-level link, group dropdown item, or user menu item all change the
  // path). On `lg`+ the collapse is always shown, so this is a no-op there.
  useEffect(() => {
    setExpanded(false)
  }, [pathname])

  // A viewport that grows past the breakpoint (rotation, a resized window) drops
  // the drawer for the inline bar; leaving the state open would then mean the
  // desktop bar came back with an invisible menu still "expanded" behind it.
  useEffect(() => {
    if (!drawerNav) {
      setExpanded(false)
    }
  }, [drawerNav])

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
    const page = stripProjectPrefix(pathname)
    return items.some((item) => pathMatches(page, item.to))
  }

  /** The count badge an entry carries: only the work queue has one. */
  function badgeFor(entry: NavEntry) {
    return entry.to === TASKS_PATH ? <WaitingBadge count={waitingOnMe} /> : null
  }

  /**
   * The badge a group's toggle carries: the sum over its entries, so a count
   * inside a closed dropdown is not a count nobody sees.
   */
  function groupBadge(group: NavGroup) {
    const count = group.items.some((item) => item.to === TASKS_PATH) ? waitingOnMe : 0
    return <WaitingBadge count={count} />
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
        to={projectPath(entry.to)}
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
          {groupBadge(group)}
        </Dropdown.Toggle>
        <Dropdown.Menu>
          {group.items.map((item) => (
            <Dropdown.Item
              key={item.to}
              as={NavLink}
              to={projectPath(item.to)}
              title={t(item.titleKey)}
              className="kukatko-tap-target d-flex align-items-center gap-2"
            >
              <Icon name={item.icon} />
              {t(item.labelKey)}
              {badgeFor(item)}
            </Dropdown.Item>
          ))}
        </Dropdown.Menu>
      </Dropdown>
    )
  }

  return (
    // One flex column for the whole shell, so the footer can be held at the
    // bottom of a page that does not fill the window: the navbar contributes its
    // real height here rather than an estimate, and `.kukatko-page` takes what is
    // left (see `.kukatko-shell` in `app.css`). The fixed chrome below — the
    // drawer, the tab bar, the modal — is out of flow and unaffected.
    <div
      className={`kukatko-shell${sidebar ? ' kukatko-shell--sidebar' : ''}${
        rail ? ' kukatko-shell--sidebar-collapsed' : ''
      }`}
    >
      {/* The account's choice of desktop layout: the left sidebar, or — the
          default, and always below `lg` — the top bar. Never both. The sidebar
          is fixed to the left edge and out of flow; the shell's start padding
          (`.kukatko-shell--sidebar`) gives it its lane, so nothing of the page
          ends up underneath. */}
      {sidebar ? (
        <NavSidebar
          nav={nav}
          neutral={neutral}
          waitingOnMe={waiting}
          paletteOpen={paletteOpen}
          onPaletteOpenChange={setPaletteOpen}
          onLogout={() => {
            void handleLogout()
          }}
          collapsed={sidebarCollapsed}
          onCollapsedChange={setSidebarCollapsed}
        />
      ) : (
        <Navbar
          expand="lg"
          variant="dark"
          sticky="top"
          expanded={expanded}
          onToggle={(next) => {
            setExpanded(next)
          }}
          className="kukatko-navbar"
        >
          <Container>
            {/* Search leads the bar, as a bare icon button: it opens a dialog, so it
                does not need the width of something you can type into. Below `lg`
                the tab bar's search tab is the palette's door instead, so only the
                dialog and its shortcuts are mounted there. */}
            {!neutral && (
              <SearchCommand
                open={paletteOpen}
                onOpenChange={setPaletteOpen}
                showTrigger={!drawerNav}
              />
            )}
            {/* Where the reader stands: the current project's letter mark alone
                (the name is its label and tooltip), the door to the chooser when
                there is another project to pick — never a menu. It costs the row
                one 1.5rem square; below `lg` it takes the phone row's leading
                edge. */}
            <ProjectIdentity />
            {/* The inline bar is the `lg`+ navigation only: below that the same
                items are the drawer's, so rendering both would duplicate every
                link in the DOM. */}
            {!drawerNav && (
              <Navbar.Collapse id={MOBILE_MENU_ID}>
                {/* The library's navigation, or — in the neutral shell — just the
                    room it would take, so the user menu stays at the far end. */}
                <Nav className="me-auto">
                  {/* The everyday loop, loudest first — Library (the homepage),
                      Albums, Labels and Search — then the browse dropdown, the
                      review game (curators and above), Upload as the bar's one
                      filled CTA, and behind a divider the quieter tools, per item
                      by role. The order and the gating are `desktopNav`'s, shared
                      with the sidebar; the administration hangs off the user menu. */}
                  {nav.main.map((slot, index) =>
                    slot.kind === 'link' ? (
                      renderLink(slot.entry, { cta: slot.cta === true })
                    ) : slot.kind === 'group' ? (
                      <Fragment key={slot.group.id}>{renderGroup(slot.group)}</Fragment>
                    ) : (
                      <div
                        key={`divider-${String(index)}`}
                        className="kukatko-nav-divider"
                        aria-hidden="true"
                      />
                    ),
                  )}
                </Nav>
                {/* The bell (the in-app inbox) and the shortcuts: two bare icon
                    buttons, ~39px of the bar's slack for the bell — its badge sits
                    on the corner and costs no width. */}
                <Nav className="align-items-center gap-2">
                  {!neutral && <NotificationBell />}
                  <KeyboardShortcutsHelp />
                </Nav>
                {user && (
                  <Nav className="ms-3">
                    <UserMenu
                      nav={nav}
                      placement="bar"
                      onLogout={() => {
                        void handleLogout()
                      }}
                    />
                  </Nav>
                )}
              </Navbar.Collapse>
            )}
            {/* The hamburger is the whole phone row, held at the trailing edge
                (`ms-auto`): a menu button sits best under the thumb there. It is
                `display: none` on `lg`+, so the desktop bar is unmoved by where it
                stands in the DOM. It wears the tasks-waiting count on its corner:
                Úkoly is inside the menu, and a summons behind a closed menu is no
                summons. The children replace react-bootstrap's default glyph, so
                the glyph is restated alongside the badge. */}
            {/* On a phone the bell stands just before the hamburger: the inbox
                is news, and news behind a closed menu is no news. It takes the
                trailing push (`ms-auto`) so the pair sits together at the edge. */}
            {drawerNav && user && !neutral && <NotificationBell className="ms-auto me-2" />}
            <Navbar.Toggle
              aria-controls={MOBILE_MENU_ID}
              label={t('nav.openMenu')}
              className={`position-relative${drawerNav && user && !neutral ? '' : ' ms-auto'}`}
            >
              <span className="navbar-toggler-icon" />
              <WaitingBadge count={waiting} over />
              {/* The other projects' news, on the opposite corner from the
                  tasks count: "Switch library" is inside the drawer. */}
              {drawerNav && (
                <ProjectUnreadBadge
                  count={othersUnread}
                  label={t('projectSwitcher.othersUnread', { count: othersUnread })}
                  className="kk-project-unread--over-start"
                />
              )}
            </Navbar.Toggle>
          </Container>
        </Navbar>
      )}
      {/* Phone only: the hamburger opens a real drawer of labelled sections
          rather than expanding the whole nav inline into the bar. */}
      {drawerNav && (
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
            step less of it and `md`+ keeps the full 1.5rem. The bottom padding
            is the stylesheet's (`.kukatko-main`), not a `pb-*` utility, so a
            page that fills the viewport — the map on a phone — can drop it
            without an `!important`. */}
        <Container as="main" className="pt-3 pt-md-4 kukatko-main">
          {/* The project's own notices: none in the neutral shell, where the
              context's project is not the page's. */}
          {!neutral && <AnnouncementBanner />}
          <DeactivatedProjectBanner />
          {children ?? <Outlet />}
        </Container>
        <Footer text={footerText}>
          <JobQueueStatus />
        </Footer>
      </div>
      {/* Phone only: the everyday destinations as a fixed bottom strip, so they
          do not cost a hamburger open-then-tap. Renders nothing on `lg`+, where
          the top bar above is already the whole navigation. */}
      {!neutral && <MobileTabBar onOpenSearch={openPalette} />}
      {/* Shown once, to an account that has never seen it, over whatever it
          landed on. It renders nothing — and asks the backend nothing — for
          everybody else, which is almost every page load. */}
      {!neutral && <WelcomeModal onSettled={settleWelcome} />}
      {/* Asked once per account and browser whether to send notifications —
          only after the welcome is out of the way, so two dialogs never stack.
          It also renders nothing, and asks nothing, once answered. Like the
          welcome it waits for a page inside a project. */}
      {welcomeSettled && !neutral && <PushPrompt />}
    </div>
  )
}

import Dropdown from 'react-bootstrap/Dropdown'
import NavDropdown from 'react-bootstrap/NavDropdown'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { useCapabilities } from '../capabilities/CapabilitiesContext'
import { bareVersion } from '../lib/version'
import { useProjectPath } from '../project/ProjectContext'
import { useLeaveProject } from '../project/useLeaveProject'

import { Icon } from './Icon'
import { type DesktopNav, type NavEntry, PROJECTS_ITEM } from './navItems'
import { ProjectUnreadBadge } from './notifications/ProjectUnreadBadge'
import { PersonAvatar } from './PersonAvatar'

/**
 * The signed-in account's menu on a desktop, in either navigation layout: in the
 * top bar it is the dropdown at the trailing end (`placement="bar"`), in the
 * left sidebar the account row at its foot that opens upwards
 * (`placement="sidebar"`). The **content is one and the same** — "Switch
 * library" (only when there is somewhere else to go, wearing the other
 * projects' unread total), the account block, the "Správa" section the role
 * clears, sign-out and the build — so the two layouts cannot drift; only the
 * toggle's shape differs.
 *
 * The toggle is a picture where the name alone used to stand: whatever the
 * account resolves to, and the coloured initial — the common case — when it
 * resolves to nothing. The other projects' news rides on the picture's corner,
 * so it costs no width.
 */
export function UserMenu({
  nav,
  onLogout,
  placement,
}: {
  nav: DesktopNav
  onLogout: () => void
  placement: 'bar' | 'sidebar'
}) {
  const { t } = useTranslation()
  const { user } = useAuth()
  const leave = useLeaveProject()
  const othersUnread = leave.offered ? leave.othersUnread : 0
  // The build the server runs, printed at the foot of the menu. It rides along
  // on the capabilities the shell already holds, so opening the menu costs no
  // request; `null` (nothing loaded yet, or the call failed) shows nothing.
  const version = bareVersion(useCapabilities().version)
  const projectPath = useProjectPath()

  if (user === null) {
    return null
  }
  const name = user.display_name || user.username

  const title = (
    <span className="d-inline-flex align-items-center gap-2 mw-100">
      <span className="kk-user-menu__picture">
        <PersonAvatar name={name} userUid={user.uid} className="kk-avatar--sm" />
        <ProjectUnreadBadge
          count={othersUnread}
          label={t('projectSwitcher.othersUnread', { count: othersUnread })}
          className="kk-project-unread--over"
        />
      </span>
      {/* In the bar: out of sight (not out of the accessible name) in the
          992–1399px band, where the project's name took its width; see
          `.kk-user-menu__name`. In the sidebar there is room for it. */}
      <span className="kk-user-menu__name">{name}</span>
    </span>
  )

  /**
   * One entry of the menu: icon, label, action tooltip. A plain `Link` rather
   * than a `NavLink` — the menu is closed while you are on the page it points
   * at, so an active highlight in there would never be seen.
   */
  function item(entry: NavEntry) {
    return (
      <Dropdown.Item
        key={entry.to}
        as={Link}
        to={projectPath(entry.to)}
        title={t(entry.titleKey)}
        className="d-flex align-items-center gap-2"
      >
        <Icon name={entry.icon} />
        {t(entry.labelKey)}
      </Dropdown.Item>
    )
  }

  const items = (
    <>
      {/* Leaving the project for the chooser leads the menu: it is about which
          library the account is in, before anything about the account itself. */}
      {leave.offered && (
        <>
          <Dropdown.Item
            as={Link}
            to={PROJECTS_ITEM.to}
            title={t(PROJECTS_ITEM.titleKey)}
            className="d-flex align-items-center gap-2"
          >
            <Icon name={PROJECTS_ITEM.icon} />
            <span className="flex-grow-1">{t(PROJECTS_ITEM.labelKey)}</span>
            <ProjectUnreadBadge
              count={othersUnread}
              label={t('projectSwitcher.othersUnread', { count: othersUnread })}
            />
          </Dropdown.Item>
          <Dropdown.Divider />
        </>
      )}
      {nav.account.map((entry) => item(entry))}
      {/* Administration: one labelled section between the account block and
          sign-out, rendered only for the entries the role actually clears, so a
          viewer sees neither heading nor divider. */}
      {nav.admin.length > 0 && (
        <>
          <Dropdown.Divider />
          <Dropdown.Header className="kk-menu-section">{t('nav.admin')}</Dropdown.Header>
          {nav.admin.map((entry) => item(entry))}
        </>
      )}
      <Dropdown.Divider />
      <Dropdown.Item
        title={t('nav.titles.logout')}
        className="d-flex align-items-center gap-2"
        onClick={onLogout}
      >
        <Icon name="box-arrow-right" />
        {t('nav.logout')}
      </Dropdown.Item>
      {/* The build, as a small muted footnote under the last action: an
          `ItemText` is not a menu item, so it neither takes focus when arrowing
          through the menu nor invites a click. The full form — with the commit —
          lives on the help page. */}
      {version && (
        <Dropdown.ItemText className="kk-menu-version" title={t('nav.versionTitle')}>
          {t('nav.versionLabel', { version })}
        </Dropdown.ItemText>
      )}
    </>
  )

  if (placement === 'bar') {
    return (
      <NavDropdown align="end" title={title} id="user-menu">
        {items}
      </NavDropdown>
    )
  }
  return (
    <Dropdown drop="up" className="kk-sidebar__account">
      <Dropdown.Toggle
        id="user-menu"
        variant="link"
        title={t('nav.sidebar.accountTitle')}
        className="kk-sidebar__account-toggle"
      >
        {title}
      </Dropdown.Toggle>
      <Dropdown.Menu className="kk-sidebar__account-menu">{items}</Dropdown.Menu>
    </Dropdown>
  )
}

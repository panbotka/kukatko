import { type ReactElement, useId, useState } from 'react'
import Dropdown from 'react-bootstrap/Dropdown'
import OverlayTrigger from 'react-bootstrap/OverlayTrigger'
import Tooltip from 'react-bootstrap/Tooltip'
import { useTranslation } from 'react-i18next'
import { NavLink, useLocation } from 'react-router-dom'

import { LIBRARY_PATH } from '../lib/libraryView'
import { useProjectPath } from '../project/ProjectContext'
import { stripProjectPrefix } from '../project/paths'

import { Icon } from './Icon'
import { KeyboardShortcutsHelp } from './KeyboardShortcutsHelp'
import {
  BROWSE_GROUP,
  type DesktopNav,
  type NavEntry,
  type NavGroup,
  pathMatches,
  TASKS_PATH,
} from './navItems'
import { NotificationBell } from './notifications/NotificationBell'
import { ProjectIdentity } from './ProjectIdentity'
import { SearchCommand } from './search/SearchCommand'
import { WaitingBadge } from './tasks/WaitingBadge'
import { UserMenu } from './UserMenu'

/**
 * The desktop navigation laid out down the left edge — the layout an account
 * picks on its account page instead of the top bar (see `lib/navPosition.ts`).
 * {@link Layout} mounts it only from `lg` up and only for an account that chose
 * it; a phone keeps the tab bar and the drawer whatever the choice.
 *
 * **It is the top bar turned on its side, not a second menu.** It renders the
 * same {@link DesktopNav} the bar renders, in the bar's order read top to
 * bottom: the everyday links, the „Procházet" group, the review game, the
 * filled upload call-to-action, a hairline, the „Nástroje" group. A dropdown of
 * the bar becomes a collapsible group here — a button that says whether it is
 * open (`aria-expanded`) over its list — since a sidebar has the height a bar
 * lacks. „Procházet" starts open, the tools start folded, and any group holding
 * the current page starts open, so "you are here" is never hidden; a folded
 * group's toggle lights up and wears the tasks count instead.
 *
 * The head carries what leads the bar: the current project's identity, the
 * search palette's button, the bell and the keyboard shortcuts. The foot carries
 * the account menu — the very {@link UserMenu} of the bar (switch library,
 * account, administration, sign-out), opening upwards.
 *
 * Only the middle scrolls: a maintainer's unfolded list is taller than a laptop
 * screen, and the account at the foot must stay in reach — and the menu it opens
 * must not be clipped by a scroller, which is why the foot sits outside it.
 *
 * **It folds to an icon rail** (`collapsed`, remembered per account by
 * `lib/navSidebarCollapsed.ts`; {@link Layout} owns the state because the
 * shell's lane follows it). The toggle at the foot says which way it goes
 * („Sbalit menu" / „Rozbalit menu") and whether the sidebar is unfolded
 * (`aria-expanded`). Folded, every row keeps its glyph, its label as the
 * accessible name (visually hidden) and shows it as a tooltip on hover and
 * keyboard focus; the current page keeps its pill. A group has no room to
 * unfold under itself, so its glyph opens a flyout beside the rail instead —
 * the group's entries with their full labels, a react-bootstrap `Dropdown`, so
 * the arrows walk it, Escape and an outside click close it. The account menu
 * opens beside the rail the same way, and the counts sit on the glyphs'
 * corners, still part of each name. Nothing is reachable unfolded that is not
 * reachable folded.
 */
export function NavSidebar({
  nav,
  neutral,
  waitingOnMe,
  paletteOpen,
  onPaletteOpenChange,
  onLogout,
  collapsed,
  onCollapsedChange,
}: {
  nav: DesktopNav
  /** A page outside any project: no search and no bell, as in the bar. */
  neutral: boolean
  /** The tasks waiting on the reader, worn by the Úkoly row. */
  waitingOnMe: number
  paletteOpen: boolean
  onPaletteOpenChange: (open: boolean) => void
  onLogout: () => void
  /** Folded to the icon-only rail. */
  collapsed: boolean
  onCollapsedChange: (collapsed: boolean) => void
}) {
  const { t } = useTranslation()
  const { pathname } = useLocation()
  const projectPath = useProjectPath()
  // Which groups the reader folded or unfolded by hand; the rest follow the
  // defaults described above.
  const [toggled, setToggled] = useState<Record<string, boolean>>({})
  // The one group whose flyout is open on the folded rail, if any.
  const [flyout, setFlyout] = useState<string | null>(null)
  const sidebarId = useId()

  /** True when any route in `items` is the current location. */
  function groupActive(items: NavEntry[]): boolean {
    const page = stripProjectPrefix(pathname)
    return items.some((item) => pathMatches(page, item.to))
  }

  /**
   * One row: icon, visible label, action tooltip, and the tasks count on Úkoly.
   * The library (the root) and an `exact` entry match only their own path, so
   * the root does not light up on every page.
   */
  function renderLink(entry: NavEntry, cta = false) {
    const label = t(entry.labelKey)
    const link = (
      <NavLink
        to={projectPath(entry.to)}
        end={entry.to === LIBRARY_PATH || entry.exact === true}
        // Folded, the tooltip says the label; a native title beside it would
        // be a second, different bubble.
        title={collapsed ? undefined : t(entry.titleKey)}
        className={`kk-sidebar__link${cta ? ' kk-sidebar__link--cta' : ''}`}
      >
        <Icon name={entry.icon} className="kk-sidebar__icon" />
        <span className={collapsed ? 'visually-hidden' : 'kk-sidebar__label'}>{label}</span>
        {entry.to === TASKS_PATH && <WaitingBadge count={waitingOnMe} over={collapsed} />}
      </NavLink>
    )
    return <li key={entry.to}>{collapsed ? <RailTip label={label}>{link}</RailTip> : link}</li>
  }

  /**
   * One group on the folded rail: its glyph opens the entries in a flyout
   * beside the rail, with their full labels. The menu is fixed-positioned so
   * the scroller it lives in cannot clip it.
   */
  function renderFlyout(group: NavGroup) {
    const active = groupActive(group.items)
    const holdsTasks = group.items.some((item) => item.to === TASKS_PATH)
    const label = t(group.labelKey)
    const open = flyout === group.id
    return (
      <li key={group.id}>
        <Dropdown
          drop="end"
          show={open}
          onToggle={(next) => {
            // Opening another group closes this one through its outside click,
            // which lands after the other's open: close only if still ours.
            setFlyout((current) => (next ? group.id : current === group.id ? null : current))
          }}
        >
          <RailTip label={label} suppress={open}>
            <Dropdown.Toggle
              as="button"
              type="button"
              id={group.id}
              className={`kk-sidebar__link kk-sidebar__flyout-toggle${active ? ' active' : ''}`}
            >
              <Icon name={group.icon} className="kk-sidebar__icon" />
              <span className="visually-hidden">{label}</span>
              {holdsTasks && <WaitingBadge count={waitingOnMe} over />}
            </Dropdown.Toggle>
          </RailTip>
          {/* Mounted up front: a menu first mounted by its own opening was
              measured before it was laid out, and its first flyout landed at
              the top of the window instead of beside the glyph. */}
          <Dropdown.Menu
            className="kk-sidebar__flyout"
            popperConfig={{ strategy: 'fixed' }}
            renderOnMount
          >
            <Dropdown.Header className="kk-menu-section">{label}</Dropdown.Header>
            {group.items.map((item) => (
              <Dropdown.Item
                key={item.to}
                as={NavLink}
                to={projectPath(item.to)}
                end={item.exact === true}
                title={t(item.titleKey)}
                className="d-flex align-items-center gap-2"
              >
                <Icon name={item.icon} />
                <span className="flex-grow-1">{t(item.labelKey)}</span>
                {item.to === TASKS_PATH && <WaitingBadge count={waitingOnMe} />}
              </Dropdown.Item>
            ))}
          </Dropdown.Menu>
        </Dropdown>
      </li>
    )
  }

  /** One collapsible group: the toggle, then its rows when it is open. */
  function renderGroup(group: NavGroup) {
    const active = groupActive(group.items)
    const open = toggled[group.id] ?? (group.id === BROWSE_GROUP.id || active)
    const listId = `${group.id}-list`
    const holdsTasks = group.items.some((item) => item.to === TASKS_PATH)
    return (
      <li key={group.id} className="kk-sidebar__group">
        <button
          type="button"
          id={group.id}
          title={t(group.titleKey)}
          aria-expanded={open}
          aria-controls={listId}
          className={`kk-sidebar__link kk-sidebar__toggle${!open && active ? ' active' : ''}`}
          onClick={() => {
            setToggled((prev) => ({ ...prev, [group.id]: !open }))
          }}
        >
          <Icon name={group.icon} className="kk-sidebar__icon" />
          <span className="kk-sidebar__label">{t(group.labelKey)}</span>
          {/* A count inside a folded group is a count nobody sees, so the
              toggle carries it — exactly as the bar's dropdown toggle does. */}
          {!open && holdsTasks && <WaitingBadge count={waitingOnMe} />}
          <Icon name="chevron-down" className="kk-sidebar__chevron" />
        </button>
        <ul id={listId} className="kk-sidebar__list kk-sidebar__list--nested" hidden={!open}>
          {group.items.map((item) => renderLink(item))}
        </ul>
      </li>
    )
  }

  const foldLabel = t(collapsed ? 'nav.sidebar.expand' : 'nav.sidebar.collapse')
  const foldToggle = (
    <button
      type="button"
      className="kk-sidebar__link kk-sidebar__fold"
      aria-expanded={!collapsed}
      aria-controls={sidebarId}
      title={collapsed ? undefined : foldLabel}
      onClick={() => {
        setFlyout(null)
        onCollapsedChange(!collapsed)
      }}
    >
      <Icon
        name={collapsed ? 'chevron-double-right' : 'chevron-double-left'}
        className="kk-sidebar__icon"
      />
      <span className={collapsed ? 'visually-hidden' : 'kk-sidebar__label'}>{foldLabel}</span>
    </button>
  )

  return (
    <div id={sidebarId} className={`kk-sidebar${collapsed ? ' kk-sidebar--collapsed' : ''}`}>
      <div className="kk-sidebar__head">
        <ProjectIdentity withName={!collapsed} compact={collapsed} />
        <div className="kk-sidebar__tools">
          {!neutral && <SearchCommand open={paletteOpen} onOpenChange={onPaletteOpenChange} />}
          {!neutral && <NotificationBell drop="end" />}
          <KeyboardShortcutsHelp />
        </div>
      </div>
      <nav className="kk-sidebar__nav" aria-label={t('nav.sidebar.label')}>
        <ul className="kk-sidebar__list">
          {nav.main.map((slot, index) =>
            slot.kind === 'link' ? (
              renderLink(slot.entry, slot.cta === true)
            ) : slot.kind === 'group' ? (
              collapsed ? (
                renderFlyout(slot.group)
              ) : (
                renderGroup(slot.group)
              )
            ) : (
              <li
                key={`divider-${String(index)}`}
                className="kk-sidebar__divider"
                aria-hidden="true"
              />
            ),
          )}
        </ul>
      </nav>
      <div className="kk-sidebar__foot">
        {collapsed ? <RailTip label={foldLabel}>{foldToggle}</RailTip> : foldToggle}
        <UserMenu nav={nav} onLogout={onLogout} placement={collapsed ? 'rail' : 'sidebar'} />
      </div>
    </div>
  )
}

/**
 * The folded rail's tooltip: `label` beside the glyph on hover and on keyboard
 * focus. `suppress` keeps it down while the control's own flyout is open, so
 * the bubble does not lie over the menu it names.
 */
function RailTip({
  label,
  suppress = false,
  children,
}: {
  label: string
  suppress?: boolean
  children: ReactElement
}) {
  const id = useId()
  return (
    <OverlayTrigger
      placement="right"
      trigger={['hover', 'focus']}
      show={suppress ? false : undefined}
      overlay={<Tooltip id={id}>{label}</Tooltip>}
    >
      {children}
    </OverlayTrigger>
  )
}

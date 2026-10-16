import { useState } from 'react'
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
 */
export function NavSidebar({
  nav,
  neutral,
  waitingOnMe,
  paletteOpen,
  onPaletteOpenChange,
  onLogout,
}: {
  nav: DesktopNav
  /** A page outside any project: no search and no bell, as in the bar. */
  neutral: boolean
  /** The tasks waiting on the reader, worn by the Úkoly row. */
  waitingOnMe: number
  paletteOpen: boolean
  onPaletteOpenChange: (open: boolean) => void
  onLogout: () => void
}) {
  const { t } = useTranslation()
  const { pathname } = useLocation()
  const projectPath = useProjectPath()
  // Which groups the reader folded or unfolded by hand; the rest follow the
  // defaults described above.
  const [toggled, setToggled] = useState<Record<string, boolean>>({})

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
    return (
      <li key={entry.to}>
        <NavLink
          to={projectPath(entry.to)}
          end={entry.to === LIBRARY_PATH || entry.exact === true}
          title={t(entry.titleKey)}
          className={`kk-sidebar__link${cta ? ' kk-sidebar__link--cta' : ''}`}
        >
          <Icon name={entry.icon} className="kk-sidebar__icon" />
          <span className="kk-sidebar__label">{t(entry.labelKey)}</span>
          {entry.to === TASKS_PATH && <WaitingBadge count={waitingOnMe} />}
        </NavLink>
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

  return (
    <div className="kk-sidebar">
      <div className="kk-sidebar__head">
        <ProjectIdentity />
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
              renderGroup(slot.group)
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
        <UserMenu nav={nav} onLogout={onLogout} placement="sidebar" />
      </div>
    </div>
  )
}

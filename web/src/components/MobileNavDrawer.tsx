import type { ParseKeys } from 'i18next'
import Offcanvas from 'react-bootstrap/Offcanvas'
import { useTranslation } from 'react-i18next'
import { NavLink } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { useCapabilities } from '../capabilities/CapabilitiesContext'
import { LIBRARY_PATH } from '../lib/libraryView'
import { formatVersion } from '../lib/version'

import { Icon } from './Icon'
import { KeyboardShortcutsHelp } from './KeyboardShortcutsHelp'
import {
  ACCOUNT_ITEM,
  adminItems,
  BROWSE_GROUP,
  HELP_ITEM,
  myPhotosItem,
  type NavEntry,
  type NavGroup,
  PRIMARY_ITEMS,
  REVIEW_ITEM,
  STATS_ITEM,
  TOOLS_GROUP,
  UPLOAD_ITEM,
} from './navItems'

/**
 * The id the navbar's hamburger points at with `aria-controls`. The desktop
 * `Navbar.Collapse` carries the same id — only one of the two is ever mounted,
 * so the toggle always names the region it actually opens.
 */
export const MOBILE_MENU_ID = 'main-navbar'

/** The drawer title, referenced by the dialog's `aria-labelledby`. */
const TITLE_ID = 'kk-navdrawer-title'

/**
 * One labelled block of destinations inside the drawer. The heading is what
 * turns the menu from one long undifferentiated list into something a thumb can
 * scan: it names the block, and it is the accessible name of the `region` the
 * rows live in.
 */
interface DrawerSection {
  id: string
  labelKey: ParseKeys
  items: NavEntry[]
}

/**
 * The phone menu, as a real drawer.
 *
 * Below the navbar's `md` breakpoint the bar's dropdowns cannot open as popovers
 * (react-bootstrap disables Popper inside a `Navbar`), so the collapsed burger
 * used to expand every group *inline* — one long nested stack of links with no
 * grouping, no headings and cramped rows. This renders the same menu as an
 * Offcanvas drawer instead: it slides in over the page, scrolls on its own when
 * tall, dismisses by tapping outside / the close button / Escape, and lays the
 * items out as labelled sections of comfortable ≥44px rows.
 *
 * **The set of items and their role gating is exactly the navbar's**, because
 * both read the same registries in `navItems.ts`: the everyday block, Procházet,
 * the editor-only Nástroje, and the account block that stands in for the user
 * menu (account, the library statistics, help, the keyboard-shortcuts overlay,
 * the **Správa** group and sign-out). A section whose role gate is closed is not
 * rendered at all, exactly as its dropdown is not rendered in the bar.
 *
 * Správa is a group *inside* the account block rather than a section of its own,
 * because that is where the desktop keeps it too: one labelled group in the user
 * menu, between the account entries and sign-out, holding what used to be the
 * bar's two admin dropdowns. Its rows are gated one by one (`adminItems`), so an
 * admin who is not a maintainer gets the two governance rows and nothing else.
 *
 * Mounted by {@link Layout} only below the breakpoint, so the desktop DOM keeps a
 * single set of nav links; navigating closes it (both via `Layout`'s pathname
 * effect and each row's own `onClick`, which also covers re-tapping the route you
 * are already on).
 */
export function MobileNavDrawer({
  show,
  onHide,
  onLogout,
}: {
  show: boolean
  onHide: () => void
  onLogout: () => void
}) {
  const { t } = useTranslation()
  const { user, canWrite, isAdmin, isMaintainer } = useAuth()
  // The build the server runs, printed above sign-out exactly as the desktop
  // user menu prints it. It comes from the capabilities the shell already holds,
  // so opening the drawer costs no request.
  const version = formatVersion(useCapabilities().version)
  // "My photos", offered only to an account that has said which person of the
  // library it is — the same gate the desktop user menu applies, from the same
  // builder, so the two menus cannot drift.
  const myPhotos = myPhotosItem(user?.subject_uid)
  // The administration rows of the account block, per-item gated exactly as the
  // desktop user menu gates them; empty means the group is not rendered at all.
  const admin = adminItems({ isAdmin, isMaintainer })

  // Each of the bar's dropdowns becomes one section, unfolded, behind the very
  // same role gate — a closed gate drops the whole section, exactly as it drops
  // the dropdown up in the bar. Browse is open to every signed-in role.
  const groups: { open: boolean; group: NavGroup }[] = [
    { open: true, group: BROWSE_GROUP },
    { open: canWrite, group: TOOLS_GROUP },
  ]

  // The everyday block leads (it is what the bar shows loudest), then the browse
  // destinations, then the role-gated clusters in ladder order.
  const sections: DrawerSection[] = [
    {
      id: 'main',
      labelKey: 'nav.sections.main',
      // The review game and the upload CTA share one gate, so they share one
      // spread; the leaderboard they used to sandwich now lives in Procházet.
      items: [...PRIMARY_ITEMS, ...(canWrite ? [REVIEW_ITEM, UPLOAD_ITEM] : [])],
    },
    ...groups
      .filter((candidate) => candidate.open)
      .map(({ group }) => ({ id: group.id, labelKey: group.labelKey, items: group.items })),
  ]

  /**
   * One tap row: icon, label, action tooltip. The root entry (the library) is
   * matched exactly — without `end` its highlight would be a prefix match and
   * light up on every route. Upload keeps the bar's filled call-to-action look.
   */
  function renderRow(entry: NavEntry) {
    const cta = entry.to === UPLOAD_ITEM.to
    return (
      <NavLink
        key={entry.to}
        to={entry.to}
        end={entry.to === LIBRARY_PATH}
        title={t(entry.titleKey)}
        className={`kk-navdrawer__link${cta ? ' kk-navdrawer__link--cta' : ''}`}
        onClick={onHide}
      >
        <Icon name={entry.icon} className="kk-navdrawer__icon" />
        <span className="kk-navdrawer__label">{t(entry.labelKey)}</span>
      </NavLink>
    )
  }

  return (
    <Offcanvas
      id={MOBILE_MENU_ID}
      show={show}
      onHide={onHide}
      placement="end"
      className="kk-navdrawer"
      aria-labelledby={TITLE_ID}
    >
      <Offcanvas.Header closeButton closeLabel={t('nav.closeMenu')} className="kk-navdrawer__head">
        <Offcanvas.Title id={TITLE_ID}>{t('nav.menu')}</Offcanvas.Title>
      </Offcanvas.Header>
      <Offcanvas.Body className="kk-navdrawer__body">
        {sections.map((section) => (
          <section
            key={section.id}
            className="kk-navdrawer__section"
            aria-labelledby={`kk-navdrawer-${section.id}`}
          >
            <h2 id={`kk-navdrawer-${section.id}`} className="kk-navdrawer__heading">
              {t(section.labelKey)}
            </h2>
            {section.items.map((item) => renderRow(item))}
          </section>
        ))}

        {/* The user menu, unfolded: the same account and help destinations, the
            keyboard-shortcuts overlay that lives in the bar beside them, the
            Správa group and sign-out — which dismisses through a handler, not a
            route change. Gated on a signed-in user exactly as the bar's user
            dropdown is. */}
        {user && (
          <section className="kk-navdrawer__section" aria-labelledby="kk-navdrawer-account">
            <h2 id="kk-navdrawer-account" className="kk-navdrawer__heading">
              {t('nav.sections.account')}
            </h2>
            {renderRow(ACCOUNT_ITEM)}
            {myPhotos && renderRow(myPhotos)}
            {renderRow(STATS_ITEM)}
            {renderRow(HELP_ITEM)}
            <KeyboardShortcutsHelp variant="row" />
            {/* The build, as text rather than a row: no button, no link, nothing
                the thumb or the keyboard can land on — an unobtrusive fact above
                the one destructive action in the drawer. */}
            {version && (
              <p className="kk-navdrawer__version" title={t('nav.versionTitle')}>
                {version}
              </p>
            )}
            {/* Administration: one labelled group between the account rows and
                sign-out, in the same place the desktop user menu puts it. A
                nested `section` rather than a top-level one, because it belongs
                to the account block — and because promoting it would put it
                *after* sign-out, teaching a different order than the bar. */}
            {admin.length > 0 && (
              <section className="kk-navdrawer__subsection" aria-labelledby="kk-navdrawer-admin">
                <h3 id="kk-navdrawer-admin" className="kk-navdrawer__heading">
                  {t('nav.admin')}
                </h3>
                {admin.map((item) => renderRow(item))}
              </section>
            )}
            <button
              type="button"
              title={t('nav.titles.logout')}
              className="kk-navdrawer__link"
              onClick={onLogout}
            >
              <Icon name="box-arrow-right" className="kk-navdrawer__icon" />
              <span className="kk-navdrawer__label">{t('nav.logout')}</span>
            </button>
          </section>
        )}
      </Offcanvas.Body>
    </Offcanvas>
  )
}

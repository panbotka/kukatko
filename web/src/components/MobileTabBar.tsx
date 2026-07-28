import type { ParseKeys } from 'i18next'
import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { NavLink } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { useIsNarrowViewport } from '../hooks/useIsNarrowViewport'
import { LIBRARY_PATH } from '../lib/libraryView'

import { Icon, type IconName } from './Icon'

/** One destination in the bottom bar: a glyph, a short label, a route. */
interface TabEntry {
  to: string
  labelKey: ParseKeys
  titleKey: ParseKeys
  icon: IconName
  /** Editors only — a viewer simply gets one tab fewer. */
  writeOnly?: boolean
}

/**
 * The everyday loop, and nothing else. The bar is deliberately not a second copy
 * of the navbar: browse (people, places, map), the review game, the tools and the
 * admin groups all stay in the hamburger menu, because a bottom bar earns its
 * permanent strip of a phone screen only by being short enough to hit blind.
 *
 * Four tabs at most, so a 320px screen still gives each one a comfortable finger
 * target and an unabbreviated label; Upload drops out for a viewer, leaving three.
 * The library is the homepage, so its tab points at the site root.
 */
const TABS: readonly TabEntry[] = [
  { to: LIBRARY_PATH, labelKey: 'nav.library', titleKey: 'nav.titles.library', icon: 'images' },
  { to: '/albums', labelKey: 'nav.albums', titleKey: 'nav.titles.albums', icon: 'collection' },
  { to: '/labels', labelKey: 'nav.labels', titleKey: 'nav.titles.labels', icon: 'tags' },
  {
    to: '/upload',
    labelKey: 'nav.upload',
    titleKey: 'nav.titles.upload',
    icon: 'cloud-arrow-up',
    writeOnly: true,
  },
]

/**
 * The mobile bottom tab bar: a fixed strip of the primary destinations, shown
 * **only below the navbar's `md` expand breakpoint**. On a phone the whole
 * primary navigation is otherwise folded into the hamburger, so reaching the
 * library or the albums costs an open-then-tap every single time; the bar puts
 * the everyday loop one thumb-reach away while the top bar keeps search.
 *
 * On `md`+ it renders nothing at all — the decision is made in JS via
 * {@link useIsNarrowViewport} rather than by a `d-md-none` display rule, so the
 * desktop DOM has no duplicate set of navigation links for assistive tech (or a
 * test) to trip over. The class is kept as a second guard for the instant
 * between a resize and React's re-render.
 *
 * The bar publishes its own rendered height into `--kk-tabbar-height` on the
 * document root, mirroring how `BatchActionBar` publishes `--kk-batch-bar-height`.
 * That one variable is what keeps everything else clear of it: the page reserves
 * bottom scroll clearance (`body`'s padding), the floating batch bar stacks
 * *above* the tabs instead of landing on them, and the timeline rail stops short
 * of them. The variable is removed on unmount and on the desktop breakpoint, so
 * nothing reserves space for a bar that is not there.
 */
export function MobileTabBar() {
  const { t } = useTranslation()
  const { canWrite } = useAuth()
  const narrow = useIsNarrowViewport()
  const barRef = useRef<HTMLElement>(null)

  // Publish the live height (safe-area padding included) so the rest of the shell
  // can reserve exactly that much room. Re-runs when the breakpoint flips, which
  // is also what tears the variable back down on the way to desktop.
  useEffect(() => {
    const bar = barRef.current
    const root = document.documentElement
    if (bar === null) {
      root.style.removeProperty('--kk-tabbar-height')
      return
    }
    const publish = (): void => {
      root.style.setProperty('--kk-tabbar-height', `${bar.getBoundingClientRect().height}px`)
    }
    publish()
    // ResizeObserver is absent in jsdom; the one-off publish above still runs there.
    const observer =
      typeof ResizeObserver === 'function'
        ? new ResizeObserver(() => {
            publish()
          })
        : null
    observer?.observe(bar)
    return () => {
      observer?.disconnect()
      root.style.removeProperty('--kk-tabbar-height')
    }
  }, [narrow])

  if (!narrow) {
    return null
  }

  const tabs = TABS.filter((tab) => tab.writeOnly !== true || canWrite)

  return (
    <nav ref={barRef} className="kk-tabbar d-md-none" aria-label={t('nav.tabBar')}>
      {tabs.map((tab) => (
        <NavLink
          key={tab.to}
          to={tab.to}
          // Without `end` the root route's prefix match would light the library
          // tab up on every page in the app.
          end={tab.to === LIBRARY_PATH}
          title={t(tab.titleKey)}
          className="kk-tabbar__tab"
        >
          <Icon name={tab.icon} className="kk-tabbar__icon" />
          <span className="kk-tabbar__label">{t(tab.labelKey)}</span>
        </NavLink>
      ))}
    </nav>
  )
}

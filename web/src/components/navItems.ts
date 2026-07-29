import type { ParseKeys } from 'i18next'

import { LIBRARY_PATH } from '../lib/libraryView'

import type { IconName } from './Icon'

/**
 * A single navigable destination. `titleKey` names the *action* the entry
 * performs ("Show the albums"), not the destination noun — it becomes the
 * `title` tooltip, so it should tell a first-time user what clicking does while
 * the short visible label plus `icon` carry recognition for the daily users.
 */
export interface NavEntry {
  to: string
  labelKey: ParseKeys
  titleKey: ParseKeys
  icon: IconName
}

/** A dropdown of related destinations, behind a labelled, icon-bearing toggle. */
export interface NavGroup {
  id: string
  labelKey: ParseKeys
  titleKey: ParseKeys
  icon: IconName
  items: NavEntry[]
}

/**
 * The always-visible destinations, in the order the library is actually browsed:
 * everything, then by album, then by label. Available to every signed-in role.
 * The library is the homepage, so its entry points to the site root.
 */
export const PRIMARY_ITEMS: NavEntry[] = [
  { to: LIBRARY_PATH, labelKey: 'nav.library', titleKey: 'nav.titles.library', icon: 'images' },
  { to: '/albums', labelKey: 'nav.albums', titleKey: 'nav.titles.albums', icon: 'collection' },
  { to: '/labels', labelKey: 'nav.labels', titleKey: 'nav.titles.labels', icon: 'tags' },
]

/** The "Procházet" (Browse) group: the less-travelled ways into the library. */
export const BROWSE_GROUP: NavGroup = {
  id: 'nav-browse',
  labelKey: 'nav.browse',
  titleKey: 'nav.titles.browse',
  icon: 'compass',
  items: [
    {
      to: '/favorites',
      labelKey: 'nav.favorites',
      titleKey: 'nav.titles.favorites',
      icon: 'heart',
    },
    { to: '/people', labelKey: 'nav.people', titleKey: 'nav.titles.people', icon: 'people' },
    { to: '/places', labelKey: 'nav.places', titleKey: 'nav.titles.places', icon: 'geo-alt' },
    { to: '/map', labelKey: 'nav.map', titleKey: 'nav.titles.map', icon: 'map' },
  ],
}

/**
 * The editor-only "Nástroje" (Tools) group, gated behind `canWrite`. It gathers
 * the power-user curation tools that a day-to-day browser rarely reaches for —
 * starting with "Rozšířit" (expand), which grows an album or label with similar
 * photos. Keeping expand here, rather than shouting for attention next to Alba /
 * Štítky, is the whole point of Part 3: the everyday loop stays uncluttered while
 * the tools remain one visible dropdown away.
 */
export const TOOLS_GROUP: NavGroup = {
  id: 'nav-tools',
  labelKey: 'nav.tools',
  titleKey: 'nav.titles.tools',
  icon: 'tools',
  items: [
    { to: '/expand', labelKey: 'nav.expand', titleKey: 'nav.titles.expand', icon: 'magic' },
    {
      to: '/faces',
      labelKey: 'nav.faceSearch',
      titleKey: 'nav.titles.faceSearch',
      icon: 'person-bounding-box',
    },
    {
      to: '/recognition',
      labelKey: 'nav.recognition',
      titleKey: 'nav.titles.recognition',
      icon: 'person-check',
    },
    {
      to: '/outliers',
      labelKey: 'nav.outliers',
      titleKey: 'nav.titles.outliers',
      icon: 'exclamation-triangle',
    },
    {
      to: '/duplicates',
      labelKey: 'nav.duplicates',
      titleKey: 'nav.titles.duplicates',
      icon: 'files',
    },
    { to: '/trash', labelKey: 'nav.trash', titleKey: 'nav.titles.trash', icon: 'trash' },
  ],
}

/**
 * The maintainer-only "Provoz" (Operations) group, gated behind `isMaintainer`.
 * It gathers the operational tools at the top of the role ladder — import,
 * library maintenance and system status. Import lives here rather than top-level:
 * it is no longer an off-ladder capability, it needs the maintainer role like the
 * rest of operations.
 */
export const OPERATIONS_GROUP: NavGroup = {
  id: 'nav-operations',
  labelKey: 'nav.operations',
  titleKey: 'nav.titles.operations',
  icon: 'sliders',
  items: [
    {
      to: '/import',
      labelKey: 'nav.import',
      titleKey: 'nav.titles.import',
      icon: 'box-arrow-in-down',
    },
    {
      to: '/maintenance',
      labelKey: 'nav.maintenance',
      titleKey: 'nav.titles.maintenance',
      icon: 'wrench-adjustable',
    },
    { to: '/system', labelKey: 'nav.system', titleKey: 'nav.titles.system', icon: 'activity' },
  ],
}

/**
 * The governance "Správa" (Admin) group, gated behind `isAdmin` (admin or
 * higher). It holds the account and audit administration — the powers an admin
 * has that stop short of operations.
 */
export const GOVERNANCE_GROUP: NavGroup = {
  id: 'nav-governance',
  labelKey: 'nav.admin',
  titleKey: 'nav.titles.admin',
  icon: 'shield-lock',
  items: [
    { to: '/users', labelKey: 'nav.users', titleKey: 'nav.titles.users', icon: 'person-gear' },
    { to: '/audit', labelKey: 'nav.audit', titleKey: 'nav.titles.audit', icon: 'clock-history' },
  ],
}

/**
 * The write-gated review game. Top-level rather than buried in "Nástroje":
 * tidying the library one question at a time is the app's most-used curation
 * loop, and a game nobody can find is a game nobody plays.
 */
export const REVIEW_ITEM: NavEntry = {
  to: '/review',
  labelKey: 'nav.review',
  titleKey: 'nav.titles.review',
  icon: 'ui-checks',
}

/**
 * The sorting leaderboard — the review game's competition standings. Visible to
 * every signed-in role (viewer and up): reading the aggregate counts is not a
 * write action, so it is not gated behind a role group. It sits top-level next
 * to the review game it summarizes, so the game's scoreboard is one click away.
 */
export const LEADERBOARD_ITEM: NavEntry = {
  to: '/leaderboard',
  labelKey: 'nav.leaderboard',
  titleKey: 'nav.titles.leaderboard',
  icon: 'trophy',
}

/**
 * The write-gated upload entry. Adding photos is the everyday loop's payoff, so
 * it is not just top-level but the bar's one call-to-action: rendered as a filled
 * pill (see `renderLink`'s `cta` option) so a non-technical user's eye lands on
 * "add photos" instead of treating it as just another link beside Import.
 */
export const UPLOAD_ITEM: NavEntry = {
  to: '/upload',
  labelKey: 'nav.upload',
  titleKey: 'nav.titles.upload',
  icon: 'cloud-arrow-up',
}

/** The signed-in user's own account page — the user menu's first entry. */
export const ACCOUNT_ITEM: NavEntry = {
  to: '/account',
  labelKey: 'nav.account',
  titleKey: 'nav.titles.account',
  icon: 'person-circle',
}

/** The help page, alongside the account entry in the user menu. */
export const HELP_ITEM: NavEntry = {
  to: '/help',
  labelKey: 'nav.help',
  titleKey: 'nav.titles.help',
  icon: 'question-circle',
}

/**
 * Reports whether `pathname` matches the given nav route, treating a route as
 * active for its detail sub-paths too (e.g. `/albums/ab12` activates `/albums`).
 * Used to light up the parent dropdown when any of its children is current.
 */
export function pathMatches(pathname: string, route: string): boolean {
  return pathname === route || pathname.startsWith(`${route}/`)
}

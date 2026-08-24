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
 * everything, then by album, then by label, then by query. Available to every
 * signed-in role. The library is the homepage, so its entry points to the site root.
 *
 * **Hledání is a menu item, not just a shortcut.** The magnifier button in the bar
 * (see `SearchCommand`) opens the command palette and names its chords in a `title`
 * — which a phone never shows and a first-time desktop user has to hover to find.
 * The `/search` page behind this entry is the only place carrying the query-language
 * help and the search-mode selector, i.e. the app's strongest feature; it gets a
 * label like every other destination and the bare circle stays as the shortcut.
 */
export const PRIMARY_ITEMS: NavEntry[] = [
  { to: LIBRARY_PATH, labelKey: 'nav.library', titleKey: 'nav.titles.library', icon: 'images' },
  { to: '/albums', labelKey: 'nav.albums', titleKey: 'nav.titles.albums', icon: 'collection' },
  { to: '/labels', labelKey: 'nav.labels', titleKey: 'nav.titles.labels', icon: 'tags' },
  { to: '/search', labelKey: 'nav.search', titleKey: 'nav.titles.search', icon: 'search' },
]

/**
 * The "Procházet" (Browse) group: the less-travelled ways into the library.
 *
 * Saved searches sit right after Oblíbené because that is what they are — smart
 * albums, a stored way *into* the library — and their only other door was a
 * dropdown on `/search`, one level deeper than anything else in the app. Žebříček
 * closes the group: the sorting scoreboard is a way of looking at the library too,
 * but a rarely-used one, and a top-level slot next to Knihovna and Alba oversold it.
 */
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
    {
      to: '/saved',
      labelKey: 'savedSearches.nav',
      titleKey: 'savedSearches.navTitle',
      icon: 'bookmarks',
    },
    { to: '/people', labelKey: 'nav.people', titleKey: 'nav.titles.people', icon: 'people' },
    { to: '/places', labelKey: 'nav.places', titleKey: 'nav.titles.places', icon: 'geo-alt' },
    { to: '/map', labelKey: 'nav.map', titleKey: 'nav.titles.map', icon: 'map' },
    {
      to: '/leaderboard',
      labelKey: 'nav.leaderboard',
      titleKey: 'nav.titles.leaderboard',
      icon: 'trophy',
    },
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
      to: '/duplicate-markers',
      labelKey: 'nav.duplicateMarkers',
      titleKey: 'nav.titles.duplicateMarkers',
      icon: 'person-lines-fill',
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
 * Which role unlocks one entry of the "Správa" section: `maintainer` for the
 * operational entries, `admin` for the governance ones (admin *or* higher, so a
 * maintainer clears it too).
 */
export type AdminGate = 'maintainer' | 'admin'

/** One entry of the "Správa" section, carrying the role that unlocks it. */
export interface AdminEntry extends NavEntry {
  gate: AdminGate
}

/**
 * The "Správa" (Admin) section of the user menu — what used to be two separate
 * dropdowns in the bar: the maintainer's "Provoz" (import, maintenance, system)
 * and the admin's "Správa" (users, audit). Both belong to the signed-in identity
 * rather than to browsing the library, and both spent the bar's scarcest
 * resource, width — for a maintainer the inline row overflowed its container
 * below 1400px. Folded into one labelled section of the user dropdown, in ladder
 * order: operations first, governance after.
 *
 * The gate is **per item**, not on the section: an admin who is not a maintainer
 * gets only Uživatelé and Audit, and a role that clears neither gate gets no
 * section at all (see {@link adminItems}).
 */
export const ADMIN_ITEMS: AdminEntry[] = [
  {
    to: '/import',
    labelKey: 'nav.import',
    titleKey: 'nav.titles.import',
    icon: 'box-arrow-in-down',
    gate: 'maintainer',
  },
  {
    to: '/maintenance',
    labelKey: 'nav.maintenance',
    titleKey: 'nav.titles.maintenance',
    icon: 'wrench-adjustable',
    gate: 'maintainer',
  },
  {
    to: '/system',
    labelKey: 'nav.system',
    titleKey: 'nav.titles.system',
    icon: 'activity',
    gate: 'maintainer',
  },
  {
    to: '/users',
    labelKey: 'nav.users',
    titleKey: 'nav.titles.users',
    icon: 'person-gear',
    gate: 'admin',
  },
  {
    to: '/audit',
    labelKey: 'nav.audit',
    titleKey: 'nav.titles.audit',
    icon: 'clock-history',
    gate: 'admin',
  },
  {
    to: '/settings',
    labelKey: 'nav.settings',
    titleKey: 'nav.titles.settings',
    icon: 'sliders',
    gate: 'admin',
  },
]

/**
 * The entries of {@link ADMIN_ITEMS} the given roles may actually reach, in menu
 * order. An empty result means the section is not rendered at all — no orphan
 * heading, no stray divider — which is what a viewer and an editor get.
 */
export function adminItems(roles: { isAdmin: boolean; isMaintainer: boolean }): NavEntry[] {
  return ADMIN_ITEMS.filter((entry) =>
    entry.gate === 'maintainer' ? roles.isMaintainer : roles.isAdmin,
  )
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

/**
 * "My photos": the library scoped to the person the signed-in account says it
 * is. Built rather than declared, because the destination depends on *who* is
 * asking — the subject UID is part of the route.
 *
 * It lives in the user menu beside the account entry, not in the bar: the
 * inline row is the app's scarcest space and already runs long for an editor,
 * while "mine" belongs with the rest of what is mine. It is offered **only** to
 * an account that has set the link (see {@link myPhotosItem} returning null) —
 * an entry that leads to an empty grid is worse than no entry.
 */
export function myPhotosItem(subjectUid: string | null | undefined): NavEntry | null {
  if (subjectUid === null || subjectUid === undefined || subjectUid === '') {
    return null
  }
  return {
    // The person facet of the library grid, the same one a person's page links
    // to — so "my photos" is an ordinary scoped view with every filter, sort and
    // the Back button working as they do everywhere else.
    to: `${LIBRARY_PATH}?person=${encodeURIComponent(subjectUid)}`,
    labelKey: 'nav.myPhotos',
    titleKey: 'nav.titles.myPhotos',
    icon: 'person-hearts',
  }
}

/**
 * The library-statistics page, in the user menu beside the account and help
 * entries. It lives there rather than in a role-gated group because the counts
 * are open to every signed-in role — the menu the whole app shares is the one
 * place a viewer can reach them from.
 */
export const STATS_ITEM: NavEntry = {
  to: '/stats',
  labelKey: 'nav.stats',
  titleKey: 'nav.titles.stats',
  icon: 'bar-chart',
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

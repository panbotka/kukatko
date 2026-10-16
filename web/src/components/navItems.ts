import type { ParseKeys } from 'i18next'

import { LIBRARY_PATH } from '../lib/libraryView'
import { isInstancePath, PROJECTS_PATH } from '../project/paths'

/**
 * The work queue's route. Named because the shell singles it out: it is the
 * one destination that carries a count badge (the tasks waiting on the reader),
 * wherever the entry is rendered — the bar's dropdown, its toggle, the drawer.
 */
export const TASKS_PATH = '/tasks'

/**
 * The system administrator's project list. An instance page (it lives under
 * `/system`), so no project prefix is ever put on it.
 */
export const SYSTEM_PROJECTS_PATH = '/system/projects'

/** The system administrator's page of one project, `/system/projects/<slug>`. */
export function systemProjectPath(slug: string): string {
  return `${SYSTEM_PROJECTS_PATH}/${encodeURIComponent(slug)}`
}

/** The system administrator's list of every account of the instance. */
export const SYSTEM_USERS_PATH = '/system/users'

/**
 * The system administrator's audit trail of the whole instance: every project's
 * entries and the instance's own (accounts, instance roles, blocking), which no
 * project's audit page shows.
 */
export const SYSTEM_AUDIT_PATH = '/system/audit'

/** A project admin's members page — a page of the project, `/p/<slug>/members`. */
export const MEMBERS_PATH = '/members'

import type { IconName } from './Icon'

/**
 * A single navigable destination. `titleKey` names the *action* the entry
 * performs ("Show the albums"), not the destination noun — it becomes the
 * `title` tooltip, so it should tell a first-time user what clicking does while
 * the short visible label plus `icon` carry recognition for the daily users.
 */
export interface NavEntry {
  /**
   * The page's own path (`/albums`), never its address: the shell puts it into
   * the current project (`/p/<slug>/albums`) with `useProjectPath` when it
   * renders the link, and an instance page such as `/account` passes through
   * unchanged. Keeping the project out of here keeps this list static.
   */
  to: string
  labelKey: ParseKeys
  titleKey: ParseKeys
  icon: IconName
  /**
   * Lights the entry up only on its own path, not on its sub-paths — for a
   * destination whose children are destinations of their own (`/system` next
   * to `/system/projects`).
   */
  exact?: boolean
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
 * — which a first-time desktop user has to hover to find. The `/search` page behind
 * this entry is the only place carrying the query-language help and the search-mode
 * selector, i.e. the app's strongest feature; it gets a label like every other
 * destination and the bare circle stays as the shortcut. On a phone this entry is
 * the drawer's row and the page's only door: the tab bar's search tab opens the
 * palette there, and the top bar has no magnifier.
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
    { to: TASKS_PATH, labelKey: 'nav.tasks', titleKey: 'nav.titles.tasks', icon: 'ui-checks' },
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
 * Which role unlocks one entry of the "Nástroje" section: `curator` for the face,
 * people and collection tools, `editor` for the ones that archive or delete
 * photos (duplicates, trash). Each is "that role *or* higher", mirroring the
 * route guard in `App.tsx` the entry leads to.
 */
export type ToolGate = 'curator' | 'editor'

/** One entry of the "Nástroje" group, carrying the role that unlocks it. */
export interface ToolEntry extends NavEntry {
  gate: ToolGate
}

/**
 * The entries of the "Nástroje" (Tools) group: power-user curation tools that a day-to-day
 * browser rarely reaches for — starting with "Rozšířit" (expand), which grows an
 * album or label with similar photos. Keeping expand here, rather than shouting
 * for attention next to Alba / Štítky, is the whole point of Part 3: the everyday
 * loop stays uncluttered while the tools remain one visible dropdown away.
 *
 * The gate is **per item** (see {@link toolsGroup}): a curator gets the face and
 * collection tools but not duplicates or the trash, which touch the photos
 * themselves; a viewer gets no group at all.
 */
export const TOOL_ITEMS: ToolEntry[] = [
  {
    to: '/expand',
    labelKey: 'nav.expand',
    titleKey: 'nav.titles.expand',
    icon: 'magic',
    gate: 'curator',
  },
  {
    to: '/faces',
    labelKey: 'nav.faceSearch',
    titleKey: 'nav.titles.faceSearch',
    icon: 'person-bounding-box',
    gate: 'curator',
  },
  {
    to: '/recognition',
    labelKey: 'nav.recognition',
    titleKey: 'nav.titles.recognition',
    icon: 'person-check',
    gate: 'curator',
  },
  {
    to: '/outliers',
    labelKey: 'nav.outliers',
    titleKey: 'nav.titles.outliers',
    icon: 'exclamation-triangle',
    gate: 'curator',
  },
  {
    to: '/duplicate-markers',
    labelKey: 'nav.duplicateMarkers',
    titleKey: 'nav.titles.duplicateMarkers',
    icon: 'person-lines-fill',
    gate: 'curator',
  },
  {
    to: '/upload-links',
    labelKey: 'nav.uploadLinks',
    titleKey: 'nav.titles.uploadLinks',
    icon: 'link-45deg',
    gate: 'curator',
  },
  {
    to: '/duplicates',
    labelKey: 'nav.duplicates',
    titleKey: 'nav.titles.duplicates',
    icon: 'files',
    gate: 'editor',
  },
  {
    to: '/trash',
    labelKey: 'nav.trash',
    titleKey: 'nav.titles.trash',
    icon: 'trash',
    gate: 'editor',
  },
]

/** The whole "Nástroje" group, every entry regardless of role, in menu order. */
export const TOOLS_GROUP: NavGroup = {
  id: 'nav-tools',
  labelKey: 'nav.tools',
  titleKey: 'nav.titles.tools',
  icon: 'tools',
  items: TOOL_ITEMS,
}

/**
 * The "Nástroje" group narrowed to the entries the given roles may actually
 * reach, in menu order — or null when that leaves nothing, so neither the bar's
 * dropdown nor the drawer's section renders an empty shell (a viewer's case).
 */
export function toolsGroup(roles: { canCurate: boolean; canWrite: boolean }): NavGroup | null {
  const items = TOOL_ITEMS.filter((entry) =>
    entry.gate === 'editor' ? roles.canWrite : roles.canCurate,
  )
  if (items.length === 0) {
    return null
  }
  return { ...TOOLS_GROUP, items }
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
 * dropdowns in the bar: the maintainer's "Provoz" (import, maintenance, system,
 * and the instance's projects, accounts and audit) and the admin's "Správa" (users, audit). Both belong to the signed-in identity
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
    exact: true,
  },
  {
    to: SYSTEM_PROJECTS_PATH,
    labelKey: 'nav.systemProjects',
    titleKey: 'nav.titles.systemProjects',
    icon: 'diagram-3',
    gate: 'maintainer',
  },
  {
    to: SYSTEM_USERS_PATH,
    labelKey: 'nav.systemUsers',
    titleKey: 'nav.titles.systemUsers',
    icon: 'shield-lock',
    gate: 'maintainer',
  },
  {
    to: SYSTEM_AUDIT_PATH,
    labelKey: 'nav.systemAudit',
    titleKey: 'nav.titles.systemAudit',
    icon: 'clock-history',
    gate: 'maintainer',
  },
  {
    to: MEMBERS_PATH,
    labelKey: 'nav.members',
    titleKey: 'nav.titles.members',
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
 *
 * `instanceOnly` keeps just the instance's own pages (maintenance, system,
 * projects, accounts, the instance's audit) — what the neutral shell of a page outside any project
 * offers, where a project's members, audit or settings would belong to a
 * library the page is not about.
 */
export function adminItems(roles: {
  isAdmin: boolean
  isMaintainer: boolean
  instanceOnly?: boolean
}): NavEntry[] {
  return ADMIN_ITEMS.filter(
    (entry) =>
      (entry.gate === 'maintainer' ? roles.isMaintainer : roles.isAdmin) &&
      (roles.instanceOnly !== true || isInstancePath(entry.to)),
  )
}

/**
 * The curation-gated review game (`canCurate`). Top-level rather than buried in "Nástroje":
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
 * The upload entry, open to every signed-in role. Adding photos is the everyday loop's payoff, so
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
 * "Switch library": leaving the project for the chooser, the one place a
 * project is picked. It hangs off the account menu (the bar's user dropdown
 * and the drawer's account section), offered only when there is somewhere
 * else to go (`useLeaveProject`). It is an instance page, so the shell's
 * `useProjectPath` leaves it as it is — `/projects`, never under a project.
 */
export const PROJECTS_ITEM: NavEntry = {
  to: PROJECTS_PATH,
  labelKey: 'projectSwitcher.leave',
  titleKey: 'projectSwitcher.leaveTitle',
  icon: 'arrow-left-right',
}

/**
 * Reports whether `pathname` matches the given nav route, treating a route as
 * active for its detail sub-paths too (e.g. `/albums/ab12` activates `/albums`).
 * Used to light up the parent dropdown when any of its children is current.
 */
export function pathMatches(pathname: string, route: string): boolean {
  return pathname === route || pathname.startsWith(`${route}/`)
}

/**
 * One slot of the desktop navigation's main run, in the order the top bar
 * draws it from left to right and the sidebar from top to bottom: a link (the
 * upload one the filled call-to-action), a group of destinations (a dropdown in
 * the bar, a collapsible section in the sidebar), or the divider that fences
 * the quieter tools off from the everyday loop.
 */
export type NavSlot =
  | { kind: 'link'; entry: NavEntry; cta?: boolean }
  | { kind: 'group'; group: NavGroup }
  | { kind: 'divider' }

/**
 * Everything the desktop navigation offers one account, whichever way it is
 * laid out. **The one source of truth for both layouts:** the top bar and the
 * left sidebar render this and nothing else (plus the identity, search, bell and
 * shortcuts controls, which are not destinations), so a destination cannot exist
 * in one of them and not the other — `navItems.test` holds it.
 */
export interface DesktopNav {
  /** The library's navigation, in bar order; empty in the neutral shell. */
  main: NavSlot[]
  /** The account block of the user menu: account, my photos, statistics, help. */
  account: NavEntry[]
  /** The user menu's "Správa" section, per item by role; empty hides it. */
  admin: NavEntry[]
}

/**
 * Builds the {@link DesktopNav} for an account: the roles gate the review game,
 * the tools (per item) and the administration (per item); `neutral` (a page
 * outside any project) drops every library destination and keeps only the
 * instance's own administration; `subjectUid` offers "my photos" when the account
 * has said which person it is. "Switch library" is not here — whether it is
 * offered depends on the other projects (`useLeaveProject`), and it leads the
 * user menu in both layouts alike.
 */
export function desktopNav(opts: {
  canCurate: boolean
  canWrite: boolean
  isAdmin: boolean
  isMaintainer: boolean
  neutral: boolean
  subjectUid?: string | null
}): DesktopNav {
  const { canCurate, canWrite, isAdmin, isMaintainer, neutral } = opts
  const admin = adminItems({ isAdmin, isMaintainer, instanceOnly: neutral })
  if (neutral) {
    return { main: [], account: [ACCOUNT_ITEM], admin }
  }
  const tools = toolsGroup({ canCurate, canWrite })
  const myPhotos = myPhotosItem(opts.subjectUid)
  const main: NavSlot[] = [
    ...PRIMARY_ITEMS.map((entry): NavSlot => ({ kind: 'link', entry })),
    { kind: 'group', group: BROWSE_GROUP },
    ...(canCurate ? [{ kind: 'link', entry: REVIEW_ITEM } satisfies NavSlot] : []),
    { kind: 'link', entry: UPLOAD_ITEM, cta: true },
    ...(tools === null
      ? []
      : [{ kind: 'divider' } satisfies NavSlot, { kind: 'group', group: tools } satisfies NavSlot]),
  ]
  return {
    main,
    account: [ACCOUNT_ITEM, ...(myPhotos === null ? [] : [myPhotos]), STATS_ITEM, HELP_ITEM],
    admin,
  }
}

/**
 * Every route a {@link DesktopNav} leads to, in reading order: the main run with
 * each group unfolded in place, then the account block, then administration.
 * What a parity check compares two renderings against.
 */
export function desktopNavRoutes(nav: DesktopNav): string[] {
  const main = nav.main.flatMap((slot) =>
    slot.kind === 'link'
      ? [slot.entry.to]
      : slot.kind === 'group'
        ? slot.group.items.map((entry) => entry.to)
        : [],
  )
  return [...main, ...nav.account.map((entry) => entry.to), ...nav.admin.map((entry) => entry.to)]
}

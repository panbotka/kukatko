import { type RouteObject, matchRoutes } from 'react-router-dom'

import type { InstanceRole, ProjectMembership, Role } from '../services/auth'

/**
 * Where a project's pages live in the address bar: `/p/<slug>` + the page's
 * own path. Everything that reads or curates one library sits under it; only
 * the instance-level pages ({@link INSTANCE_PATH_PREFIXES}) do not.
 */
export const PROJECT_ROUTE_PREFIX = '/p/'

/**
 * The chooser every redirect falls back to when the account has no project it
 * may enter: no membership at all, or none that is approved and active.
 */
export const PROJECTS_PATH = '/projects'

/**
 * The instance-level pages, which belong to the account or the instance rather
 * than to a library and so never carry a project: signing in and registering,
 * the reset link and the public upload page, the account, the operations pages
 * and the project chooser. {@link projectPathFor} leaves these untouched, which
 * is what lets a single nav list mix both kinds of destination.
 */
export const INSTANCE_PATH_PREFIXES = [
  '/login',
  '/register',
  '/password-reset',
  '/u',
  '/account',
  '/system',
  '/maintenance',
  PROJECTS_PATH,
] as const

/** The leading `/p/<slug>` of a pathname, capturing the slug. */
const PROJECT_PREFIX = /^\/p\/([^/?#]+)/

/** The path part of `path`, i.e. everything before its `?` or `#`. */
function pathPart(path: string): string {
  const end = path.search(/[?#]/)
  return end === -1 ? path : path.slice(0, end)
}

/** True when `path` names an instance-level page (see {@link INSTANCE_PATH_PREFIXES}). */
export function isInstancePath(path: string): boolean {
  const own = pathPart(path)
  return INSTANCE_PATH_PREFIXES.some((prefix) => own === prefix || own.startsWith(`${prefix}/`))
}

/** The slug of the project a pathname lives in, or `null` outside `/p/<slug>`. */
export function projectSlugInPath(pathname: string): string | null {
  const match = PROJECT_PREFIX.exec(pathname)
  if (match === null) {
    return null
  }
  try {
    return decodeURIComponent(match[1])
  } catch {
    return match[1]
  }
}

/**
 * The address of the project page `path` in the project `slug`.
 *
 * `path` is the page's own absolute path as the app always wrote it
 * (`/photos/abc?x=1`, `/` for the library). It is returned unchanged when it is
 * an instance-level page, already carries a project (`/p/…`), or is not an
 * in-app path at all (an external URL); a `null` slug — an account with no
 * project to enter — sends every project page to the chooser.
 *
 * The library, `/`, becomes the bare `/p/<slug>` (with its query and hash), so
 * the project's home has one address rather than two.
 */
export function projectPathFor(slug: string | null, path: string): string {
  if (!path.startsWith('/') || path.startsWith('//')) {
    return path
  }
  if (path.startsWith(PROJECT_ROUTE_PREFIX) || isInstancePath(path)) {
    return path
  }
  if (slug === null) {
    return PROJECTS_PATH
  }
  const base = `${PROJECT_ROUTE_PREFIX}${encodeURIComponent(slug)}`
  return path === '/' || path.startsWith('/?') || path.startsWith('/#')
    ? `${base}${path.slice(1)}`
    : `${base}${path}`
}

/**
 * The page part of a pathname with its `/p/<slug>` removed (`/` for the
 * project's home), or the pathname itself outside a project. It is what a check
 * like "is this the albums page?" must compare against, now that the address
 * carries the project.
 */
export function stripProjectPrefix(pathname: string): string {
  const match = PROJECT_PREFIX.exec(pathname)
  if (match === null) {
    return pathname
  }
  const rest = pathname.slice(match[0].length)
  return rest === '' ? '/' : rest
}

/**
 * The pages that read the same in every project — a listing, a search, a tool
 * with no subject of its own — named by their single path segment (`''` is the
 * library, the project's home). Switching projects on one of these keeps the
 * page; see {@link routeAfterSwitch}. "My activity" is not here: it is
 * `/account/activity`, an instance page, which carries no project to switch.
 */
export const KEEP_ON_SWITCH: ReadonlySet<string> = new Set([
  '',
  'favorites',
  'albums',
  'labels',
  'tasks',
  'search',
  'saved',
  'map',
  'places',
  'people',
  'leaderboard',
  'upload',
  'stats',
  'help',
  'notifications',
])

/**
 * Where the project switcher takes the reader who moves from the project
 * `fromSlug` to `toSlug` while on `currentPath` (a pathname, optionally with
 * its query and hash).
 *
 * A **list** page (see {@link KEEP_ON_SWITCH}) — the albums, the people, the
 * search — exists in every project, so the reader lands on the same page of the
 * destination: `/p/v/albums` → `/p/rodina/albums`. Anything deeper carries a
 * uid (a photo, an album, a person, a task, a notification, a cluster…) or is a
 * tool scoped to one (`/people/clusters`, `/people/:uid/tree`), and a uid of one
 * library names nothing in another, so those land on the destination's home.
 * So does an instance page (`/account`) or an address outside `fromSlug`.
 *
 * The query and the hash are dropped on purpose: they hold filters, and those
 * name albums, people and labels by uid just as often as they hold free text.
 */
export function routeAfterSwitch(currentPath: string, fromSlug: string, toSlug: string): string {
  const home = projectPathFor(toSlug, '/')
  const path = pathPart(currentPath)
  if (projectSlugInPath(path) !== fromSlug) {
    return home
  }
  const page = stripProjectPrefix(path).replace(/\/+$/, '')
  const segment = page.replace(/^\//, '')
  if (segment.includes('/') || !KEEP_ON_SWITCH.has(segment)) {
    return home
  }
  return projectPathFor(toSlug, segment === '' ? '/' : `/${segment}`)
}

/** The membership fields the entry rule and the role predicates read. */
export interface SessionProjects {
  projects?: ProjectMembership[]
  last_project_slug?: string
}

/** True for a membership its holder may work in: approved, in an active project. */
export function isEnterable(membership: ProjectMembership): boolean {
  return membership.approved && membership.active
}

/**
 * The entry rule — where `/` and every redirect from an old address take the
 * account: the project it last worked in, when that is still an approved
 * membership of an active project; else its only such membership; else `null`,
 * which means the chooser at {@link PROJECTS_PATH}.
 *
 * Several enterable projects and no usable "last" one is `null` on purpose:
 * picking the first would be a guess about which library the person meant.
 */
export function entryProjectSlug(session: SessionProjects | null): string | null {
  const enterable = (session?.projects ?? []).filter(isEnterable)
  const last = session?.last_project_slug
  if (last !== undefined && last !== '' && enterable.some((m) => m.slug === last)) {
    return last
  }
  return enterable.length === 1 ? enterable[0].slug : null
}

/**
 * The role the account acts with in `membership`'s project: a maintainer (the
 * instance's system administrator) holds every power everywhere, anybody else
 * the role of their membership, and nobody without one any role at all.
 */
export function projectRoleOf(
  instanceRole: InstanceRole | null,
  membership: ProjectMembership | null,
): Role | null {
  if (instanceRole === 'maintainer') {
    return 'maintainer'
  }
  return membership?.role ?? null
}

/** True when `pathname` is one of the project's own pages (its catch-all excluded). */
export function isProjectPage(routes: RouteObject[], pathname: string): boolean {
  const matches = matchRoutes(routes, pathname)
  if (matches === null || matches.length === 0) {
    return false
  }
  return matches[matches.length - 1].route.path !== '*'
}

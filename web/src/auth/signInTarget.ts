import { isValidElement } from 'react'
import { type RouteObject, matchRoutes } from 'react-router-dom'

import { isEnterable, projectRoleOf, projectSlugInPath } from '../project/paths'
import {
  type GuardRole,
  type InstanceRole,
  type ProjectMembership,
  type Role,
  roleAtLeast,
} from '../services/auth'

import { RequireImport, RequireRole } from './ProtectedRoute'

/**
 * Where a sign-in lands when the address it was meant to return to is not one
 * the signed-in account may use: `/`, the front door, which forwards into the
 * account's project or to the chooser — the very place a sign-in with no return
 * target reaches.
 */
export const DEFAULT_LANDING = '/'

/** What a return target is judged against: who has just signed in. */
export interface SignedInAccount {
  instanceRole: InstanceRole | null
  projects: ProjectMembership[]
}

/** The path part of `target`, i.e. everything before its `?` or `#`. */
function pathPart(target: string): string {
  const end = target.search(/[?#]/)
  return end === -1 ? target : target.slice(0, end)
}

/** The least role a route element demands, or null for an element that gates nothing. */
function gateOf(element: RouteObject['element']): GuardRole | null {
  if (!isValidElement(element)) {
    return null
  }
  if (element.type === RequireRole) {
    return (element.props as { role: GuardRole }).role
  }
  if (element.type === RequireImport) {
    return 'maintainer'
  }
  return null
}

/**
 * True when `account` may use the page at `target` (a path, optionally with its
 * query and hash), judged against `routes` — the app's guarded route table.
 *
 * Two things can refuse it, the same two the pages themselves check:
 *
 * - a `/p/<slug>/…` page needs an approved membership of an active `<slug>`
 *   (the system administrator enters any project). A project the account is not
 *   in and one that does not exist are refused alike, so the answer reveals
 *   nothing about foreign projects;
 * - every `RequireRole`/`RequireImport` on the matched branch must be met, by
 *   the role in that project — or, outside a project, by the instance role,
 *   which only a maintainer turns into a role at all.
 *
 * The gates are read off the route elements themselves, so the answer cannot
 * drift from what the page would show. An address the table does not know (a
 * public page such as an invitation) has no gate and is allowed.
 */
export function mayUseTarget(
  routes: RouteObject[],
  target: string,
  account: SignedInAccount,
): boolean {
  const pathname = pathPart(target)
  let role: Role | null = account.instanceRole === 'maintainer' ? 'maintainer' : null
  const slug = projectSlugInPath(pathname)
  if (slug !== null) {
    const membership =
      account.projects.find((candidate) => candidate.slug === slug && isEnterable(candidate)) ??
      null
    if (membership === null && account.instanceRole !== 'maintainer') {
      return false
    }
    role = projectRoleOf(account.instanceRole, membership)
  }
  for (const match of matchRoutes(routes, pathname) ?? []) {
    const required = gateOf(match.route.element)
    if (required !== null && (role === null || !roleAtLeast(role, required))) {
      return false
    }
  }
  return true
}

/**
 * Where a sign-in by `account` goes: `target` when the account may use it (see
 * {@link mayUseTarget}), else {@link DEFAULT_LANDING}. The fallback is silent —
 * no error page, and never a flash of the page that would have refused them.
 */
export function signInTarget(
  routes: RouteObject[],
  target: string,
  account: SignedInAccount,
): string {
  return mayUseTarget(routes, target, account) ? target : DEFAULT_LANDING
}

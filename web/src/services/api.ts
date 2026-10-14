/**
 * The one place that knows where the API lives. Every service builds its URLs
 * through these helpers and never hardcodes `/api/v1` itself.
 *
 * The API has two halves. Instance-wide routes (sign-in and the account, push
 * subscriptions, jobs, system, backup, maintenance, restore, audit, user
 * pictures, the public upload-link page, the anonymous settings read) sit
 * directly under `/api/v1` — use {@link instancePath}. Everything that reads or
 * writes a library's catalogue belongs to one project and sits under
 * `/api/v1/p/<slug>` — use {@link apiPath}.
 *
 * The current project is plain module state rather than a React context so a
 * service function stays a plain function. `AuthProvider` sets it from
 * `GET /auth/me` and clears it on sign-out.
 */

/** The root of every API route. */
const API_ROOT = '/api/v1'

let currentProjectSlug: string | null = null

/**
 * Records the project the project-scoped calls go to, or `null` when there is
 * none (signed out, or still loading the session).
 */
export function setCurrentProjectSlug(slug: string | null): void {
  currentProjectSlug = slug
}

/** The slug {@link apiPath} prefixes, or `null` when no project is current. */
export function getCurrentProjectSlug(): string | null {
  return currentProjectSlug
}

/**
 * The URL of an instance-wide route: `/api/v1` + `path` (which starts with
 * `/`, e.g. `instancePath('/auth/me')`).
 */
export function instancePath(path: string): string {
  return `${API_ROOT}${path}`
}

/**
 * The URL of a project-scoped route in the current project:
 * `/api/v1/p/<slug>` + `path` (e.g. `apiPath('/photos')`).
 *
 * @throws Error when no project is current. That is a bug, not a state: only a
 *   signed-in page calls a project route, and signing in sets the project. An
 *   anonymous page must use {@link instancePath}.
 */
export function apiPath(path: string): string {
  if (currentProjectSlug === null) {
    throw new Error(`no current project (requested ${path})`)
  }
  return `${API_ROOT}/p/${encodeURIComponent(currentProjectSlug)}${path}`
}

/**
 * The URL of a project-scoped route in the named project:
 * `/api/v1/p/<project>` + `path`. For an instance-level page that acts on a
 * project the maintainer picked (the nameless-subject repair on `/maintenance`)
 * — the request must say which project, never fall back on the current one.
 */
export function projectApiPath(project: string, path: string): string {
  return `${API_ROOT}/p/${encodeURIComponent(project)}${path}`
}

import { Navigate, type RouteObject, useLocation } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { NotFoundPage } from '../pages/NotFoundPage'

import { PROJECTS_PATH, entryProjectSlug, isProjectPage, projectPathFor } from './paths'

/** The project the entry rule sends the signed-in account to, or null for the chooser. */
function useEntrySlug(): string | null {
  const { projects, lastProjectSlug } = useAuth()
  return entryProjectSlug({ projects, last_project_slug: lastProjectSlug ?? '' })
}

/**
 * The front door, `/`: forwards into the project the entry rule picks — the
 * one the account last worked in, else its only one — or to the chooser when
 * there is no single project to pick. The query and hash ride along, so an old
 * `/?q=…` link still opens the library filtered. The history entry is replaced:
 * Back from the library must not land on `/` and be forwarded again.
 */
export function EntryRedirect() {
  const slug = useEntrySlug()
  const { search, hash } = useLocation()
  return <Navigate to={projectPathFor(slug, `/${search}${hash}`)} replace />
}

/**
 * The fallback for every address outside a project. One that names a project
 * page from before projects existed — `/photos/abc`, `/albums`, `/n/xyz`, the
 * share sheet's `/share-target?share=…` — is forwarded to the same page in the
 * entry project, query string and hash intact (or to the chooser when there is
 * no single project to pick); anything else is simply not found.
 *
 * `routes` are the project's route objects, so the set of addresses recognised
 * here is the set of pages mounted under `/p/:slug` and cannot drift from it.
 */
export function LegacyRedirect({ routes }: { routes: RouteObject[] }) {
  const slug = useEntrySlug()
  const { pathname, search, hash } = useLocation()
  if (!isProjectPage(routes, pathname)) {
    return <NotFoundPage />
  }
  const target = slug === null ? PROJECTS_PATH : projectPathFor(slug, `${pathname}${search}${hash}`)
  return <Navigate to={target} replace />
}

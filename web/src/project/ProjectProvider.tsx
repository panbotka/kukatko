import { type ReactNode, useCallback, useContext, useMemo } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'

import { AuthContext } from '../auth/AuthContext'
import { setCurrentProjectSlug } from '../services/api'

import { ProjectContext, type ProjectContextValue, type ProjectStatus } from './ProjectContext'
import { entryProjectSlug, isEnterable, projectSlugInPath, routeAfterSwitch } from './paths'

/**
 * Provides the current project to the app, directly under `AuthProvider`.
 *
 * The project is read from the address — `/p/:slug` — and matched against the
 * account's memberships from `GET /auth/me`. A match makes it current; a slug
 * the account has no approved membership of in an active project is
 * `not-member`, which the route table answers with the ordinary not-found page,
 * so a stranger cannot even learn that the project exists. Pages outside a
 * project (`/account`, `/system`, …) work in the project the entry rule picks.
 *
 * The slug is handed to the API layer (`setCurrentProjectSlug`) during render,
 * not in an effect: the pages below fetch in their own effects, and React runs
 * a child's effects before its parent's, so an effect here would come too late
 * for the very first request of a freshly opened project.
 */
export function ProjectProvider({ children }: { children: ReactNode }) {
  const auth = useContext(AuthContext)
  const { pathname } = useLocation()
  const navigate = useNavigate()

  const authStatus = auth?.status ?? 'loading'
  const projects = useMemo(() => auth?.projects ?? [], [auth?.projects])
  const lastProjectSlug = auth?.lastProjectSlug ?? ''
  const urlSlug = projectSlugInPath(pathname)

  const { project, status } = useMemo((): {
    project: ProjectContextValue['project']
    status: ProjectStatus
  } => {
    if (authStatus === 'loading') {
      return { project: null, status: 'loading' }
    }
    const entrySlug = entryProjectSlug({ projects, last_project_slug: lastProjectSlug })
    const entry = projects.find((m) => m.slug === entrySlug) ?? null
    if (urlSlug === null) {
      return { project: entry, status: 'ready' }
    }
    const named = projects.find((m) => m.slug === urlSlug && isEnterable(m))
    return named === undefined
      ? { project: entry, status: 'not-member' }
      : { project: named, status: 'ready' }
  }, [authStatus, projects, lastProjectSlug, urlSlug])

  if (authStatus !== 'loading') {
    setCurrentProjectSlug(project?.slug ?? null)
  }

  const currentSlug = project?.slug ?? ''
  const switchProject = useCallback(
    (slug: string) => {
      void navigate(routeAfterSwitch(pathname, currentSlug, slug))
    },
    [navigate, pathname, currentSlug],
  )

  // The project is the page's own only when the address names it and the
  // account belongs to it; on `/account` it is merely the entry-rule fallback.
  const inAddress = status === 'ready' && urlSlug !== null

  const value = useMemo<ProjectContextValue>(
    () => ({ project, projects, status, inAddress, switchProject }),
    [project, projects, status, inAddress, switchProject],
  )

  return <ProjectContext.Provider value={value}>{children}</ProjectContext.Provider>
}

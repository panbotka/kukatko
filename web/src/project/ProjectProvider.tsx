import { type ReactNode, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'

import { AuthContext } from '../auth/AuthContext'
import { setCurrentProjectSlug } from '../services/api'
import type { ProjectMembership } from '../services/auth'
import { type AdminProject, fetchAdminProjects } from '../services/projects'

import { ProjectContext, type ProjectContextValue, type ProjectStatus } from './ProjectContext'
import { entryProjectSlug, isEnterable, projectSlugInPath, routeAfterSwitch } from './paths'

/**
 * Provides the current project to the app, directly under `AuthProvider`.
 *
 * The project is read from the address — `/p/:slug` — and matched against the
 * account's memberships from `GET /auth/me`. A match makes it current; a slug
 * the account has no approved membership of in an active project is
 * `not-member`, which the route table answers with the ordinary not-found page,
 * so a stranger cannot even learn that the project exists — except a membership
 * of an active project still waiting for approval, which is `pending`: the
 * account already knows that project, and "waiting for approval" is the one
 * thing worth telling it — and a membership of a deactivated project, which is
 * `inactive`: the chooser already lists it as unavailable. Pages outside a project (`/account`, `/system`, …)
 * work in the project the entry rule picks.
 *
 * **The maintainer enters every project**, exactly as the API lets it: a
 * deactivated one, one it is a member of but not approved in, and one it is no
 * member of at all. The last kind is not in `GET /auth/me`, so it is looked up
 * in the system administrator's project list (`GET /projects`) — the status is
 * `loading` meanwhile, and `not-member` only for a slug no project has.
 *
 * The slug is handed to the API layer (`setCurrentProjectSlug`) during render,
 * not in an effect: the pages below fetch in their own effects, and React runs
 * a child's effects before its parent's, so an effect here would come too late
 * for the very first request of a freshly opened project.
 * A layout effect would beat the children's fetches but not their render, and
 * the media URL builders (`thumbUrl`, `subjectAvatarUrl`, `faceCropUrl`,
 * `tileLayerUrl`, …) call `apiPath` while rendering: the first commit after a
 * sign-in would throw (no slug yet) and the first after a switch would point
 * every `<img>` at the previous project. Moving the assignment out of render
 * means passing the slug into each of those builders first; until then a render
 * React discards can leave the slug a step ahead, which the server's scoping
 * bounds to a 404 or the next project's answer to a request the outlet's
 * remount aborts anyway.
 */
export function ProjectProvider({ children }: { children: ReactNode }) {
  const auth = useContext(AuthContext)
  const { pathname } = useLocation()
  const navigate = useNavigate()

  const authStatus = auth?.status ?? 'loading'
  const projects = useMemo(() => auth?.projects ?? [], [auth?.projects])
  const lastProjectSlug = auth?.lastProjectSlug ?? ''
  const isMaintainer = auth?.instanceRole === 'maintainer'
  const urlSlug = projectSlugInPath(pathname)
  const lookup = useMaintainerLookup(
    authStatus === 'authenticated' &&
      isMaintainer &&
      urlSlug !== null &&
      !projects.some((m) => m.slug === urlSlug)
      ? urlSlug
      : null,
  )

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
    const membership = projects.find((m) => m.slug === urlSlug)
    if (membership !== undefined && isEnterable(membership)) {
      return { project: membership, status: 'ready' }
    }
    if (isMaintainer) {
      if (membership !== undefined) {
        // Deactivated, or never approved: neither stops the system administrator.
        return { project: { ...membership, approved: true }, status: 'ready' }
      }
      if (lookup?.slug !== urlSlug) {
        return { project: entry, status: 'loading' }
      }
      return lookup.project === null
        ? { project: entry, status: 'not-member' }
        : { project: lookup.project, status: 'ready' }
    }
    if (membership?.active === true && !membership.approved) {
      return { project: entry, status: 'pending' }
    }
    if (membership !== undefined && !membership.active) {
      return { project: entry, status: 'inactive' }
    }
    return { project: entry, status: 'not-member' }
  }, [authStatus, projects, lastProjectSlug, urlSlug, isMaintainer, lookup])

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

/** What {@link useMaintainerLookup} found for one slug. */
interface Lookup {
  slug: string
  /** The project as the maintainer enters it, or null when no project has the slug. */
  project: ProjectMembership | null
}

/**
 * The project `slug` as a maintainer who is no member of it enters it: as its
 * admin, with no "this is me" person. A deactivated project keeps `active:
 * false`, which is what the shell's "deactivated" banner reads.
 */
function maintainerEntry(project: AdminProject): ProjectMembership {
  return {
    uid: project.uid,
    slug: project.slug,
    name: project.name,
    description: project.description,
    active: project.active,
    role: 'admin',
    approved: true,
    subject_uid: null,
  }
}

/**
 * Looks the project `slug` up in the system administrator's project list, for
 * a maintainer who opened a project it is no member of; `null` asks nothing.
 * The answer is tagged with its slug, so a stale one (the address moved on) is
 * told apart from the current. A failed lookup reads as "no such project" —
 * the ordinary not-found page, which is what the maintainer would see anyway
 * if the project were gone.
 */
function useMaintainerLookup(slug: string | null): Lookup | null {
  const [lookup, setLookup] = useState<Lookup | null>(null)
  useEffect(() => {
    if (slug === null) {
      return
    }
    const controller = new AbortController()
    fetchAdminProjects(controller.signal)
      .then((list) => {
        const found = list.find((p) => p.slug === slug)
        setLookup({ slug, project: found === undefined ? null : maintainerEntry(found) })
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          setLookup({ slug, project: null })
        }
      })
    return () => {
      controller.abort()
    }
  }, [slug])
  return lookup
}

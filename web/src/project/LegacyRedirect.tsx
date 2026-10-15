import { useEffect, useState } from 'react'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Navigate, type RouteObject, useLocation } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { NotFoundPage } from '../pages/NotFoundPage'
import { type EntityKind, resolveEntityProject } from '../services/resolve'

import {
  PROJECTS_PATH,
  entryProjectSlug,
  isProjectPage,
  legacyEntityOf,
  projectPathFor,
} from './paths'

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
 * page from before projects existed — `/photos/abc`, `/albums`, `/n/xyz` — is
 * forwarded to the same page in a
 * project, query string and hash intact; anything else is simply not found.
 *
 * **Which project depends on the page.** An old link to one entity — a photo,
 * an album, a label, a task, a person, a notification — belongs to the
 * project that holds that entity, which the backend's resolver names (`GET
 * /resolve/{kind}/{uid}`) when the reader may enter it: an old e-mail or
 * bookmark keeps opening its photo even after the reader moved on to another
 * project. When the resolver knows no project the reader may enter, the link
 * leads nowhere and the page says so; when it cannot be asked (rate limit,
 * server down) the entry rule is the best remaining guess. A page about no
 * single entity (`/albums`, `/map`) goes to the entry project — the one the
 * account last worked in, else its only one — or to the chooser when there is
 * no single project to pick.
 *
 * `routes` are the project's route objects, so the set of addresses recognised
 * here is the set of pages mounted under `/p/:slug` and cannot drift from it.
 */
export function LegacyRedirect({ routes }: { routes: RouteObject[] }) {
  const { pathname } = useLocation()
  if (!isProjectPage(routes, pathname)) {
    return <NotFoundPage />
  }
  const entity = legacyEntityOf(pathname)
  return entity === null ? (
    <EntryProjectRedirect />
  ) : (
    <EntityRedirect key={`${entity.kind}/${entity.uid}`} kind={entity.kind} uid={entity.uid} />
  )
}

/** Forwards the current address into the entry project, or to the chooser. */
function EntryProjectRedirect() {
  const slug = useEntrySlug()
  const { pathname, search, hash } = useLocation()
  const target = slug === null ? PROJECTS_PATH : projectPathFor(slug, `${pathname}${search}${hash}`)
  return <Navigate to={target} replace />
}

/** Where the resolver sent an old entity link: a slug, nowhere, or "could not ask". */
type Resolution =
  | { state: 'resolving' }
  | { state: 'found'; slug: string }
  | { state: 'none' }
  | { state: 'failed' }

/**
 * Asks the resolver which project the `kind` entity `uid` lives in and forwards
 * the current address there (see {@link LegacyRedirect}).
 */
function EntityRedirect({ kind, uid }: { kind: EntityKind; uid: string }) {
  const { t } = useTranslation()
  const { pathname, search, hash } = useLocation()
  const [resolution, setResolution] = useState<Resolution>({ state: 'resolving' })

  useEffect(() => {
    const controller = new AbortController()
    resolveEntityProject(kind, uid, controller.signal)
      .then((slug) => {
        setResolution(slug === null ? { state: 'none' } : { state: 'found', slug })
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          setResolution({ state: 'failed' })
        }
      })
    return () => {
      controller.abort()
    }
  }, [kind, uid])

  switch (resolution.state) {
    case 'resolving':
      return (
        <div className="d-flex justify-content-center py-5">
          <Spinner animation="border" role="status">
            <span className="visually-hidden">{t('auth.loading')}</span>
          </Spinner>
        </div>
      )
    case 'found':
      return (
        <Navigate to={projectPathFor(resolution.slug, `${pathname}${search}${hash}`)} replace />
      )
    case 'none':
      return <NotFoundPage />
    case 'failed':
      return <EntryProjectRedirect />
  }
}

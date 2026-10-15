import { createContext, useCallback, useContext } from 'react'

import { getCurrentProjectSlug } from '../services/api'
import type { ProjectMembership } from '../services/auth'

import { projectPathFor } from './paths'

/**
 * Whether the project named in the address is one the account may work in:
 * `loading` until the session is known (and, for a maintainer, until the
 * project it is no member of has been looked up); `pending` when the account's
 * membership of that active project still waits for its admin's approval — the
 * one refusal worth explaining, since the account already knows the project
 * exists; `not-member` when the address names a project the account may not
 * enter at all (no membership, a deactivated project, or no such project — the
 * three are indistinguishable on purpose); `ready` otherwise.
 */
export type ProjectStatus = 'loading' | 'ready' | 'pending' | 'not-member'

/** Value exposed by {@link ProjectProvider} through {@link useProject}. */
export interface ProjectContextValue {
  /**
   * The project the app works in: the one named by `/p/:slug` when the account
   * may enter it (an approved member of an active project, or the maintainer,
   * who enters every project — a deactivated one keeps `active: false`), else — on an instance-level page such as `/account`, or a
   * `not-member` address — the project the entry rule picks, so the shell's
   * links and counts still point somewhere real. `null` only for an account
   * with no project to enter.
   */
  project: ProjectMembership | null
  /** Every membership `GET /auth/me` listed, entered or not. */
  projects: ProjectMembership[]
  status: ProjectStatus
  /**
   * True when the address itself names `project` (`/p/:slug`, a member): the
   * page belongs to it. False on an instance page, where `project` is only the
   * entry-rule fallback — the tab title and the switcher's links read this.
   */
  inAddress: boolean
  /**
   * Moves to the project `slug`: the same page when it is a list every project
   * has, the project's home otherwise (see `routeAfterSwitch`).
   */
  switchProject: (slug: string) => void
}

export const ProjectContext = createContext<ProjectContextValue | null>(null)

/**
 * Accesses the current project. Must be called within a {@link ProjectProvider}.
 *
 * @throws Error when used outside of a `ProjectProvider`.
 */
export function useProject(): ProjectContextValue {
  const ctx = useContext(ProjectContext)
  if (ctx === null) {
    throw new Error('useProject must be used within a ProjectProvider')
  }
  return ctx
}

/**
 * True when the shell must not wear any project's colours: on an instance page
 * (`/account`, `/system…`), and on a `/p/:slug` the account may not enter (an
 * unknown or foreign slug, a membership still waiting for approval). There the
 * project in the context is only the entry rule's fallback, and its name, its
 * navigation, its banner or its welcome would claim the page for a library it
 * does not belong to. Outside a provider (a component test) it is false.
 */
export function useNeutralChrome(): boolean {
  const ctx = useContext(ProjectContext)
  return ctx !== null && !ctx.inAddress
}

/**
 * The slug the app works in, or `null` for none. Outside a provider — a
 * component test mounting one piece of the tree — it is the slug the API calls
 * go to, which the provider keeps equal to its own anyway.
 */
export function useProjectSlug(): string | null {
  const ctx = useContext(ProjectContext)
  return ctx === null ? getCurrentProjectSlug() : (ctx.project?.slug ?? null)
}

/**
 * The display name of the project the app works in, or `null` for none — and
 * outside a provider (a component test), where there is no project to name.
 */
export function useProjectName(): string | null {
  const ctx = useContext(ProjectContext)
  return ctx?.project?.name ?? null
}

/**
 * The function every in-app link goes through: it turns a page's own path
 * (`/photos/abc`) into its address in the current project
 * (`/p/<slug>/photos/abc`). Instance-level pages, addresses already carrying a
 * project and external URLs pass through unchanged — see {@link projectPathFor}.
 */
export function useProjectPath(): (path: string) => string {
  const slug = useProjectSlug()
  return useCallback((path: string) => projectPathFor(slug, path), [slug])
}

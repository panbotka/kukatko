import { createContext, useCallback, useContext } from 'react'

import { getCurrentProjectSlug } from '../services/api'
import type { ProjectMembership } from '../services/auth'

import { projectPathFor } from './paths'

/**
 * Whether the project named in the address is one the account may work in:
 * `loading` until the session is known, `not-member` when the address names a
 * project the account has no approved membership of (or that does not exist —
 * the two are indistinguishable on purpose), `ready` otherwise.
 */
export type ProjectStatus = 'loading' | 'ready' | 'not-member'

/** Value exposed by {@link ProjectProvider} through {@link useProject}. */
export interface ProjectContextValue {
  /**
   * The project the app works in: the one named by `/p/:slug` when the account
   * belongs to it, else — on an instance-level page such as `/account`, or a
   * `not-member` address — the project the entry rule picks, so the shell's
   * links and counts still point somewhere real. `null` only for an account
   * with no project to enter.
   */
  project: ProjectMembership | null
  /** Every membership `GET /auth/me` listed, entered or not. */
  projects: ProjectMembership[]
  status: ProjectStatus
  /** Moves to the home page of the project `slug`. */
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
 * The slug the app works in, or `null` for none. Outside a provider — a
 * component test mounting one piece of the tree — it is the slug the API calls
 * go to, which the provider keeps equal to its own anyway.
 */
export function useProjectSlug(): string | null {
  const ctx = useContext(ProjectContext)
  return ctx === null ? getCurrentProjectSlug() : (ctx.project?.slug ?? null)
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

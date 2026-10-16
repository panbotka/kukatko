import { useContext } from 'react'

import { AuthContext } from '../auth/AuthContext'
import { LIBRARY_PATH } from '../lib/libraryView'
import { readLastProject } from '../lib/lastProject'
import type { ProjectMembership } from '../services/auth'

import { ProjectContext } from './ProjectContext'
import { PROJECTS_PATH, isEnterable, projectPathFor } from './paths'

/**
 * The project a page outside one leads back to: the one the account was last
 * in on this browser (`remembered`), when that is still a membership it may
 * enter; else its only enterable project; else `null`, which means the chooser
 * at {@link PROJECTS_PATH}.
 *
 * Several enterable projects and nothing usable remembered is `null` on
 * purpose, as in the entry rule: picking the first would be a guess. A
 * remembered project the account is no member of — a maintainer who looked into
 * a stranger's library — is not one this list can name, so it falls back too.
 */
export function backTarget(
  remembered: string | null,
  projects: readonly ProjectMembership[],
): ProjectMembership | null {
  const enterable = projects.filter(isEnterable)
  if (remembered !== null) {
    const found = enterable.find((m) => m.slug === remembered)
    if (found !== undefined) {
      return found
    }
  }
  return enterable.length === 1 ? enterable[0] : null
}

/** What {@link useBackToProject} reports. */
export interface BackToProject {
  /** The project to go back to, or `null` when the way back is the chooser. */
  project: ProjectMembership | null
  /** Where the way back leads: the project's library, or `/projects`. */
  to: string
}

/**
 * The way back into a project from a page outside one (`/account`,
 * `/system…`): {@link backTarget} over the project remembered for the signed-in
 * account (`lib/lastProject.ts`) and its memberships. `null` outside a provider
 * (an isolated component test) and without a signed-in account, where there is
 * nobody to lead anywhere.
 */
export function useBackToProject(): BackToProject | null {
  const ctx = useContext(ProjectContext)
  const uid = useContext(AuthContext)?.user?.uid
  if (ctx === null || uid === undefined) {
    return null
  }
  const project = backTarget(readLastProject(uid), ctx.projects)
  return {
    project,
    to: project === null ? PROJECTS_PATH : projectPathFor(project.slug, LIBRARY_PATH),
  }
}

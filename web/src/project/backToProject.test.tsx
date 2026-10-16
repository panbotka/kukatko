import { renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { writeLastProject } from '../lib/lastProject'
import type { ProjectMembership } from '../services/auth'

import { backTarget, useBackToProject } from './backToProject'
import { ProjectContext, type ProjectContextValue } from './ProjectContext'

/** A membership of the project `slug`, approved and active unless `extra` says otherwise. */
function member(
  slug: string,
  name: string,
  extra: Partial<ProjectMembership> = {},
): ProjectMembership {
  return { uid: `p-${slug}`, slug, name, active: true, role: 'viewer', approved: true, ...extra }
}

const v = member('v', 'Veselice')
const rodina = member('rodina', 'Rodina')

beforeEach(() => {
  window.localStorage.clear()
})

describe('backTarget', () => {
  it('leads to the remembered project while the account may enter it', () => {
    expect(backTarget('rodina', [v, rodina])).toBe(rodina)
  })

  it('falls back to the only enterable project', () => {
    expect(backTarget(null, [v])).toBe(v)
    // Remembered but no longer enterable: deactivated, or not approved, or gone.
    expect(backTarget('rodina', [v, member('rodina', 'Rodina', { active: false })])).toBe(v)
    expect(backTarget('rodina', [v, member('rodina', 'Rodina', { approved: false })])).toBe(v)
    expect(backTarget('zmizely', [v])).toBe(v)
  })

  it('falls back to the chooser with no single project to name', () => {
    expect(backTarget(null, [v, rodina])).toBeNull()
    expect(backTarget('zmizely', [v, rodina])).toBeNull()
    expect(backTarget(null, [])).toBeNull()
    expect(backTarget(null, [member('v', 'Veselice', { active: false })])).toBeNull()
  })
})

describe('useBackToProject', () => {
  /** Providers for account `uid` (none for undefined) with memberships `projects`. */
  function wrapper(uid: string | undefined, projects: ProjectMembership[] | null) {
    const auth = { user: uid === undefined ? null : { uid } } as unknown as AuthContextValue
    const ctx: ProjectContextValue | null =
      projects === null
        ? null
        : {
            project: projects[0] ?? null,
            projects,
            status: 'ready',
            inAddress: false,
            switchProject: vi.fn(),
          }
    return ({ children }: { children: ReactNode }) => (
      <AuthContext.Provider value={auth}>
        <ProjectContext.Provider value={ctx}>{children}</ProjectContext.Provider>
      </AuthContext.Provider>
    )
  }

  it("leads to the remembered project's library", () => {
    writeLastProject('u1', 'rodina')
    const { result } = renderHook(() => useBackToProject(), { wrapper: wrapper('u1', [v, rodina]) })

    expect(result.current).toEqual({ project: rodina, to: '/p/rodina' })
  })

  it("does not follow another account's memory", () => {
    writeLastProject('u2', 'rodina')
    const { result } = renderHook(() => useBackToProject(), { wrapper: wrapper('u1', [v, rodina]) })

    expect(result.current).toEqual({ project: null, to: '/projects' })
  })

  it("leads to the only project's library when nothing is remembered", () => {
    const { result } = renderHook(() => useBackToProject(), { wrapper: wrapper('u1', [v]) })

    expect(result.current).toEqual({ project: v, to: '/p/v' })
  })

  it('leads nowhere without an account or a provider', () => {
    expect(
      renderHook(() => useBackToProject(), { wrapper: wrapper(undefined, [v]) }).result.current,
    ).toBeNull()
    expect(
      renderHook(() => useBackToProject(), { wrapper: wrapper('u1', null) }).result.current,
    ).toBeNull()
  })
})

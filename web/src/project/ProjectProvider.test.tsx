import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import { AuthContext, type AuthContextValue, useAuth } from '../auth/AuthContext'
import { getCurrentProjectSlug, setCurrentProjectSlug } from '../services/api'
import type { InstanceRole, ProjectMembership } from '../services/auth'

import { useProject, useProjectPath } from './ProjectContext'
import { ProjectProvider } from './ProjectProvider'

const MEMBERSHIPS: ProjectMembership[] = [
  { uid: 'pv', slug: 'v', name: 'Veselice', active: true, role: 'editor', approved: true },
  { uid: 'pr', slug: 'rodina', name: 'Rodina', active: true, role: 'viewer', approved: true },
]

/**
 * A signed-in account whose `/auth/me` role is `viewer` but which edits in `v`
 * and only looks in `rodina` — so a predicate that still read the account-wide
 * role would be caught.
 */
function auth(instanceRole: InstanceRole = 'user'): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'U', role: 'viewer' },
    role: 'viewer',
    instanceRole,
    projects: MEMBERSHIPS,
    lastProjectSlug: 'rodina',
    canCurate: false,
    canWrite: false,
    isAdmin: false,
    isMaintainer: instanceRole === 'maintainer',
    canImport: instanceRole === 'maintainer',
  } as unknown as AuthContextValue
}

/** Prints what the provider and `useAuth` report for the current address. */
function Probe() {
  const { project, status } = useProject()
  const { role, canCurate, canWrite, isAdmin, isMaintainer, canImport } = useAuth()
  const projectPath = useProjectPath()
  return (
    <dl>
      <dd data-testid="status">{status}</dd>
      <dd data-testid="project">{project?.slug ?? '-'}</dd>
      <dd data-testid="role">{role ?? '-'}</dd>
      <dd data-testid="flags">
        {[canCurate, canWrite, isAdmin, isMaintainer, canImport].map(String).join(',')}
      </dd>
      <dd data-testid="link">{projectPath('/albums')}</dd>
    </dl>
  )
}

function renderAt(path: string, value: AuthContextValue = auth()) {
  return render(
    <AuthContext.Provider value={value}>
      <MemoryRouter initialEntries={[path]}>
        <ProjectProvider>
          <Probe />
        </ProjectProvider>
      </MemoryRouter>
    </AuthContext.Provider>,
  )
}

beforeEach(() => {
  setCurrentProjectSlug(null)
})

describe('ProjectProvider', () => {
  it('makes the project the address names current, links and API calls included', () => {
    renderAt('/p/v/albums')

    expect(screen.getByTestId('status')).toHaveTextContent('ready')
    expect(screen.getByTestId('project')).toHaveTextContent('v')
    expect(screen.getByTestId('link')).toHaveTextContent('/p/v/albums')
    expect(getCurrentProjectSlug()).toBe('v')
  })

  it('reads the role predicates from the current project, not the account', () => {
    const { unmount } = renderAt('/p/v')
    expect(screen.getByTestId('role')).toHaveTextContent('editor')
    // canCurate, canWrite, isAdmin, isMaintainer, canImport
    expect(screen.getByTestId('flags')).toHaveTextContent('true,true,false,false,false')
    unmount()

    renderAt('/p/rodina')
    expect(screen.getByTestId('role')).toHaveTextContent('viewer')
    expect(screen.getByTestId('flags')).toHaveTextContent('false,false,false,false,false')
  })

  it('gives the instance maintainer every power in any project it belongs to', () => {
    renderAt('/p/rodina', auth('maintainer'))

    expect(screen.getByTestId('role')).toHaveTextContent('maintainer')
    expect(screen.getByTestId('flags')).toHaveTextContent('true,true,true,true,true')
  })

  it('reports a project the account does not belong to as not-member', () => {
    renderAt('/p/elsewhere/albums')

    expect(screen.getByTestId('status')).toHaveTextContent('not-member')
    // The shell keeps working in the entry project, never in the foreign one.
    expect(screen.getByTestId('link')).toHaveTextContent('/p/rodina/albums')
    expect(getCurrentProjectSlug()).toBe('rodina')
  })

  it('works in the entry project on an instance page', () => {
    renderAt('/account')

    expect(screen.getByTestId('status')).toHaveTextContent('ready')
    expect(screen.getByTestId('project')).toHaveTextContent('rodina')
    expect(screen.getByTestId('link')).toHaveTextContent('/p/rodina/albums')
  })

  it('has no project for a visitor, and points project links at the chooser', () => {
    renderAt('/p/v/albums', {
      ...auth(),
      status: 'unauthenticated',
      user: null,
      role: null,
      instanceRole: null,
      projects: [],
      lastProjectSlug: null,
    })

    expect(screen.getByTestId('project')).toHaveTextContent('-')
    expect(screen.getByTestId('role')).toHaveTextContent('-')
    expect(screen.getByTestId('link')).toHaveTextContent('/projects')
    expect(getCurrentProjectSlug()).toBeNull()
  })

  describe('switchProject', () => {
    /** Switches to `rodina` on a click, and prints where the app went. */
    function Switch() {
      const { switchProject, inAddress } = useProject()
      const { pathname } = useLocation()
      return (
        <>
          <button
            type="button"
            onClick={() => {
              switchProject('rodina')
            }}
          >
            switch
          </button>
          <span data-testid="where">{pathname}</span>
          <span data-testid="in-address">{String(inAddress)}</span>
        </>
      )
    }

    function renderSwitch(path: string) {
      return render(
        <AuthContext.Provider value={auth()}>
          <MemoryRouter initialEntries={[path]}>
            <ProjectProvider>
              <Switch />
            </ProjectProvider>
          </MemoryRouter>
        </AuthContext.Provider>,
      )
    }

    it('keeps a list page every project has', async () => {
      const user = userEvent.setup()
      renderSwitch('/p/v/people')
      expect(screen.getByTestId('in-address')).toHaveTextContent('true')

      await user.click(screen.getByRole('button', { name: 'switch' }))

      expect(screen.getByTestId('where')).toHaveTextContent(/^\/p\/rodina\/people$/)
    })

    it('goes to the destination home from a page with a uid', async () => {
      const user = userEvent.setup()
      renderSwitch('/p/v/people/s1')

      await user.click(screen.getByRole('button', { name: 'switch' }))

      expect(screen.getByTestId('where')).toHaveTextContent(/^\/p\/rodina$/)
    })

    it('goes home from an instance page, which no project owns', async () => {
      const user = userEvent.setup()
      renderSwitch('/account')
      expect(screen.getByTestId('in-address')).toHaveTextContent('false')

      await user.click(screen.getByRole('button', { name: 'switch' }))

      expect(screen.getByTestId('where')).toHaveTextContent(/^\/p\/rodina$/)
    })
  })
})

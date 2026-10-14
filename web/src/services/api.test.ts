import { beforeEach, describe, expect, it } from 'vitest'

import {
  apiPath,
  getCurrentProjectSlug,
  instancePath,
  projectApiPath,
  setCurrentProjectSlug,
} from './api'

beforeEach(() => {
  setCurrentProjectSlug(null)
})

describe('instancePath', () => {
  it('puts an instance route directly under /api/v1', () => {
    expect(instancePath('/auth/me')).toBe('/api/v1/auth/me')
  })

  it('needs no current project', () => {
    expect(getCurrentProjectSlug()).toBeNull()
    expect(instancePath('/settings/public')).toBe('/api/v1/settings/public')
  })
})

describe('apiPath', () => {
  it('prefixes a project route with the current project', () => {
    setCurrentProjectSlug('v')
    expect(apiPath('/photos')).toBe('/api/v1/p/v/photos')
    expect(apiPath('/photos/ph1/thumb/tile_500?v=2')).toBe(
      '/api/v1/p/v/photos/ph1/thumb/tile_500?v=2',
    )
  })

  it('follows the project as it changes', () => {
    setCurrentProjectSlug('v')
    expect(apiPath('/albums')).toBe('/api/v1/p/v/albums')
    setCurrentProjectSlug('rodina')
    expect(apiPath('/albums')).toBe('/api/v1/p/rodina/albums')
  })

  it('throws, naming the route, when no project is current', () => {
    // A signed-out page calling a project route is a bug, never a state to
    // render: the error says so instead of fetching a path that cannot exist.
    expect(() => apiPath('/photos')).toThrow(/no current project.*\/photos/)
  })

  it('throws again once the project is cleared', () => {
    setCurrentProjectSlug('v')
    setCurrentProjectSlug(null)
    expect(() => apiPath('/photos')).toThrow(Error)
  })
})

describe('projectApiPath', () => {
  it('prefixes a route with the named project, whatever is current', () => {
    setCurrentProjectSlug('v')
    expect(projectApiPath('rodina', '/maintenance/nameless-subjects')).toBe(
      '/api/v1/p/rodina/maintenance/nameless-subjects',
    )
  })

  it('needs no current project and escapes the slug', () => {
    expect(projectApiPath('a/b', '/photos')).toBe('/api/v1/p/a%2Fb/photos')
  })
})

describe('current project slug', () => {
  it('reads back what was set, including null', () => {
    setCurrentProjectSlug('v')
    expect(getCurrentProjectSlug()).toBe('v')
    setCurrentProjectSlug(null)
    expect(getCurrentProjectSlug()).toBeNull()
  })
})

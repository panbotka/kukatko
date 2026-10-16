import { describe, expect, it } from 'vitest'

import { isInstancePath, projectPathFor } from '../project/paths'

import {
  adminItems,
  PRIMARY_ITEMS,
  PROJECTS_ITEM,
  systemProjectPath,
  TOOLS_GROUP,
  toolsGroup,
} from './navItems'

/** The routes of a narrowed group, or null when there is none. */
function routes(roles: { canCurate: boolean; canWrite: boolean }): string[] | null {
  return toolsGroup(roles)?.items.map((entry) => entry.to) ?? null
}

describe('toolsGroup', () => {
  it('drops the whole group for a viewer, so no empty dropdown renders', () => {
    expect(toolsGroup({ canCurate: false, canWrite: false })).toBeNull()
  })

  it('gives a curator the face and collection tools, not duplicates or trash', () => {
    expect(routes({ canCurate: true, canWrite: false })).toEqual([
      '/expand',
      '/faces',
      '/recognition',
      '/outliers',
      '/duplicate-markers',
      '/upload-links',
    ])
  })

  it('gives an editor every tool, in menu order', () => {
    expect(routes({ canCurate: true, canWrite: true })).toEqual(
      TOOLS_GROUP.items.map((entry) => entry.to),
    )
    expect(routes({ canCurate: true, canWrite: true })).toContain('/trash')
  })

  it('keeps the group identity, so the toggle keeps its id and label', () => {
    const group = toolsGroup({ canCurate: true, canWrite: false })
    expect(group?.id).toBe(TOOLS_GROUP.id)
    expect(group?.labelKey).toBe(TOOLS_GROUP.labelKey)
  })
})

describe('PRIMARY_ITEMS', () => {
  it('keeps the /search page a labelled destination, since the phone tab opens the palette', () => {
    // The phone tab bar's search tab is the palette's door, so the drawer's
    // everyday block — built from these entries — is the only way to the page.
    const search = PRIMARY_ITEMS.find((entry) => entry.to === '/search')
    expect(search?.labelKey).toBe('nav.search')
    expect(search?.icon).toBe('search')
  })
})

describe('PROJECTS_ITEM', () => {
  it('leads to the chooser, an instance page no project prefix is put on', () => {
    // The account menu's "Switch library": the shell resolves every entry
    // through `useProjectPath`, which must leave this one alone in every project.
    expect(PROJECTS_ITEM.to).toBe('/projects')
    expect(isInstancePath(PROJECTS_ITEM.to)).toBe(true)
    expect(projectPathFor('v', PROJECTS_ITEM.to)).toBe('/projects')
    expect(PROJECTS_ITEM.labelKey).toBe('projectSwitcher.leave')
    expect(PROJECTS_ITEM.icon).toBe('arrow-left-right')
  })
})

describe('adminItems', () => {
  it('gives the system administrator the instance pages, which carry no project', () => {
    const routes = adminItems({ isAdmin: true, isMaintainer: true }).map((entry) => entry.to)
    expect(routes).toEqual([
      '/import',
      '/maintenance',
      '/system',
      '/system/projects',
      '/system/users',
      '/system/audit',
      '/members',
      '/audit',
      '/settings',
    ])
    for (const route of ['/system/projects', '/system/users', '/system/audit']) {
      expect(projectPathFor('v', route)).toBe(route)
    }
  })

  it('keeps the projects and accounts pages from a project admin', () => {
    const routes = adminItems({ isAdmin: true, isMaintainer: false }).map((entry) => entry.to)
    expect(routes).toEqual(['/members', '/audit', '/settings'])
  })

  it('lights /system only on its own page, not on its sub-pages', () => {
    const system = adminItems({ isAdmin: true, isMaintainer: true }).find(
      (entry) => entry.to === '/system',
    )
    expect(system?.exact).toBe(true)
  })
})

describe('systemProjectPath', () => {
  it('addresses a project detail by its slug', () => {
    expect(systemProjectPath('rodina')).toBe('/system/projects/rodina')
  })
})

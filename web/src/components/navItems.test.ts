import { describe, expect, it } from 'vitest'

import { isInstancePath, projectPathFor } from '../project/paths'

import {
  adminItems,
  desktopNav,
  desktopNavRoutes,
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

/** The roles of a maintainer, the widest ladder. */
const MAINTAINER = { canCurate: true, canWrite: true, isAdmin: true, isMaintainer: true }
/** The roles of a viewer, the narrowest. */
const VIEWER = { canCurate: false, canWrite: false, isAdmin: false, isMaintainer: false }

describe('desktopNav', () => {
  it('lays the main run out in the bar’s order, the tools fenced off by a divider', () => {
    const nav = desktopNav({ ...MAINTAINER, neutral: false })
    expect(
      nav.main.map((slot) =>
        slot.kind === 'link'
          ? `${slot.entry.to}${slot.cta === true ? ' (cta)' : ''}`
          : slot.kind === 'group'
            ? `[${slot.group.id}]`
            : '|',
      ),
    ).toEqual([
      '/',
      '/albums',
      '/labels',
      '/search',
      '[nav-browse]',
      '/review',
      '/upload (cta)',
      '|',
      '[nav-tools]',
    ])
  })

  it('gives a viewer no review game, no tools, no divider and no administration', () => {
    const nav = desktopNav({ ...VIEWER, neutral: false })
    expect(nav.main.some((slot) => slot.kind === 'divider')).toBe(false)
    expect(desktopNavRoutes(nav)).not.toContain('/review')
    expect(desktopNavRoutes(nav)).not.toContain('/trash')
    expect(nav.admin).toEqual([])
    expect(nav.account.map((entry) => entry.to)).toEqual(['/account', '/stats', '/help'])
  })

  it('offers "my photos" only to an account linked to a person', () => {
    const linked = desktopNav({ ...VIEWER, neutral: false, subjectUid: 'sub1' })
    expect(linked.account.map((entry) => entry.to)).toEqual([
      '/account',
      '/?person=sub1',
      '/stats',
      '/help',
    ])
  })

  it('keeps only the account and the instance’s own pages in the neutral shell', () => {
    const nav = desktopNav({ ...MAINTAINER, neutral: true, subjectUid: 'sub1' })
    expect(nav.main).toEqual([])
    expect(desktopNavRoutes(nav)).toEqual([
      '/account',
      '/maintenance',
      '/system',
      '/system/projects',
      '/system/users',
      '/system/audit',
    ])
  })

  it('lists every destination once, groups unfolded in place', () => {
    const routes = desktopNavRoutes(desktopNav({ ...MAINTAINER, neutral: false }))
    expect(new Set(routes).size).toBe(routes.length)
    expect(routes.slice(0, 6)).toEqual([
      '/',
      '/albums',
      '/labels',
      '/search',
      '/favorites',
      '/saved',
    ])
    expect(routes.at(-1)).toBe('/settings')
  })
})

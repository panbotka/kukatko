import { describe, expect, it } from 'vitest'

import { TOOLS_GROUP, toolsGroup } from './navItems'

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

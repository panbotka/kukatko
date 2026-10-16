import { describe, expect, it } from 'vitest'

import type { ProjectMembership } from '../services/auth'

import { chooserSections } from './chooserOrder'

/** An approved membership of an active project named `name`, overridable. */
function member(name: string, extra: Partial<ProjectMembership> = {}): ProjectMembership {
  return {
    uid: `p-${name}`,
    slug: name.toLowerCase(),
    name,
    active: true,
    role: 'viewer',
    approved: true,
    ...extra,
  }
}

/** The names of a list, in order. */
const names = (list: ProjectMembership[]) => list.map((m) => m.name)

describe('chooserSections', () => {
  it('puts the most recently visited first, then the never-visited by name', () => {
    const { active, inactive } = chooserSections(
      [
        member('Žabovřesky'),
        member('Archiv', { last_visit_at: '2026-10-01T10:00:00Z' }),
        member('Čtvrť'),
        member('Rodina', { last_visit_at: '2026-10-15T10:00:00Z' }),
        member('Bříza'),
      ],
      'cs',
    )
    expect(names(active)).toEqual(['Rodina', 'Archiv', 'Bříza', 'Čtvrť', 'Žabovřesky'])
    expect(inactive).toEqual([])
  })

  it('sets deactivated projects apart, by name, whatever their visit', () => {
    const input = [
      member('Veselice'),
      member('Staré', { active: false, last_visit_at: '2026-10-16T10:00:00Z' }),
      member('Archiv', { active: false }),
    ]
    const { active, inactive } = chooserSections(input, 'cs')
    expect(names(active)).toEqual(['Veselice'])
    expect(names(inactive)).toEqual(['Archiv', 'Staré'])
    // The context's list is not reordered in place.
    expect(names(input)).toEqual(['Veselice', 'Staré', 'Archiv'])
  })

  it('treats an unreadable visit as never visited', () => {
    const { active } = chooserSections(
      [member('Beta', { last_visit_at: 'nonsense' }), member('Alfa', { last_visit_at: null })],
      'en',
    )
    expect(names(active)).toEqual(['Alfa', 'Beta'])
  })
})

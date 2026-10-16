import type { ProjectMembership } from '../services/auth'

/** The chooser's memberships, split the way `/projects` draws them. */
export interface ChooserSections {
  /** Memberships of active projects: the most recently visited first, then by name. */
  active: ProjectMembership[]
  /** Memberships of deactivated projects (only a maintainer has any), by name. */
  inactive: ProjectMembership[]
}

/** The visit as milliseconds, or null for a project never visited. */
function visitedAt(membership: ProjectMembership): number | null {
  if (membership.last_visit_at === undefined || membership.last_visit_at === null) {
    return null
  }
  const at = Date.parse(membership.last_visit_at)
  return Number.isNaN(at) ? null : at
}

/** Orders two memberships by name the way a reader of `locale` expects, uid breaking ties. */
function byName(locale: string) {
  const collator = new Intl.Collator(locale, { sensitivity: 'base' })
  return (a: ProjectMembership, b: ProjectMembership) =>
    collator.compare(a.name, b.name) || (a.uid < b.uid ? -1 : a.uid > b.uid ? 1 : 0)
}

/**
 * Splits and orders the account's memberships for the project chooser: active
 * projects first — the one visited most recently at the head, every visited one
 * before the never-visited, which follow by name — and the deactivated ones
 * apart, by name. The input is left untouched.
 */
export function chooserSections(projects: ProjectMembership[], locale: string): ChooserSections {
  const named = byName(locale)
  const active = projects.filter((m) => m.active)
  const inactive = projects.filter((m) => !m.active).sort(named)
  active.sort((a, b) => {
    const va = visitedAt(a)
    const vb = visitedAt(b)
    if (va !== null && vb !== null && va !== vb) {
      return vb - va
    }
    if (va !== null && vb === null) {
      return -1
    }
    if (va === null && vb !== null) {
      return 1
    }
    return named(a, b)
  })
  return { active, inactive }
}

import { describe, expect, it } from 'vitest'

import csCommon from './locales/cs/common.json'
import enCommon from './locales/en/common.json'

/** A nested i18next resource tree: strings, or further nested objects. */
interface ResourceTree {
  [key: string]: string | ResourceTree
}

/** Flattens a resource tree into dot-separated leaf keys and their strings. */
function flatten(tree: ResourceTree, prefix = '', out: Record<string, string> = {}) {
  for (const [key, value] of Object.entries(tree)) {
    const path = prefix ? `${prefix}.${key}` : key
    if (typeof value === 'string') {
      out[path] = value
    } else {
      flatten(value, path, out)
    }
  }
  return out
}

/**
 * Words naming the instance's system administrator (or the instance role it
 * is), in either language and any Czech declension: „správce systému",
 * „správcům systému", „systémová role", "system administrator", "maintainer".
 */
const NAMES_THE_ROLE =
  /správc\p{L}*\s+systému|systémov\p{L}*\s+rol|system administrator|maintainer|system role|instance role/iu

/**
 * The keys allowed to name the role, each rendered **only** for a system
 * administrator — a project member must not even learn that the role exists.
 * A key is listed exactly or as a `prefix.` covering a whole namespace.
 */
const MAINTAINER_ONLY: Record<string, string> = {
  'systemUsers.': '/system/users, RequireRole maintainer',
  'systemProject.': '/system/projects/:slug, RequireRole maintainer',
  'systemProjects.firstAdmin.toggleHelp': "/system/projects' create dialog, RequireRole maintainer",
  'nav.titles.systemUsers': 'a nav entry filtered by isMaintainer',
  'maintenance.maintainerOnly': '/maintenance, RequireRole maintainer',
  'system.maintainerOnly': '/system, RequireRole maintainer',
  'audit.maintainerOnly': 'the instance audit (/system/audit) only',
  'audit.instanceSubtitle': 'the instance audit (/system/audit) only',
  'import.maintainerOnly': '/p/:slug/import, behind RequireImport (= isMaintainer)',
  'projectDeactivated.banner': 'a deactivated project, which only a maintainer can enter',
  'roles.maintainer': 'AccountPage and HelpPage render it for a maintainer only',
  'help.sections.import.body': 'HelpPage renders the import chapter for a maintainer only',
  'activity.actions.user.instance_role': 'an instance action only a maintainer can perform',
}

/** The allow-list entry covering key, or undefined. */
function allowanceOf(key: string): string | undefined {
  return Object.keys(MAINTAINER_ONLY).find((entry) =>
    entry.endsWith('.') ? key.startsWith(entry) : key === entry,
  )
}

const CATALOGUES = {
  cs: flatten(csCommon),
  en: flatten(enCommon),
}

describe('the system administrator role is invisible to project members', () => {
  it.each(Object.entries(CATALOGUES))(
    'no %s string a non-maintainer can reach names the role',
    (_language, catalogue) => {
      const leaks = Object.entries(catalogue)
        .filter(([key, value]) => NAMES_THE_ROLE.test(value) && allowanceOf(key) === undefined)
        .map(([key, value]) => `${key}: ${value}`)
      expect(leaks).toEqual([])
    },
  )

  it('keeps no stale allow-list entry', () => {
    const keys = [...Object.keys(CATALOGUES.cs), ...Object.keys(CATALOGUES.en)]
    for (const entry of Object.keys(MAINTAINER_ONLY)) {
      const covered = keys.some((key) =>
        entry.endsWith('.') ? key.startsWith(entry) : key === entry,
      )
      expect(covered, `${entry} matches no key`).toBe(true)
    }
  })
})

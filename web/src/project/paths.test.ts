import { describe, expect, it } from 'vitest'

import type { ProjectMembership } from '../services/auth'

import {
  legacyEntityOf,
  projectRegisterPath,
  entryProjectSlug,
  isInstancePath,
  KEEP_ON_SWITCH,
  projectPathFor,
  projectRoleOf,
  projectSlugInPath,
  routeAfterSwitch,
  stripProjectPrefix,
} from './paths'

/** An approved membership of an active project `slug`, overridable. */
function member(slug: string, extra: Partial<ProjectMembership> = {}): ProjectMembership {
  return {
    uid: `p-${slug}`,
    slug,
    name: slug,
    active: true,
    role: 'viewer',
    approved: true,
    ...extra,
  }
}

describe('entryProjectSlug', () => {
  it('picks the project the account last worked in when it may still enter it', () => {
    expect(
      entryProjectSlug({ projects: [member('v'), member('rodina')], last_project_slug: 'rodina' }),
    ).toBe('rodina')
  })

  it('picks the only enterable membership when no usable last project is remembered', () => {
    expect(entryProjectSlug({ projects: [member('v')], last_project_slug: '' })).toBe('v')
    // A remembered project that was deactivated since does not count…
    expect(
      entryProjectSlug({
        projects: [member('v'), member('old', { active: false })],
        last_project_slug: 'old',
      }),
    ).toBe('v')
    // …nor does a membership still awaiting approval.
    expect(
      entryProjectSlug({
        projects: [member('v'), member('new', { approved: false })],
        last_project_slug: 'new',
      }),
    ).toBe('v')
  })

  it('has no pick — the chooser — for none, or for several and no last one', () => {
    expect(entryProjectSlug({ projects: [], last_project_slug: '' })).toBeNull()
    expect(entryProjectSlug(null)).toBeNull()
    expect(
      entryProjectSlug({ projects: [member('v'), member('rodina')], last_project_slug: '' }),
    ).toBeNull()
    expect(
      entryProjectSlug({ projects: [member('v', { approved: false })], last_project_slug: 'v' }),
    ).toBeNull()
  })
})

describe('projectPathFor', () => {
  it('puts a page path into the project', () => {
    expect(projectPathFor('v', '/photos/abc')).toBe('/p/v/photos/abc')
    expect(projectPathFor('v', '/albums?sort=name#top')).toBe('/p/v/albums?sort=name#top')
  })

  it('gives the library its bare project address', () => {
    expect(projectPathFor('v', '/')).toBe('/p/v')
    expect(projectPathFor('v', '/?person=s1')).toBe('/p/v?person=s1')
    expect(projectPathFor('v', '/#x')).toBe('/p/v#x')
  })

  it('leaves instance pages, project addresses and foreign URLs alone', () => {
    expect(projectPathFor('v', '/account')).toBe('/account')
    expect(projectPathFor('v', '/account/activity?x=1')).toBe('/account/activity?x=1')
    expect(projectPathFor('v', '/system')).toBe('/system')
    expect(projectPathFor('v', '/login')).toBe('/login')
    expect(projectPathFor('v', '/u/abc')).toBe('/u/abc')
    expect(projectPathFor('v', '/p/rodina/albums')).toBe('/p/rodina/albums')
    expect(projectPathFor('v', 'https://example.org/photos/x')).toBe('https://example.org/photos/x')
    expect(projectPathFor('v', '//example.org/x')).toBe('//example.org/x')
  })

  it('does not take a page that merely starts like an instance one for it', () => {
    expect(projectPathFor('v', '/upload')).toBe('/p/v/upload')
    expect(projectPathFor('v', '/upload-links')).toBe('/p/v/upload-links')
  })

  it('sends a project page to the chooser when there is no project', () => {
    expect(projectPathFor(null, '/albums')).toBe('/projects')
    expect(projectPathFor(null, '/account')).toBe('/account')
  })

  it('encodes the slug', () => {
    expect(projectPathFor('a b', '/albums')).toBe('/p/a%20b/albums')
  })
})

describe('project prefix parsing', () => {
  it('reads the slug out of a project address', () => {
    expect(projectSlugInPath('/p/v/photos/abc')).toBe('v')
    expect(projectSlugInPath('/p/v')).toBe('v')
    expect(projectSlugInPath('/p/a%20b/x')).toBe('a b')
    expect(projectSlugInPath('/photos/abc')).toBeNull()
    expect(projectSlugInPath('/projects')).toBeNull()
  })

  it('strips the prefix down to the page path', () => {
    expect(stripProjectPrefix('/p/v/albums/x')).toBe('/albums/x')
    expect(stripProjectPrefix('/p/v')).toBe('/')
    expect(stripProjectPrefix('/p/v/')).toBe('/')
    expect(stripProjectPrefix('/account')).toBe('/account')
  })

  it('tells instance pages apart', () => {
    expect(isInstancePath('/maintenance')).toBe(true)
    expect(isInstancePath('/password-reset/tok')).toBe(true)
    expect(isInstancePath('/projects')).toBe(true)
    expect(isInstancePath('/albums')).toBe(false)
    expect(isInstancePath('/users')).toBe(false)
  })
})

describe('projectRoleOf', () => {
  it('is the membership role, or every power for the maintainer', () => {
    expect(projectRoleOf('user', member('v', { role: 'curator' }))).toBe('curator')
    expect(projectRoleOf('maintainer', member('v', { role: 'viewer' }))).toBe('maintainer')
    expect(projectRoleOf('maintainer', null)).toBe('maintainer')
    expect(projectRoleOf('user', null)).toBeNull()
  })
})

describe('routeAfterSwitch', () => {
  it('keeps a list page, which every project has', () => {
    expect(routeAfterSwitch('/p/v/albums', 'v', 'rodina')).toBe('/p/rodina/albums')
    expect(routeAfterSwitch('/p/v/people', 'v', 'rodina')).toBe('/p/rodina/people')
    expect(routeAfterSwitch('/p/v/notifications', 'v', 'rodina')).toBe('/p/rodina/notifications')
    for (const page of KEEP_ON_SWITCH) {
      const from = page === '' ? '/p/v' : `/p/v/${page}`
      const to = page === '' ? '/p/rodina' : `/p/rodina/${page}`
      expect(routeAfterSwitch(from, 'v', 'rodina')).toBe(to)
    }
  })

  it('keeps the library, the project home, as the home', () => {
    expect(routeAfterSwitch('/p/v', 'v', 'rodina')).toBe('/p/rodina')
    expect(routeAfterSwitch('/p/v/', 'v', 'rodina')).toBe('/p/rodina')
  })

  it('tolerates a trailing slash on a list page', () => {
    expect(routeAfterSwitch('/p/v/search/', 'v', 'rodina')).toBe('/p/rodina/search')
  })

  it('drops the query and the hash, which may name uids of the old library', () => {
    expect(routeAfterSwitch('/p/v/search?q=person:abc#x', 'v', 'rodina')).toBe('/p/rodina/search')
    expect(routeAfterSwitch('/p/v?album=abc', 'v', 'rodina')).toBe('/p/rodina')
  })

  it('goes home from any page that carries a uid', () => {
    for (const page of [
      '/photos/abc',
      '/albums/abc',
      '/albums/abc/faces',
      '/labels/abc',
      '/people/abc',
      '/people/abc/tree',
      '/tasks/abc',
      '/n/abc',
      '/people/clusters',
    ]) {
      expect(routeAfterSwitch(`/p/v${page}`, 'v', 'rodina')).toBe('/p/rodina')
    }
  })

  it('goes home from a page that is not a list (tools, admin, the slideshow)', () => {
    for (const page of ['/review', '/duplicates', '/users', '/settings', '/slideshow', '/nope']) {
      expect(routeAfterSwitch(`/p/v${page}`, 'v', 'rodina')).toBe('/p/rodina')
    }
  })

  it('goes home from an instance page or an address outside the source project', () => {
    expect(routeAfterSwitch('/account', 'v', 'rodina')).toBe('/p/rodina')
    expect(routeAfterSwitch('/account/activity', 'v', 'rodina')).toBe('/p/rodina')
    expect(routeAfterSwitch('/projects', 'v', 'rodina')).toBe('/p/rodina')
    expect(routeAfterSwitch('/p/other/albums', 'v', 'rodina')).toBe('/p/rodina')
  })

  it('encodes the destination slug like every other project link', () => {
    expect(routeAfterSwitch('/p/v/albums', 'v', 'a b')).toBe('/p/a%20b/albums')
  })
})

describe('projectRegisterPath', () => {
  it("is the project's public registration page, slug encoded", () => {
    expect(projectRegisterPath('rodina')).toBe('/p/rodina/register')
    expect(projectRegisterPath('a b')).toBe('/p/a%20b/register')
  })
})

describe('legacyEntityOf', () => {
  it('names the entity of every page about one', () => {
    expect(legacyEntityOf('/photos/ph1')).toEqual({ kind: 'photo', uid: 'ph1' })
    expect(legacyEntityOf('/albums/al1')).toEqual({ kind: 'album', uid: 'al1' })
    expect(legacyEntityOf('/albums/al1/faces')).toEqual({ kind: 'album', uid: 'al1' })
    expect(legacyEntityOf('/labels/lb1')).toEqual({ kind: 'label', uid: 'lb1' })
    expect(legacyEntityOf('/tasks/tk1')).toEqual({ kind: 'task', uid: 'tk1' })
    expect(legacyEntityOf('/people/su1/tree')).toEqual({ kind: 'subject', uid: 'su1' })
    expect(legacyEntityOf('/n/nt1')).toEqual({ kind: 'notification', uid: 'nt1' })
  })

  it('decodes the uid', () => {
    expect(legacyEntityOf('/photos/a%20b')).toEqual({ kind: 'photo', uid: 'a b' })
  })

  it('is null for a page about no single entity', () => {
    for (const path of ['/', '/albums', '/people', '/people/clusters', '/map', '/search']) {
      expect(legacyEntityOf(path)).toBeNull()
    }
  })
})

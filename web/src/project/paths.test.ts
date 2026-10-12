import { describe, expect, it } from 'vitest'

import type { ProjectMembership } from '../services/auth'

import {
  entryProjectSlug,
  isInstancePath,
  projectPathFor,
  projectRoleOf,
  projectSlugInPath,
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

import { beforeEach, describe, expect, it, vi } from 'vitest'

import { readLastProject, writeLastProject } from './lastProject'

beforeEach(() => {
  window.localStorage.clear()
})

describe('last project storage', () => {
  it('remembers nothing until a project was shown', () => {
    expect(readLastProject('u1')).toBeNull()
  })

  it('reads back the slug an account was last in, under its own uid', () => {
    writeLastProject('u1', 'v')

    expect(readLastProject('u1')).toBe('v')
    expect(window.localStorage.getItem('kukatko.lastProject.u1')).toBe('v')

    writeLastProject('u1', 'rodina')
    expect(readLastProject('u1')).toBe('rodina')
  })

  it('keeps two accounts on one browser apart', () => {
    writeLastProject('u1', 'v')
    writeLastProject('u2', 'rodina')

    expect(readLastProject('u1')).toBe('v')
    expect(readLastProject('u2')).toBe('rodina')
    expect(readLastProject('u3')).toBeNull()
  })

  it('neither reads nor writes without an account or a slug', () => {
    writeLastProject(undefined, 'v')
    writeLastProject('u1', '')

    expect(window.localStorage.length).toBe(0)
    expect(readLastProject(undefined)).toBeNull()
  })

  it('remembers nothing, and does not throw, when storage refuses to work', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied')
    })

    expect(() => {
      writeLastProject('u1', 'v')
    }).not.toThrow()
    expect(readLastProject('u1')).toBeNull()
  })
})

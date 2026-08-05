import { describe, expect, it } from 'vitest'

import { commitUrl, formatVersion } from './version'

describe('formatVersion', () => {
  it('prefixes a semantic version with v', () => {
    expect(formatVersion({ version: '0.5.1', commit: 'abc1234' })).toBe('v0.5.1')
  })

  it('shows a development build verbatim', () => {
    expect(formatVersion({ version: 'dev', commit: 'none' })).toBe('dev')
  })

  it('trims surrounding whitespace', () => {
    expect(formatVersion({ version: ' 1.0.0 ', commit: 'abc1234' })).toBe('v1.0.0')
  })

  it('returns null when there is no build metadata', () => {
    expect(formatVersion(undefined)).toBeNull()
  })

  it('returns null for an empty version', () => {
    expect(formatVersion({ version: '   ', commit: 'abc1234' })).toBeNull()
  })
})

describe('commitUrl', () => {
  it('links a short commit hash to the public repository', () => {
    expect(commitUrl({ version: '0.5.1', commit: '77fba72' })).toBe(
      'https://github.com/panbotka/kukatko/commit/77fba72',
    )
  })

  it('links a full 40-character hash', () => {
    const sha = 'a'.repeat(40)
    expect(commitUrl({ version: '0.5.1', commit: sha })).toBe(
      `https://github.com/panbotka/kukatko/commit/${sha}`,
    )
  })

  it('returns null for a development build', () => {
    expect(commitUrl({ version: 'dev', commit: 'none' })).toBeNull()
  })

  it('returns null when there is no build metadata', () => {
    expect(commitUrl(undefined)).toBeNull()
  })

  it('returns null for anything that is not a commit hash', () => {
    expect(commitUrl({ version: '0.5.1', commit: '' })).toBeNull()
    expect(commitUrl({ version: '0.5.1', commit: 'abc' })).toBeNull()
    expect(commitUrl({ version: '0.5.1', commit: 'not-a-sha-at-all' })).toBeNull()
  })
})

import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

import csCommon from '../../i18n/locales/cs/common.json'
import enCommon from '../../i18n/locales/en/common.json'
import { JOB_TYPE_LABELS } from './jobTypeLabels'

/**
 * The job types the Go queue declares, read straight from
 * `internal/jobs/models.go` (`TypeX = "x"`), so this list cannot drift from the
 * backend. Vitest runs with `web/` as its cwd; the repo root is tried too.
 */
function goJobTypes(): string[] {
  const path = ['../internal/jobs/models.go', 'internal/jobs/models.go']
    .map((rel) => resolve(process.cwd(), rel))
    .find((candidate) => existsSync(candidate))
  if (path === undefined) {
    throw new Error(`internal/jobs/models.go not found from cwd ${process.cwd()}`)
  }
  const source = readFileSync(path, 'utf8')
  return [...source.matchAll(/^\s*Type[A-Za-z0-9]+\s*=\s*"([a-z_]+)"/gm)].map((m) => m[1])
}

/** Reads a dotted key out of a locale tree, or undefined when any segment is missing. */
function lookup(tree: unknown, dotted: string): unknown {
  return dotted.split('.').reduce<unknown>((node, part) => {
    if (node === null || typeof node !== 'object') {
      return undefined
    }
    return (node as Record<string, unknown>)[part]
  }, tree)
}

describe('JOB_TYPE_LABELS', () => {
  const types = goJobTypes()

  it('finds the Go job types', () => {
    expect(types).toContain('image_embed')
    expect(types).toContain('storage_rekey')
  })

  it.each(types)('names %s in both locales', (type) => {
    const key = JOB_TYPE_LABELS[type]
    expect(key, `${type} has no label key`).toBeDefined()
    for (const [locale, tree] of [
      ['cs', csCommon],
      ['en', enCommon],
    ] as const) {
      const text = lookup(tree, String(key))
      expect(typeof text === 'string' && text.trim() !== '', `${locale} ${String(key)}`).toBe(true)
    }
  })

  it('names no type the backend does not have', () => {
    expect(Object.keys(JOB_TYPE_LABELS).filter((type) => !types.includes(type))).toEqual([])
  })
})

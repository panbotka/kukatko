// @vitest-environment node
//
// Build tooling, not app code: this file drives a real `vite build`, and esbuild
// refuses to run under jsdom (its TextEncoder yields a foreign Uint8Array).
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { afterEach, describe, expect, it } from 'vitest'
import { build } from 'vite'

import { gitkeepPlugin, PLACEHOLDER_FILE, placeholderPath, restorePlaceholder } from './gitkeep'

/** Scratch directories created by a test, removed again afterwards. */
const scratch: string[] = []

/** Creates an empty scratch directory that is cleaned up after the test. */
function makeScratch(): string {
  const dir = mkdtempSync(join(tmpdir(), 'kukatko-gitkeep-'))
  scratch.push(dir)
  return dir
}

afterEach(() => {
  for (const dir of scratch.splice(0)) {
    rmSync(dir, { recursive: true, force: true })
  }
})

describe('placeholderPath', () => {
  it('resolves a relative outDir against the project root', () => {
    // The value vite.config.ts actually carries.
    expect(placeholderPath('/repo/web', '../internal/web/static/dist')).toBe(
      `/repo/internal/web/static/dist/${PLACEHOLDER_FILE}`,
    )
  })

  it('keeps an absolute outDir, which is what the resolved config holds', () => {
    expect(placeholderPath('/repo/web', '/repo/internal/web/static/dist')).toBe(
      `/repo/internal/web/static/dist/${PLACEHOLDER_FILE}`,
    )
  })
})

describe('restorePlaceholder', () => {
  it('writes an empty placeholder into an emptied output directory', () => {
    const root = makeScratch()

    restorePlaceholder(root, '.')

    expect(readFileSync(join(root, PLACEHOLDER_FILE), 'utf8')).toBe('')
  })

  it('is idempotent, so repeated builds leave the same tracked empty file', () => {
    const root = makeScratch()
    writeFileSync(join(root, PLACEHOLDER_FILE), '')

    restorePlaceholder(root, '.')
    restorePlaceholder(root, '.')

    expect(readFileSync(join(root, PLACEHOLDER_FILE), 'utf8')).toBe('')
  })
})

describe('gitkeepPlugin', () => {
  it('only applies to a build, which is the thing that empties outDir', () => {
    const plugin = gitkeepPlugin()

    expect(plugin.name).toBe('kukatko-gitkeep')
    expect(plugin.apply).toBe('build')
  })

  // The regression this whole file exists for: a real Vite build empties its
  // output directory and would otherwise leave the tracked placeholder deleted,
  // which is what stamped every release binary +dirty.
  it('restores the placeholder a real build deleted', async () => {
    const root = makeScratch()
    const outDir = makeScratch()
    writeFileSync(join(root, 'index.html'), '<!doctype html><title>t</title>\n')
    // The state of a checkout before a build: the tracked placeholder, plus a
    // leftover the emptying is supposed to remove.
    writeFileSync(join(outDir, PLACEHOLDER_FILE), '')
    writeFileSync(join(outDir, 'stale.js'), 'export {}\n')

    await build({
      root,
      logLevel: 'silent',
      build: { outDir, emptyOutDir: true },
      plugins: [gitkeepPlugin()],
    })

    expect(existsSync(join(outDir, 'stale.js'))).toBe(false)
    expect(existsSync(join(outDir, 'index.html'))).toBe(true)
    expect(readFileSync(join(outDir, PLACEHOLDER_FILE), 'utf8')).toBe('')
    // Into the output directory, not the project root.
    expect(existsSync(join(root, PLACEHOLDER_FILE))).toBe(false)
  })
})

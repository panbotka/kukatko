import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative, resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

/**
 * A dialog's footer has one filled button: the action. Its cancel/close is
 * quiet — `outline-secondary` (or `link`) — so the eye lands on "Uložit" or
 * "Smazat" first. A filled `secondary` cancel is Superhero's teal plate, the
 * same weight as the orange primary beside it, and the two read as a choice
 * between equals (UI/UX review 2026-10-11, finding #33). This walks every
 * component under `src/`, cuts out each `<Modal.Footer>` and fails on a filled
 * neutral variant inside it.
 */

/** Filled variants with no meaning of their own — never right for a footer's second button. */
const FILLED_NEUTRAL = new Set(['secondary', 'info', 'light', 'dark'])

function sourceRoot(): string {
  const here = resolve(process.cwd(), 'src')
  return statSync(here, { throwIfNoEntry: false })?.isDirectory() === true
    ? here
    : resolve(process.cwd(), 'web/src')
}

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const path = join(dir, entry)
    return statSync(path).isDirectory() ? walk(path) : [path]
  })
}

/** Every `<Modal.Footer …>…</Modal.Footer>` in one source file. */
function footers(source: string): string[] {
  return [...source.matchAll(/<Modal\.Footer\b[\s\S]*?<\/Modal\.Footer>/g)].map((m) => m[0])
}

/** The literal `variant="…"` values in a fragment (expressions are left to review). */
function variants(fragment: string): string[] {
  return [...fragment.matchAll(/\bvariant=["']([\w-]+)["']/g)].map((m) => m[1])
}

const root = sourceRoot()
const components = walk(root).filter((path) => path.endsWith('.tsx') && !path.endsWith('.test.tsx'))

describe('modal footers', () => {
  it('are found, so the guard below is not vacuous', () => {
    const count = components.reduce(
      (sum, path) => sum + footers(readFileSync(path, 'utf8')).length,
      0,
    )
    expect(count).toBeGreaterThan(20)
  })

  it('keep the action the only filled button: cancel/close is outlined', () => {
    const offenders = components.flatMap((path) =>
      footers(readFileSync(path, 'utf8')).flatMap((footer) =>
        variants(footer)
          .filter((variant) => FILLED_NEUTRAL.has(variant))
          .map((variant) => `${relative(root, path)}: variant="${variant}"`),
      ),
    )
    expect(offenders).toEqual([])
  })
})

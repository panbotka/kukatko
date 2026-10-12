import { readdirSync, statSync } from 'node:fs'
import { join, relative, resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

import { readCss } from '../test/css'

/**
 * Every drawer names its ✕ in the interface language.
 *
 * react-bootstrap's own accessible name for a header close button is the
 * English literal `Close`. Modals get the translated name from the app's
 * `Modal` wrapper (and ESLint keeps every modal on it), but an `Offcanvas` has
 * no such wrapper — each of the few drawers says what its ✕ closes ("Zavřít
 * filtry", "Zavřít menu") — so this walks the components and fails on an
 * `Offcanvas.Header` with a `closeButton` and no `closeLabel`.
 */

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

const root = sourceRoot()
const files = walk(root)
  .map((path) => `src/${relative(root, path).split('\\').join('/')}`)
  .filter((path) => path.endsWith('.tsx') && !path.endsWith('.test.tsx'))

/** The opening tags of every `Offcanvas.Header` in a source file. */
function offcanvasHeaders(source: string): string[] {
  return source.match(/<Offcanvas\.Header\b[^>]*>/g) ?? []
}

describe('Offcanvas header close buttons', () => {
  const withHeaders = files.filter((path) => offcanvasHeaders(readCss(path)).length > 0)

  it('finds the drawers it is meant to guard', () => {
    expect(withHeaders).toContain('src/components/MobileNavDrawer.tsx')
  })

  it.each(withHeaders)('%s labels every drawer close button', (path) => {
    for (const tag of offcanvasHeaders(readCss(path))) {
      if (/\bcloseButton\b/.test(tag)) {
        expect(tag).toMatch(/\bcloseLabel=/)
      }
    }
  })
})

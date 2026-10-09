import { readdirSync, statSync } from 'node:fs'
import { join, relative, resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

import { stripCssComments } from '../test/colour'
import { readCss } from '../test/css'

/**
 * One palette, everything derived — enforced.
 *
 * The palette can only be swapped by editing `palette.css` if no other file
 * spells out a colour of its own: a stray `#fff` or `rgb(0 0 0 / 50%)` is a
 * place the next palette silently does not reach (and the very kind of place
 * where light text ends up on a light surface). So this walks every stylesheet
 * and every component under `src/` and fails on a colour literal anywhere but
 * the palette file. Derive instead — `var(--kk-…)`, or `color-mix(in srgb,
 * var(--kk-…) N%, transparent)` for a translucent step.
 *
 * Comments are exempt (prose may name a colour it is explaining), and so are
 * tests, which measure colours rather than paint them.
 */

/** Files allowed to name a colour: the palette, and the webfont declarations (no colours, but no reason to scan). */
const ALLOWED = new Set(['src/styles/palette.css', 'src/styles/fonts.css'])

/** Hex colours, the colour functions, named black/white, and hex inside a data URI (`%23rrggbb`). */
const LITERAL =
  /(?<![\w-])#[0-9a-fA-F]{3,8}\b|%23[0-9a-fA-F]{3,8}\b|\b(?:rgba?|hsla?|hwb|lab|lch|oklab|oklch)\(|(?<![\w-])(?:white|black)(?![\w-])/g

/** A bare `r, g, b` triplet assigned to a custom property (Bootstrap's `-rgb` convention). */
const TRIPLET = /--[\w-]+:\s*\d{1,3}\s*,\s*\d{1,3}\s*,\s*\d{1,3}\s*[;}]/g

function sourceRoot(): string {
  const css = readCss('src/styles/palette.css')
  expect(css.length).toBeGreaterThan(0)
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

/**
 * TypeScript comments out, string contents kept (a colour hides in a style
 * string). A close button's `variant="white"` names Bootstrap's `.btn-close-white`
 * filter, not a colour, so it goes too.
 */
function stripTsComments(source: string): string {
  return source
    .replace(/\/\*[\s\S]*?\*\//g, (comment) => comment.replace(/[^\n]/g, ''))
    .replace(/(?:closeVariant|variant)=["']white["']/g, '')
    .split('\n')
    .map((line) => line.replace(/(^|[^:'"`])\/\/.*$/, '$1'))
    .join('\n')
}

const root = sourceRoot()
const files = walk(root)
  .map((path) => `src/${relative(root, path).split('\\').join('/')}`)
  .filter((path) => /\.(css|tsx?)$/.test(path))
  .filter((path) => !/\.test\.tsx?$/.test(path) && !path.startsWith('src/test/'))
  .filter((path) => !ALLOWED.has(path))

describe('colour literals outside the palette', () => {
  it('finds the stylesheets and components it is meant to guard', () => {
    expect(files).toContain('src/styles/tokens.css')
    expect(files).toContain('src/styles/app.css')
    expect(files).toContain('src/styles/bootstrapBridge.css')
    expect(files.some((path) => path.endsWith('.tsx'))).toBe(true)
  })

  it.each(files)('%s names no colour of its own', (path) => {
    const source = readCss(path)
    const code = path.endsWith('.css') ? stripCssComments(source) : stripTsComments(source)
    const found = [...code.matchAll(LITERAL), ...code.matchAll(TRIPLET)].map((m) => {
      const line = code.slice(0, m.index).split('\n').length
      return `${path}:${line}: ${m[0]}`
    })
    expect(found).toEqual([])
  })

  it('catches what it claims to catch', () => {
    const probes = [
      'color: #fff;',
      'background: rgb(0 0 0 / 50%);',
      'border-color: white;',
      'url("data:image/svg+xml,%3csvg stroke=\'%23eceae5\'")',
      '--bs-btn-focus-shadow-rgb: 95, 176, 242;',
    ]
    for (const probe of probes) {
      expect(
        [...probe.matchAll(LITERAL), ...probe.matchAll(TRIPLET)].length,
        probe,
      ).toBeGreaterThan(0)
    }
    for (const clean of ['white-space: nowrap;', 'color: var(--kk-text);', 'cursor: pointer;']) {
      expect([...clean.matchAll(LITERAL), ...clean.matchAll(TRIPLET)], clean).toEqual([])
    }
  })
})

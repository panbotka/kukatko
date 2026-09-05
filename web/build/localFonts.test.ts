// @vitest-environment node
//
// Build tooling, not app code: this file drives a real `vite build`, and esbuild
// refuses to run under jsdom (its TextEncoder yields a foreign Uint8Array).
import { mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { afterEach, describe, expect, it } from 'vitest'
import { build } from 'vite'

import { findRemoteFontHosts, localFontsPlugin, stripRemoteFontImports } from './localFonts'

/** The exact statement Bootswatch's compiled theme opens with, minified. */
const BOOTSWATCH_IMPORT =
  '@import"https://fonts.googleapis.com/css2?family=Lato:wght@300;400;700&display=swap";'

/** Scratch directories created by a test, removed again afterwards. */
const scratch: string[] = []

/** Creates an empty scratch directory that is cleaned up after the test. */
function makeScratch(): string {
  const dir = mkdtempSync(join(tmpdir(), 'kukatko-fonts-'))
  scratch.push(dir)
  return dir
}

afterEach(() => {
  for (const dir of scratch.splice(0)) {
    rmSync(dir, { recursive: true, force: true })
  }
})

describe('stripRemoteFontImports', () => {
  // The regression: the URL's own `;` separators used to cut the statement in
  // half and leave `400;700&display=swap";` behind as broken CSS.
  it('removes the whole Bootswatch import, semicolons in the query and all', () => {
    const css = `@charset "UTF-8";${BOOTSWATCH_IMPORT}body{color:red}`

    expect(stripRemoteFontImports(css)).toBe('@charset "UTF-8";body{color:red}')
  })

  it('removes the unminified `url()` spelling a dev build reads', () => {
    const css =
      '@import url(https://fonts.googleapis.com/css2?family=Lato:wght@300;400;700&display=swap);\nbody { color: red; }\n'

    expect(stripRemoteFontImports(css)).toBe('\nbody { color: red; }\n')
  })

  it('removes a quoted `url()` and a gstatic target as well', () => {
    expect(stripRemoteFontImports("@import url('https://fonts.gstatic.com/s/lato/x.css');")).toBe(
      '',
    )
  })

  it('removes an import carrying a media query, up to its semicolon', () => {
    expect(
      stripRemoteFontImports('@import "https://fonts.googleapis.com/css2?x" screen;a{b:c}'),
    ).toBe('a{b:c}')
  })

  it('leaves a stylesheet with no font call untouched', () => {
    const css = '@import "./tokens.css";\n@font-face { font-family: "Lato"; }\n'

    expect(stripRemoteFontImports(css)).toBe(css)
  })

  // The strip is narrow on purpose: a dependency legitimately importing a
  // stylesheet from elsewhere must not be silently dropped.
  it('leaves an import of some other remote stylesheet alone', () => {
    const css = '@import "https://example.com/theme.css";'

    expect(stripRemoteFontImports(css)).toBe(css)
  })
})

describe('findRemoteFontHosts', () => {
  it('reports each font host an asset still mentions', () => {
    expect(
      findRemoteFontHosts(`${BOOTSWATCH_IMPORT}url(https://fonts.gstatic.com/s/lato/a.woff2)`),
    ).toEqual(['fonts.googleapis.com', 'fonts.gstatic.com'])
  })

  it('reports nothing for a stylesheet that keeps its fonts local', () => {
    expect(findRemoteFontHosts("src:url('/assets/lato-latin-400.woff2')")).toEqual([])
  })
})

describe('localFontsPlugin', () => {
  it('also applies to the dev server, which must be as offline-capable as the binary', () => {
    const plugin = localFontsPlugin()

    expect(plugin.name).toBe('kukatko-local-fonts')
    expect(plugin.enforce).toBe('pre')
    expect(plugin.apply).toBeUndefined()
  })

  // What the spec asked to be checked: the *built* CSS, not the source. Vite
  // hoists an absolute `@import` to the top of the bundle instead of inlining
  // it, so only a real build proves the statement is gone.
  it('keeps a themed build free of the remote import', async () => {
    const root = makeScratch()
    const outDir = makeScratch()
    writeFileSync(join(root, 'theme.css'), `${BOOTSWATCH_IMPORT}body{font-family:Lato,sans-serif}`)
    writeFileSync(join(root, 'main.js'), "import './theme.css'\n")
    writeFileSync(
      join(root, 'index.html'),
      '<!doctype html><title>t</title><script type="module" src="/main.js"></script>\n',
    )

    await build({
      root,
      logLevel: 'silent',
      build: { outDir, emptyOutDir: true },
      plugins: [localFontsPlugin()],
    })

    const assets = join(outDir, 'assets')
    const css = readdirSync(assets)
      .filter((name) => name.endsWith('.css'))
      .map((name) => readFileSync(join(assets, name), 'utf8'))
      .join('')
    expect(css).not.toContain('fonts.googleapis.com')
    expect(css).not.toContain('fonts.gstatic.com')
    // The rest of the theme survived the strip.
    expect(css).toContain('font-family:Lato,sans-serif')
  })

  // Proves the guard is not inert: with the strip disabled, the very import
  // this fix removes is what the finished stylesheet still carries, and the
  // guard has to see it there.
  it('fails the build on the hoisted import Vite leaves in the finished CSS', async () => {
    const root = makeScratch()
    const outDir = makeScratch()
    writeFileSync(join(root, 'theme.css'), `${BOOTSWATCH_IMPORT}body{color:red}`)
    writeFileSync(join(root, 'main.js'), "import './theme.css'\n")
    writeFileSync(
      join(root, 'index.html'),
      '<!doctype html><title>t</title><script type="module" src="/main.js"></script>\n',
    )

    await expect(
      build({
        root,
        logLevel: 'silent',
        build: { outDir, emptyOutDir: true },
        plugins: [{ ...localFontsPlugin(), transform: undefined }],
      }),
    ).rejects.toThrow(/fonts\.googleapis\.com/)
  })

  // The guard is the part that outlives this fix: a future dependency adding
  // its own webfont call has to fail the build, not ship quietly.
  it('fails the build when an asset still names a font host', async () => {
    const root = makeScratch()
    const outDir = makeScratch()
    writeFileSync(join(root, 'index.html'), '<!doctype html><title>t</title>\n')

    await expect(
      build({
        root,
        logLevel: 'silent',
        build: { outDir, emptyOutDir: true },
        plugins: [
          localFontsPlugin(),
          {
            // Stands in for a dependency that re-introduces the call after the
            // strip has already run.
            name: 'test-smuggle-font-host',
            generateBundle(_options, bundle) {
              bundle['smuggled.css'] = {
                type: 'asset',
                fileName: 'smuggled.css',
                names: ['smuggled.css'],
                originalFileNames: [],
                name: 'smuggled.css',
                originalFileName: null,
                needsCodeReference: false,
                source: '@font-face{src:url(https://fonts.gstatic.com/s/lato/a.woff2)}',
              }
            },
          },
        ],
      }),
    ).rejects.toThrow(/fonts\.gstatic\.com/)
  })
})

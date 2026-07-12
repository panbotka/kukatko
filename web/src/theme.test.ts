import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

/**
 * Locates `index.html` relative to the working directory. Vitest runs with the
 * `web/` package as its cwd, but resolve from the repo root too so the guard holds
 * whoever launches it. `import.meta.url` is unusable here — the jsdom test
 * environment reports a non-`file:` document URL for it.
 */
function readIndexHtml(): string {
  const candidate = ['index.html', 'web/index.html']
    .map((rel) => resolve(process.cwd(), rel))
    .find((path) => existsSync(path))
  if (candidate === undefined) {
    throw new Error('index.html not found from cwd ' + process.cwd())
  }
  return readFileSync(candidate, 'utf8')
}

/**
 * Guards the app-wide dark-theme fix. Bootswatch Superhero leaves several surface
 * tokens (`--bs-tertiary-bg`, `--bs-secondary-bg`, `--bs-emphasis-color`, …) at
 * light values on `:root` and only flips them to dark inside `[data-bs-theme=dark]`.
 * With no `data-bs-theme` on the document root those surfaces paint near-white while
 * the body text is also near-white, so labels on `.bg-body-tertiary` panels (the
 * library advanced-filter panel) become invisible. Dropping the attribute would
 * silently bring that whole class of white-on-white bug back, so assert it — and the
 * aligned `color-scheme` — stay on the HTML entry document.
 */
describe('index.html theme attributes', () => {
  const html = readIndexHtml()
  const openingHtmlTag = /<html\b[^>]*>/i.exec(html)?.[0] ?? ''

  it('sets data-bs-theme="dark" on the root <html> element', () => {
    expect(openingHtmlTag).toMatch(/\bdata-bs-theme="dark"/)
  })

  it('declares a dark color-scheme so native controls render dark-appropriately', () => {
    expect(html).toMatch(/<meta[^>]*name="color-scheme"[^>]*content="dark"/i)
  })
})

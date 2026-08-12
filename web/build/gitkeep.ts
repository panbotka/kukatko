/**
 * Build-time restore of the tracked embed placeholder.
 *
 * `internal/web/static/dist/.gitkeep` is committed because `//go:embed
 * all:dist/*` (internal/web/static/embed.go) needs at least one file in that
 * directory for the package to compile — a fresh clone must build before anyone
 * has run a frontend build.
 *
 * `vite build` empties its output directory (`build.emptyOutDir`), so it deletes
 * that placeholder, and to Go's VCS stamping a deleted *tracked* file is a
 * modified working tree. That is why every released binary up to v0.8.0 reported
 * `github.com/panbotka/kukatko v0.8.0+dirty`: goreleaser validates the git state
 * *before* it runs its before-hooks, and the frontend build is one of those
 * hooks. A `+dirty` on every single release distinguishes nothing — it is meant
 * to say "this binary does not correspond exactly to that tag".
 *
 * Writing the placeholder back once the build has finished leaves the checkout
 * identical to the tag, so the suffix means again what it says (a genuinely
 * modified tree still gets it — nothing here disables VCS stamping). The restore
 * lives in the Vite build rather than in each caller so that every path that
 * builds the frontend — `make web-build`, `scripts/dev.sh`, the Dockerfile, the
 * goreleaser before-hook — inherits it from one place.
 */
import { writeFileSync } from 'node:fs'
import { join, resolve } from 'node:path'

import type { Plugin } from 'vite'

/** The tracked placeholder `vite build` deletes when it empties its outDir. */
export const PLACEHOLDER_FILE = '.gitkeep'

/**
 * Absolute path of the placeholder inside a build output directory. `outDir` is
 * resolved against the project root, which is a no-op for the resolved config
 * Vite hands the plugin (it stores an absolute path) but also gives the right
 * answer for the relative value written in vite.config.ts.
 */
export function placeholderPath(root: string, outDir: string): string {
  return join(resolve(root, outDir), PLACEHOLDER_FILE)
}

/**
 * Recreates the empty placeholder in a finished build output directory.
 * Exported so a test can exercise the write itself instead of trusting the
 * plugin wiring by eye.
 */
export function restorePlaceholder(root: string, outDir: string): void {
  writeFileSync(placeholderPath(root, outDir), '')
}

/**
 * The plugin that performs the restore. `apply: 'build'` because only a build
 * empties the output directory, and `closeBundle` because that hook runs after
 * Vite has emptied outDir and written the bundle to disk.
 */
export function gitkeepPlugin(): Plugin {
  let root = ''
  let outDir = ''
  return {
    name: 'kukatko-gitkeep',
    apply: 'build',
    configResolved(config) {
      root = config.root
      outDir = config.build.outDir
    },
    closeBundle() {
      restorePlaceholder(root, outDir)
    },
  }
}

/**
 * Build-time half of the PWA: the Vite plugin that turns build/service-worker.js
 * into the `/sw.js` the browser registers.
 *
 * The worker has to know the exact URLs of the shell it precaches, and those are
 * content-hashed by Vite and therefore only known once the bundle exists. So the
 * plugin runs at `generateBundle`, derives the manifest from the emitted files,
 * substitutes it (and a cache name derived from it) into the worker source, and
 * emits the result as an unhashed asset at the output root.
 *
 * Deriving the cache name from the manifest is what makes deployment safe: a
 * build whose assets did not change reuses the cache, and a build whose assets
 * did change gets a fresh one, which the worker's activate handler swaps to
 * after pruning the old.
 *
 * Deliberately not a dependency on workbox/vite-plugin-pwa: everything the app
 * needs is a precache list and a whitelist fetch handler, and keeping it in
 * repo means the caching rules are readable in one short file instead of
 * configured through a generator.
 */
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import type { Plugin } from 'vite'

/** Where the emitted worker lands in the build output (root scope). */
export const SERVICE_WORKER_FILE = 'sw.js'

/** The literal in service-worker.js standing in for the cache name. */
const CACHE_NAME_PLACEHOLDER = "'__KUKATKO_CACHE_NAME__'"

/** The literal in service-worker.js standing in for the precache manifest. */
const PRECACHE_PLACEHOLDER = "['__KUKATKO_PRECACHE__']"

/** Cache-name prefix; must match CACHE_PREFIX in service-worker.js. */
const CACHE_PREFIX = 'kukatko-shell-'

/**
 * File extensions that belong to the app shell. Everything Vite emits into the
 * bundle is content-hashed and therefore safe to precache forever; the list is
 * a guard so a future emitted artefact of some other kind (a source map, a
 * report) is not silently pinned into the cache.
 *
 * Files copied verbatim from web/public (the icons, the web manifest) are not
 * part of the bundle and are deliberately not precached: they are fetched on
 * demand, and the browser keeps its own copy once the app is installed.
 */
const SHELL_EXTENSIONS = ['.js', '.css', '.woff', '.woff2', '.svg', '.png', '.webmanifest']

/**
 * Builds the precache manifest from the file names of a finished bundle: the
 * shell document first, then every shell asset, as root-absolute paths, sorted
 * so the same bundle always yields the same manifest (and thus the same cache
 * name). The worker itself is never precached — it must always be revalidated.
 */
export function buildPrecacheManifest(fileNames: readonly string[]): string[] {
  const assets = fileNames
    .filter((name) => name !== SERVICE_WORKER_FILE && name !== 'index.html')
    .filter((name) => SHELL_EXTENSIONS.some((ext) => name.endsWith(ext)))
    .sort()
  return ['/index.html', ...assets.map((name) => `/${name}`)]
}

/**
 * Derives the cache name for a manifest. Same shell, same name (so a rebuild
 * that changed nothing keeps the reader's cache warm); any change to any asset
 * URL, a new name.
 */
export function cacheNameFor(manifest: readonly string[]): string {
  const digest = createHash('sha256').update(manifest.join('\n')).digest('hex')
  return `${CACHE_PREFIX}${digest.slice(0, 12)}`
}

/**
 * Substitutes the build-time placeholders in the worker source. Exported so a
 * test can render a worker and actually run it, rather than trusting the shape
 * of the output by eye.
 */
export function renderServiceWorker(source: string, manifest: readonly string[]): string {
  if (!source.includes(CACHE_NAME_PLACEHOLDER) || !source.includes(PRECACHE_PLACEHOLDER)) {
    throw new Error('service-worker.js is missing its build-time placeholders')
  }
  return source
    .replace(CACHE_NAME_PLACEHOLDER, JSON.stringify(cacheNameFor(manifest)))
    .replace(PRECACHE_PLACEHOLDER, JSON.stringify(manifest))
}

/** Absolute path of the worker template, resolved relative to this module. */
export function serviceWorkerSourcePath(): string {
  return resolve(dirname(fileURLToPath(import.meta.url)), 'service-worker.js')
}

/**
 * The Vite plugin. Build-only: in `vite dev` there is no bundle to precache and
 * no worker is emitted, which is also why registration is gated on a production
 * build (see src/pwa/register.ts).
 */
export function pwaPlugin(): Plugin {
  return {
    name: 'kukatko-pwa',
    apply: 'build',
    generateBundle(_options, bundle) {
      const manifest = buildPrecacheManifest(Object.keys(bundle))
      const source = readFileSync(serviceWorkerSourcePath(), 'utf8')
      this.emitFile({
        type: 'asset',
        fileName: SERVICE_WORKER_FILE,
        source: renderServiceWorker(source, manifest),
      })
    },
  }
}

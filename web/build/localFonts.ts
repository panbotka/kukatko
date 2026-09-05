/**
 * Build-time guarantee that the bundle asks no font server for anything.
 *
 * Kukátko is a self-hosted family archive: the photos never leave the owner's
 * hardware, so neither should the viewer's IP address. Bootswatch's compiled
 * theme opens with
 * `@import url(https://fonts.googleapis.com/css2?family=Lato:wght@300;400;700&display=swap)`,
 * and an `@import` of an absolute URL is the one thing a CSS bundler cannot
 * inline — Vite hoists it to the top of the built stylesheet instead. Every page
 * load of the single self-contained binary therefore called Google, and an
 * instance without egress silently lost its typeface.
 *
 * The fix has two halves. `src/styles/fonts.css` declares the same Lato weights
 * from files that ship inside the binary; this plugin removes the remote
 * `@import` from the theme *before* Vite's CSS pipeline sees it, so the built
 * output cannot carry it. The removal is deliberately narrow — only imports
 * pointing at Google's two font hosts — so a genuine stylesheet import is never
 * silently dropped.
 *
 * `generateBundle` then re-checks the finished assets. That guard is the
 * project's answer to "check the built CSS, not just the source": a Bootswatch
 * upgrade that renames the host, or a new dependency that adds its own webfont
 * call, fails the build rather than quietly re-introducing the outbound request.
 */
import type { OutputAsset, OutputChunk } from 'rollup'
import type { Plugin } from 'vite'

/** The font hosts no built asset may mention. */
export const REMOTE_FONT_HOSTS = ['fonts.googleapis.com', 'fonts.gstatic.com'] as const

/** The two hosts spelled as one alternation, ready for a regular expression. */
const HOST_PATTERN = REMOTE_FONT_HOSTS.map((host) => host.replaceAll('.', String.raw`\.`)).join('|')

/**
 * Matches a whole `@import` statement whose target is one of the hosts above.
 *
 * Both CSS spellings have to be covered, because the theme is read minified in
 * a production build (`@import"https://…";`) and unminified in a dev one
 * (`@import url(https://…);`). The captured quote is back-referenced as the
 * closing delimiter so the path may itself contain the semicolons Google's
 * `family=Lato:wght@300;400;700` query carries — matching up to the first `;`
 * would cut the statement in half and leave the rest as stray CSS.
 */
const REMOTE_FONT_IMPORT_RE = new RegExp(
  String.raw`@import\s*(?:url\()?\s*(['"]?)https?://(?:${HOST_PATTERN})/[^'"()]*\1\s*\)?[^;]*;?`,
  'gi',
)

/**
 * Removes every remote webfont `@import` from a stylesheet. Pure, so the rule
 * that decides what counts as a font call can be tested on its own.
 */
export function stripRemoteFontImports(css: string): string {
  return css.replace(REMOTE_FONT_IMPORT_RE, '')
}

/**
 * Reports the remote font hosts a finished asset still mentions. Used by the
 * build guard, and exported so a test can assert on the detection itself.
 */
export function findRemoteFontHosts(content: string): string[] {
  return REMOTE_FONT_HOSTS.filter((host) => content.includes(host))
}

/** Vite hands `transform` every module id; only stylesheets can hold an `@import`. */
function isStylesheet(id: string): boolean {
  return /\.(css|scss|sass|less|styl|stylus|pcss|postcss)(\?|$)/.test(id)
}

/**
 * The text of one emitted output. A binary asset (the font files themselves)
 * has a `Uint8Array` source and cannot hold a URL, so it reads as empty.
 */
function textOf(output: OutputAsset | OutputChunk): string {
  if (output.type === 'chunk') {
    return output.code
  }
  return typeof output.source === 'string' ? output.source : ''
}

/**
 * The plugin. `enforce: 'pre'` so the strip runs before Vite's own CSS plugin
 * resolves and hoists the import, and `apply: 'build'` is deliberately NOT set:
 * the dev server must be as offline-capable as the binary.
 */
export function localFontsPlugin(): Plugin {
  return {
    name: 'kukatko-local-fonts',
    enforce: 'pre',
    transform(code, id) {
      if (!isStylesheet(id)) {
        return null
      }
      const stripped = stripRemoteFontImports(code)
      return stripped === code ? null : { code: stripped, map: null }
    },
    // `order: 'post'` so the guard sees the *finished* bundle: the stylesheet
    // Vite assembles, and anything a later plugin emitted. A `pre` plugin's
    // generateBundle runs before those exist and would inspect nothing.
    generateBundle: {
      order: 'post',
      handler(_options, bundle) {
        for (const [fileName, output] of Object.entries(bundle)) {
          const hosts = findRemoteFontHosts(textOf(output))
          if (hosts.length > 0) {
            this.error(
              `${fileName} still reaches out to ${hosts.join(', ')}; ` +
                'the frontend must ship its webfonts inside the binary (see web/src/styles/fonts.css)',
            )
          }
        }
      },
    },
  }
}

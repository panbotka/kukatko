/**
 * Types for hls.js's **light** build.
 *
 * The package ships one set of declarations (`dist/hls.d.ts`) but maps them only
 * to its main entry point; `hls.js/light` — the same API minus the DRM, subtitle
 * and alternate-audio handling this app has no use for — resolves to a JavaScript
 * file with nothing beside it. Re-export the declarations under that specifier so
 * the player can import the smaller build without losing its types.
 */
declare module 'hls.js/light' {
  export * from 'hls.js'
  export { default } from 'hls.js'
}

import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

/**
 * The viewer's figure stacks four layers over one box: the blurred stand-in, the
 * smaller rendition, the photograph, and the face boxes traced on it. Three of
 * them state a z-index, so the fourth cannot be left to DOM order — 0.17.1
 * shipped exactly that mistake. The photograph took `z-index: 2` to clear its two
 * stand-ins, the face layer stated nothing, and the photograph painted over every
 * box: the faces view drew boxes that were invisible and, because the image also
 * won the hit test, unclickable.
 *
 * jsdom computes no paint order, so this guard pins the declarations that decide
 * it: the figure contains its own stacking (`isolation`), which both keeps the
 * chrome above the whole figure and lets the layers below be ordered among
 * themselves, and the face layer outranks the photograph inside it.
 */
describe('viewer figure paint order', () => {
  const css = readCss('src/components/photo/viewer.css')

  const zIndexOf = (prelude: RegExp): string | undefined => {
    const body = ruleBody(css, prelude)
    if (body === undefined) {
      throw new Error(`viewer.css declares no rule for ${prelude.source}`)
    }
    return declarations(body).get('z-index')
  }

  it('contains the figure stacking so the chrome stays above the photograph', () => {
    const body = ruleBody(css, /\.kk-viewer__figure\s*(?=\{)/)
    if (body === undefined) {
      throw new Error('viewer.css declares no .kk-viewer__figure rule')
    }
    expect(declarations(body).get('isolation')).toBe('isolate')
  })

  it('paints the face boxes above the photograph they trace', () => {
    const faces = zIndexOf(/\.kk-viewer__figure > \.kk-face-layer\s*(?=\{)/)
    const image = zIndexOf(/\.kk-viewer__figure > \.kk-viewer__image\s*(?=\{)/)
    expect(Number(faces)).toBeGreaterThan(Number(image))
  })

  it('keeps the photograph above the stand-in it replaces', () => {
    const image = zIndexOf(/\.kk-viewer__figure > \.kk-viewer__image\s*(?=\{)/)
    const under = zIndexOf(/\.kk-viewer__figure > \.kk-viewer__image--under\s*(?=\{)/)
    expect(Number(image)).toBeGreaterThan(Number(under))
  })
})

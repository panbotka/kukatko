import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

/**
 * The media load-in (`components/FadeInImage.tsx`) fades the photograph in
 * *over* its blurred stand-in and never removes the stand-in — which only works
 * while the image paints on top. jsdom computes no paint order, so this guard
 * pins the two declarations the stacking depends on: the stand-in is absolutely
 * positioned inside the tile box, and the image is positioned too, so DOM order
 * (image after stand-in) decides who is on top. A static image under an
 * absolutely positioned sibling loses regardless of DOM order — every tile in
 * the library stayed a blur after its photo had loaded, which is how 0.17.0
 * shipped exactly that.
 */
describe('media load-in paint order', () => {
  const css = readCss('src/styles/app.css')

  it('positions the image so DOM order paints it above the blurred stand-in', () => {
    const body = ruleBody(css, /\.kk-media-img\s*(?=\{)/)
    if (body === undefined) {
      throw new Error('app.css declares no .kk-media-img rule')
    }
    expect(declarations(body).get('position')).toBe('relative')
  })

  it('keeps the blurred stand-in absolutely positioned behind the image', () => {
    const body = ruleBody(css, /\.kk-media-blur\s*(?=\{)/)
    if (body === undefined) {
      throw new Error('app.css declares no .kk-media-blur rule')
    }
    expect(declarations(body).get('position')).toBe('absolute')
  })
})

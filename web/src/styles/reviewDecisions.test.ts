import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

/**
 * The review-decision table's thumbnail is where a wrong Ano/Ne gets spotted, so
 * its size and focus ring are promises jsdom cannot see: a 96 px square on a
 * desktop, smaller on a phone (so the table never scrolls sideways), and a
 * visible ring on the link that opens the photo. These guards read the shipped
 * stylesheet for those facts.
 */

const app = readCss('src/styles/app.css')

/** The phone variant: the `@media` block that shrinks the thumbnail. */
const phone = ruleBody(app, /@media \(max-width: 575\.98px\)\s*(?=\{)/, /\.kk-decision-thumb\s*\{/)

describe('review decision thumbnails', () => {
  it('draws a 96 px square that fills with a centre crop', () => {
    const thumb = declarations(ruleBody(app, /\.kk-decision-thumb\s*(?=\{)/) ?? '')
    expect(thumb.get('width')).toBe('6rem')
    expect(thumb.get('height')).toBe('6rem')
    expect(thumb.get('object-fit')).toBe('cover')
    // A block box, so the image and the blank well are the same height.
    expect(thumb.get('display')).toBe('block')
  })

  it('shrinks the square on a phone', () => {
    expect(phone).toBeDefined()
    const thumb = declarations(ruleBody(phone ?? '', /\.kk-decision-thumb\s*(?=\{)/) ?? '')
    expect(thumb.get('width')).toBe('4rem')
    expect(thumb.get('height')).toBe('4rem')
  })

  it('rings the photo link when it has keyboard focus', () => {
    const focus = declarations(
      ruleBody(app, /\.kk-decision-thumb-link:focus-visible\s*(?=\{)/) ?? '',
    )
    expect(focus.get('outline')).toBe('var(--kk-focus-ring-width) solid var(--kk-focus-ring-color)')
  })
})

import { describe, expect, it } from 'vitest'

import { revealPressedChip } from './chipStrip'

describe('revealPressedChip', () => {
  /**
   * A strip of `width` px holding chips of 100px at a 10px pitch, scrolled to
   * `scrollLeft`. jsdom lays nothing out, so the geometry is stated.
   */
  function strip(width: number, pressed: number, scrollLeft = 0): HTMLElement {
    const el = document.createElement('div')
    const chips = Array.from({ length: 10 }, (_, i) => {
      const chip = document.createElement('button')
      chip.setAttribute('aria-pressed', String(i === pressed))
      const left = i * 110 - scrollLeft
      chip.getBoundingClientRect = () => ({ left, width: 100 }) as DOMRect
      el.append(chip)
      return chip
    })
    Object.defineProperty(el, 'clientWidth', { value: width })
    Object.defineProperty(el, 'scrollWidth', { value: chips.length * 110 })
    el.scrollLeft = scrollLeft
    el.getBoundingClientRect = () => ({ left: 0, width }) as DOMRect
    return el
  }

  it('centres the pressed chip of an overflowing strip', () => {
    const el = strip(390, 6)
    revealPressedChip(el)
    // Chip 6 starts at 660; centred in 390px it starts at 145 → scrolled 515.
    expect(el.scrollLeft).toBe(515)
  })

  it('measures from where the strip already is', () => {
    const el = strip(390, 6, 200)
    revealPressedChip(el)
    expect(el.scrollLeft).toBe(515)
  })

  it('never scrolls before the start for a chip near it', () => {
    const el = strip(390, 0, 50)
    revealPressedChip(el)
    expect(el.scrollLeft).toBe(0)
  })

  it('leaves a strip that fits alone', () => {
    // Ten chips at a 110px pitch fit in 1400px: nothing to scroll.
    const el = strip(1400, 6, 0)
    revealPressedChip(el)
    expect(el.scrollLeft).toBe(0)
  })
})

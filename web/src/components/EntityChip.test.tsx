import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode } from 'react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import { readCss, ruleBody } from '../test/css'
import { EntityChip } from './EntityChip'

function renderChip(ui: ReactNode) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

describe('EntityChip', () => {
  it('links to the entity with its hue and a decorative glyph', () => {
    const { container } = renderChip(
      <EntityChip kind="album" to="/albums/a1">
        Holidays
      </EntityChip>,
    )

    const chip = screen.getByRole('link', { name: 'Holidays' })
    expect(chip).toHaveAttribute('href', '/albums/a1')
    expect(chip).toHaveClass('kk-entity-album')
    // The glyph reinforces the hue for a colour-blind reader and stays out of
    // the accessible name.
    const icon = container.querySelector('.bi-collection')
    expect(icon).toHaveAttribute('aria-hidden', 'true')
  })

  it('gives a label its own hue, so the two kinds never read alike', () => {
    renderChip(
      <>
        <EntityChip kind="album" to="/albums/a1">
          Holidays
        </EntityChip>
        <EntityChip kind="tag" to="/labels/l1">
          sunset
        </EntityChip>
      </>,
    )

    expect(screen.getByRole('link', { name: 'Holidays' })).not.toHaveClass('kk-entity-tag')
    expect(screen.getByRole('link', { name: 'sunset' })).toHaveClass('kk-entity-tag')
  })

  it('merges extra classes onto the pill', () => {
    renderChip(
      <EntityChip kind="tag" to="/labels/l1" className="fw-normal">
        sunset
      </EntityChip>,
    )

    expect(screen.getByRole('link', { name: /sunset/ })).toHaveClass('badge', 'fw-normal')
  })

  it('names what the remove control detaches and calls back', async () => {
    const onRemove = vi.fn()
    const user = userEvent.setup()
    renderChip(
      <EntityChip kind="album" to="/albums/a1" remove={{ label: 'Remove from Holidays', onRemove }}>
        Holidays
      </EntityChip>,
    )

    const remove = screen.getByRole('button', { name: 'Remove from Holidays' })
    // The X says the same thing to the mouse: an icon-only control with no
    // tooltip is a guess until it is pressed.
    expect(remove).toHaveAttribute('title', 'Remove from Holidays')

    await user.click(remove)
    expect(onRemove).toHaveBeenCalledTimes(1)
  })
})

/**
 * The tap target. `app.css` lifts a linked chip to the 44px finger floor on a
 * coarse pointer (the rule itself is pinned in `styles/tapTargets.test.ts`), but
 * it can only reach the elements it names — and jsdom evaluates no media query,
 * so what is asserted here is the shape the rule keys on: the pill *is* the
 * anchor, and where it cannot be, the anchor is its direct child.
 *
 * The bug this replaced: a 79.1 × 12.0px `<a>` inside a 111 × 20.9px decorative
 * pill, i.e. a chip whose visible edges answered no tap.
 */
describe('EntityChip as a touch target', () => {
  /** The selectors `app.css` sizes a linked chip by, read out of the sheet. */
  const coarse = ruleBody(
    readCss('src/styles/app.css'),
    /@media\s*\(pointer:\s*coarse\)/,
    /a\.badge/,
  )

  it('is the pill itself, so the floor rule reaches the whole chip', () => {
    renderChip(
      <EntityChip kind="album" to="/albums/a1">
        Holidays
      </EntityChip>,
    )

    const chip = screen.getByRole('link', { name: 'Holidays' })
    expect(chip.matches('a.badge')).toBe(true)
    // The glyph is inside the link, not beside it in the pill: a tap on the
    // leading icon has to open the album too.
    expect(chip.querySelector('.bi-collection')).not.toBeNull()
    expect(coarse).toContain('a.badge')
  })

  it('keeps the link a direct child once a remove X shares the pill', () => {
    renderChip(
      <EntityChip kind="tag" to="/labels/l1" remove={{ label: 'Remove sunset', onRemove: vi.fn() }}>
        sunset
      </EntityChip>,
    )

    const chip = screen.getByRole('link', { name: 'sunset' })
    const pill = chip.parentElement
    // An `<a>` may not contain a button, so here the pill is a span — but it is
    // still one pill: the link is its direct child (`.badge > a` stretches it to
    // the pill's full height) and the X trims it at the end.
    expect(pill?.tagName).toBe('SPAN')
    expect(pill).toHaveClass('badge')
    expect(chip.matches('.badge > a')).toBe(true)
    expect(coarse).toContain('.badge > a')
    expect(screen.getByRole('button', { name: 'Remove sunset' }).parentElement).toBe(pill)
  })
})

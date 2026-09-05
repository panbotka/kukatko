import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type ClusterView } from '../../services/people'
import { declarations, readCss, ruleBody } from '../../test/css'

import { ClusterCard } from './ClusterCard'

/** The common touch-target floor, in pixels (2.75rem at the 16px root size). */
const TOUCH_TARGET_PX = 44
const REM_PX = 16

/**
 * The narrowest the name field may ask to be: the Czech placeholder „Nové nebo
 * existující jméno…" measures 202 px in the app's own face (Lato 1rem, measured in
 * Chromium), and the control's padding and borders add 26 px on top of it.
 */
const NAME_FIELD_MIN_PX = 228

/** A one-sample cluster — enough to exercise the remove control. */
function cluster(): ClusterView {
  return {
    uid: 'fc_1',
    size: 3,
    representative: { photo_uid: 'p1', face_index: 0, bbox: [0.1, 0.1, 0.2, 0.2], det_score: 0.9 },
    examples: [{ photo_uid: 'p2', face_index: 2, bbox: [0.3, 0.3, 0.2, 0.2], det_score: 0.8 }],
    created_at: '2026-01-01T00:00:00Z',
  }
}

function renderCard(onRemoveFace = vi.fn()) {
  render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <ClusterCard
          cluster={cluster()}
          busy={false}
          onAssign={vi.fn()}
          onRemoveFace={onRemoveFace}
        />
      </MemoryRouter>
    </I18nextProvider>,
  )
  return {
    onRemoveFace,
    button: screen.getByRole('button', { name: 'Remove this face from the group' }),
  }
}

/** Resolves a `rem`/`px` length to pixels; anything else fails loudly. */
function lengthPx(value: string): number {
  const match = /^([\d.]+)(rem|px)$/.exec(value.trim())
  if (match === null) {
    throw new Error(`unsupported length: ${value}`)
  }
  return Number(match[1]) * (match[2] === 'rem' ? REM_PX : 1)
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('ClusterCard', () => {
  it('detaches the sample face it belongs to', async () => {
    const user = userEvent.setup()
    const { onRemoveFace, button } = renderCard()

    await user.click(button)

    expect(onRemoveFace).toHaveBeenCalledWith({ photo_uid: 'p2', face_index: 2 })
  })

  it('enlarges a 48px sample to the whole photo and detaches it from there', async () => {
    // A 48px square cut from a tile is not something anyone can answer „is this
    // the same person?" from — so the crop opens the frame it came from.
    const user = userEvent.setup()
    const { onRemoveFace } = renderCard()

    // [0] is the representative, [1] the sample.
    await user.click(screen.getAllByRole('button', { name: 'Enlarge the photo' })[1])

    const overlay = await screen.findByRole('dialog')
    expect(within(overlay).getByRole('img', { name: 'Sample face' })).toHaveAttribute(
      'src',
      expect.stringContaining('fit_1280'),
    )
    expect(within(overlay).getByTestId('review-open-photo')).toHaveAttribute('href', '/photos/p2')

    await user.click(
      within(overlay).getByRole('button', { name: 'Remove this face from the group' }),
    )

    expect(onRemoveFace).toHaveBeenCalledWith({ photo_uid: 'p2', face_index: 2 })
  })

  it('offers no detach for the representative, exactly as the card does not', async () => {
    const user = userEvent.setup()
    renderCard()

    await user.click(screen.getAllByRole('button', { name: 'Enlarge the photo' })[0])

    const overlay = await screen.findByRole('dialog')
    expect(within(overlay).getByTestId('review-open-photo')).toHaveAttribute('href', '/photos/p1')
    expect(
      within(overlay).queryByRole('button', { name: 'Remove this face from the group' }),
    ).toBeNull()
  })

  it('leaves the name field sized by the stylesheet, on a row that may wrap', () => {
    // The inline `flex-basis: 0` this field used to carry is the whole defect, and
    // the width it needs is a fact about the placeholder's own rendering — so the
    // geometry lives in `clusters.css`, where the guard below can read it.
    renderCard()

    const input = screen.getByLabelText('Name this group')
    expect(input).toHaveClass('kk-cluster-name__input')
    expect(input.style.flexBasis).toBe('')
    expect(input.style.minWidth).toBe('')
    // Field and submit are siblings on the one row the stylesheet lays out.
    const submit = screen.getByRole('button', { name: 'Name group' })
    expect(submit).toHaveClass('kk-cluster-name__submit')
    expect(input.parentElement).toHaveClass('kk-cluster-name')
    expect(submit.parentElement).toBe(input.parentElement)
  })

  it('leaves the remove control sized by the stylesheet, next to its sample', () => {
    // The inline `width: 18px; height: 18px` this button used to carry was exactly
    // what the touch floor could not undo (it caps height, nothing caps width), so
    // the geometry has to stay in CSS where the coarse-pointer layout can replace it.
    const { button } = renderCard()

    expect(button).toHaveClass('kk-cluster-sample__remove')
    expect(button.style.width).toBe('')
    expect(button.style.height).toBe('')
    // A sibling of the crop, not a child of it: the touch layout can then put the
    // control below the face instead of on top of it.
    const sample = button.parentElement
    expect(sample).toHaveClass('kk-cluster-sample')
    expect(sample?.querySelector('img')).toHaveAttribute('alt', 'Sample face')
  })
})

/**
 * jsdom applies no media queries, so the two pointer layouts are pinned by reading
 * the stylesheet: a compact corner badge on a mouse, and — the bug this guards — a
 * full-size touch target that is *out of the corner* on a phone. The old inline
 * 18px square met the app-wide `@media (pointer: coarse) .btn { min-height }` floor
 * and became an 18×44 red sliver down the middle of the face being judged.
 */
describe('cluster sample remove control', () => {
  const css = readCss('src/components/people/clusters.css')

  /** The declarations of a rule, or a loud failure when the class was renamed. */
  function rule(source: string, prelude: RegExp): Map<string, string> {
    const body = ruleBody(source, prelude)
    if (body === undefined) {
      throw new Error(`rule not found: ${prelude.source}`)
    }
    return declarations(body)
  }

  const coarse =
    ruleBody(css, /@media\s*\(pointer:\s*coarse\)\s*/, /kk-cluster-sample__remove/) ?? ''
  const fine = rule(css, /\.kk-cluster-sample\s+\.kk-cluster-sample__remove\s*(?=\{)/)
  const touch = rule(coarse, /\.kk-cluster-sample\s+\.kk-cluster-sample__remove\s*(?=\{)/)

  it('keeps the compact corner ✕ on a fine pointer', () => {
    expect(fine.get('position')).toBe('absolute')
    expect(fine.get('top')).toBe('0')
    expect(fine.get('right')).toBe('0')
    expect(lengthPx(fine.get('width') ?? '')).toBe(18)
    expect(lengthPx(fine.get('height') ?? '')).toBe(18)
  })

  it('gives touch a full target that does not overlay the face', () => {
    // Out of the corner entirely: not absolutely positioned over the crop any more.
    expect(touch.get('position')).toBe('static')
    // ...and comfortably tappable in BOTH dimensions, not just the height the
    // global touch floor would have set.
    expect(lengthPx(touch.get('min-width') ?? '')).toBeGreaterThanOrEqual(TOUCH_TARGET_PX)
    expect(lengthPx(touch.get('min-height') ?? '')).toBeGreaterThanOrEqual(TOUCH_TARGET_PX)
    // The fine-pointer 18px square must be released, or it would cap the target.
    expect(touch.get('width')).toBe('100%')
    expect(touch.get('height')).toBe('auto')
  })

  it('stacks the sample so the control sits under the face on touch', () => {
    const sample = rule(coarse, /\.kk-cluster-sample\s*(?=\{)/)
    expect(sample.get('display')).toBe('flex')
    expect(sample.get('flex-direction')).toBe('column')
  })
})

/**
 * The naming row is a wrapping flex line, and a flex line is broken on its items'
 * *hypothetical* sizes — so the field's basis, not its `min-width`, is what decides
 * whether the fixed-width button still fits beside it. With the `flex-basis: 0` the
 * row shipped with, the two always shared the row and the field absorbed every pixel
 * the button did not need: 41 px at the page's default five columns, too narrow to
 * read the placeholder or the name being typed. jsdom lays nothing out, so the guard
 * is on the declarations that decide the break.
 */
describe('cluster naming row', () => {
  const css = readCss('src/components/people/clusters.css')

  /** The declarations of a rule, or a loud failure when the class was renamed. */
  function rule(prelude: RegExp): Map<string, string> {
    const body = ruleBody(css, prelude)
    if (body === undefined) {
      throw new Error(`rule not found: ${prelude.source}`)
    }
    return declarations(body)
  }

  const row = rule(/\.kk-cluster-name\s*(?=\{)/)
  const input = rule(/\.kk-cluster-name__input\s*(?=\{)/)
  const submit = rule(/\.kk-cluster-name__submit\s*(?=\{)/)

  /** `flex: <grow> <shrink> <basis>` split into its three parts. */
  function flex(declared: string | undefined): [number, number, string] {
    const parts = (declared ?? '').split(/\s+/)
    if (parts.length !== 3) {
      throw new Error(`expected a three-part flex shorthand, got: ${String(declared)}`)
    }
    return [Number(parts[0]), Number(parts[1]), parts[2]]
  }

  it('gives the name field a basis wide enough for its own placeholder', () => {
    const [grow, , basis] = flex(input.get('flex'))
    // The field, not the button, takes the free space...
    expect(grow).toBeGreaterThan(0)
    // ...and it asks for at least the room the placeholder needs, so a card that
    // cannot spare it breaks the button onto the next line instead of squeezing
    // the field. A `0`/`auto` basis is exactly the defect.
    expect(lengthPx(basis)).toBeGreaterThanOrEqual(NAME_FIELD_MIN_PX)
  })

  it('lets the field shrink to the card rather than out of it', () => {
    // The tenth density and a phone's three columns are both narrower than any
    // basis worth asking for: there the field is alone on its line and must take
    // the card's width, not overflow it.
    const [, shrink] = flex(input.get('flex'))
    expect(shrink).toBeGreaterThan(0)
    expect(input.get('min-width')).toBe('0')
  })

  it('wraps the row and lets the button give way, never the field', () => {
    expect(row.get('display')).toBe('flex')
    expect(row.get('flex-wrap')).toBe('wrap')
    const [grow, shrink] = flex(submit.get('flex'))
    // The button neither claims free space nor refuses to shrink.
    expect(grow).toBe(0)
    expect(shrink).toBeGreaterThan(0)
    expect(submit.get('min-width')).toBe('0')
  })
})

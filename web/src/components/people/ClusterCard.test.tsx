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

/** The smallest a sample crop may be drawn at: room for the corner disc and a face. */
const SAMPLE_MIN_PX = 64

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

  it('enlarges a 64px sample to the whole photo and detaches it from there', async () => {
    // A 64px square cut from a tile is not something anyone can answer „is this
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

  it('leaves the remove control sized by the stylesheet, over its own sample', () => {
    // Geometry and colour live in `clusters.css`, so the guard below can read them;
    // and it is no `.btn`, whose app-wide touch floor (a 2.75rem `min-height` under a
    // coarse pointer) once stretched the corner ✕ into a red sliver down the face.
    const { button } = renderCard()

    expect(button).toHaveClass('kk-cluster-sample__remove')
    expect(button).not.toHaveClass('btn')
    expect(button).toHaveAttribute('type', 'button')
    expect(button).toHaveAttribute('title', 'Remove this face from the group')
    expect(button.style.width).toBe('')
    expect(button.style.height).toBe('')
    // A sibling of the crop inside the positioned sample, so the corner it sits
    // in is unmistakably this face's.
    const sample = button.parentElement
    expect(sample).toHaveClass('kk-cluster-sample')
    expect(sample?.querySelector('img')).toHaveAttribute('alt', 'Sample face')
  })

  it('draws the samples large enough to recognise and to carry the disc', () => {
    renderCard()

    const crop = screen.getByRole('img', { name: 'Sample face' }).parentElement
    expect(lengthPx(crop?.style.width ?? '')).toBeGreaterThanOrEqual(SAMPLE_MIN_PX)
  })
})

/**
 * jsdom applies no media queries and no `:hover`, so the control is pinned by
 * reading the stylesheet. One design on every device — a 28–32px muted disc in the
 * crop's corner, danger only when aimed at — whose touch target is grown by an
 * invisible pseudo-element rather than by resizing the disc. It replaced an 18px
 * red badge on desktop and a 44px red block under every face on a phone.
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

  const disc = rule(css, /\.kk-cluster-sample\s+\.kk-cluster-sample__remove\s*(?=\{)/)
  const aimed = rule(
    css,
    /\.kk-cluster-sample\s+\.kk-cluster-sample__remove:hover[^{]*:focus-visible[^{]*:active[^{]*(?=\{)/,
  )
  const coarse =
    ruleBody(css, /@media\s*\(pointer:\s*coarse\)\s*/, /kk-cluster-sample__remove/) ?? ''

  it('is a round disc of 28–32px in the top-right corner of the crop', () => {
    expect(disc.get('position')).toBe('absolute')
    expect(disc.get('top')).toBeDefined()
    expect(disc.get('right')).toBeDefined()
    expect(disc.get('border-radius')).toBe('50%')
    for (const side of ['width', 'height']) {
      const px = lengthPx(disc.get(side) ?? '')
      expect(px).toBeGreaterThanOrEqual(28)
      expect(px).toBeLessThanOrEqual(32)
    }
  })

  it('is muted at rest and turns danger only on hover, focus or press', () => {
    expect(disc.get('background-color')).toBe('var(--kk-photo-plate-strong)')
    expect(disc.get('color')).toBe('var(--kk-on-photo)')
    expect(aimed.get('background-color')).toBe('var(--kk-danger)')
    expect(aimed.get('color')).toBe('var(--kk-on-danger)')
  })

  it('keeps the disc its size on touch and grows only its target', () => {
    // Nothing in the coarse block resizes or moves the disc itself...
    expect(ruleBody(coarse, /\.kk-cluster-sample__remove\s*(?=\{)/)).toBeUndefined()
    // ...the target is an invisible pseudo-element, sized and centred by translate:
    // an inset pseudo is laid out against the padding box and would drift.
    const target = rule(coarse, /\.kk-cluster-sample__remove::before\s*(?=\{)/)
    expect(target.get('position')).toBe('absolute')
    expect(target.get('top')).toBe('50%')
    expect(target.get('left')).toBe('50%')
    expect(target.get('transform')).toBe('translate(-50%, -50%)')
    expect(lengthPx(target.get('width') ?? '')).toBeGreaterThanOrEqual(TOUCH_TARGET_PX)
    expect(lengthPx(target.get('height') ?? '')).toBeGreaterThanOrEqual(TOUCH_TARGET_PX)
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

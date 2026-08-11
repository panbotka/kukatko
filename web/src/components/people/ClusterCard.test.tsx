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
    expect(sample?.querySelector('[role="img"]')).toHaveAttribute('aria-label', 'Sample face')
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

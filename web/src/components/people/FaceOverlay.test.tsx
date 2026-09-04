import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type FaceView } from '../../services/people'
import { readCss, ruleBody } from '../../test/css'

import { FaceOverlay } from './FaceOverlay'

/** An unnamed detection and a named one, to cover both box styles. */
function faces(): FaceView[] {
  return [
    {
      face_index: 0,
      bbox: [0.1, 0.2, 0.3, 0.4],
      det_score: 0.9,
      action: 'create_marker',
      suggestions: [],
    },
    {
      face_index: 1,
      bbox: [0.5, 0.5, 0.2, 0.2],
      det_score: 0.8,
      action: 'assign_person',
      marker_uid: 'mk_1',
      subject_name: 'Alice',
      suggestions: [],
    },
  ]
}

function renderOverlay(
  readOnly = false,
  selected: number | null = null,
  boxes: FaceView[] = faces(),
  hovered: number | null = null,
) {
  const onSelect = vi.fn()
  const onHover = vi.fn()
  const result = render(
    <I18nextProvider i18n={i18n}>
      <FaceOverlay
        faces={boxes}
        selected={selected}
        hovered={hovered}
        onSelect={onSelect}
        onHover={onHover}
        readOnly={readOnly}
      />
    </I18nextProvider>,
  )
  return { ...result, onSelect, onHover }
}

/**
 * The four `--kk-face-*` custom properties a box is positioned by: the face's
 * centre and its size, as percentages of the layer. The floor under the apparent
 * size is `max()`/`clamp()` arithmetic in `faceOverlay.css`, and jsdom has no
 * layout to resolve it with — so the assertions here are on the geometry the
 * component hands the stylesheet, and the floor itself is asserted on the CSS.
 */
function geometry(el: HTMLElement) {
  return {
    x: el.style.getPropertyValue('--kk-face-x'),
    y: el.style.getPropertyValue('--kk-face-y'),
    w: el.style.getPropertyValue('--kk-face-w'),
    h: el.style.getPropertyValue('--kk-face-h'),
  }
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('FaceOverlay', () => {
  it('draws no image of its own — only the boxes over the photo below', () => {
    const { container } = renderOverlay()

    expect(container.querySelector('img')).toBeNull()
    expect(screen.getAllByRole('button')).toHaveLength(2)
  })

  it('positions each face box on the centre of its normalized bbox', () => {
    renderOverlay()

    // [0.1, 0.2, 0.3, 0.4] is centred on (0.25, 0.4). Centre-anchored rather
    // than corner-anchored on purpose: a box that hits the minimum size grows
    // around its face instead of sliding off it.
    expect(geometry(screen.getByRole('button', { name: 'Unnamed face 1' }))).toEqual({
      x: '25%',
      y: '40%',
      w: '30%',
      h: '40%',
    })
  })

  it('names a matched face by its subject and leaves unmatched ones numbered', () => {
    renderOverlay()

    expect(screen.getByRole('button', { name: 'Alice' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Unnamed face 1' })).toBeInTheDocument()
  })

  it('selects a face on click and marks the selected box pressed', async () => {
    const user = userEvent.setup()
    const { onSelect } = renderOverlay()

    await user.click(screen.getByRole('button', { name: 'Unnamed face 1' }))
    expect(onSelect).toHaveBeenCalledWith(0)
  })

  it('marks the selected box pressed', () => {
    renderOverlay(false, 0)

    expect(screen.getByRole('button', { name: 'Unnamed face 1' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
    expect(screen.getByRole('button', { name: 'Alice' })).toHaveAttribute('aria-pressed', 'false')
  })

  it('is read-only and click-through for viewers', () => {
    const { onSelect } = renderOverlay(true)

    const box = screen.getByRole('button', { name: 'Unnamed face 1' })
    expect(box).toBeDisabled()
    // The box does not swallow clicks meant for the image underneath it.
    expect(box).toHaveStyle({ pointerEvents: 'none' })
    expect(onSelect).not.toHaveBeenCalled()
  })

  it('colours each box by whether its face is named, in two colours only', () => {
    renderOverlay()

    // Yellow: nobody named on it yet. Green: a named person.
    const unnamed = screen.getByRole('button', { name: 'Unnamed face 1' })
    expect(unnamed).toHaveAttribute('data-face-state', 'unnamed')
    expect(unnamed).toHaveStyle({ borderColor: 'var(--bs-warning)' })

    const named = screen.getByRole('button', { name: 'Alice' })
    expect(named).toHaveAttribute('data-face-state', 'named')
    expect(named).toHaveStyle({ borderColor: 'var(--bs-success)' })
  })

  it('draws a face a marker already covers exactly like a bare detection', () => {
    // The marker/no-marker split is the backend's own bookkeeping; both are one
    // click from a name, so neither the colour nor the label may give it away.
    const [bare] = faces()
    renderOverlay(false, null, [bare, { ...bare, face_index: 2, marker_uid: 'mk_2' }])

    const boxes = screen.getAllByRole('button')
    expect(boxes).toHaveLength(2)
    for (const box of boxes) {
      expect(box).toHaveAttribute('data-face-state', 'unnamed')
      expect(box).toHaveStyle({ borderColor: 'var(--bs-warning)' })
    }
    // Neither is drawn in the colour that means "something is wrong".
    expect(screen.getByRole('button', { name: 'Unnamed face 2' })).not.toHaveStyle({
      borderColor: 'var(--bs-danger)',
    })
  })

  it('numbers every box', () => {
    renderOverlay()

    expect(screen.getByRole('button', { name: 'Unnamed face 1' })).toHaveTextContent('1')
    expect(screen.getByRole('button', { name: 'Alice' })).toHaveTextContent('2')
  })

  it('names only the box being pointed at, so labels cannot pile up', () => {
    // Every name drawn at once is what made a group photo unreadable: the labels
    // lay across each other and across the neighbouring boxes.
    const { unmount } = renderOverlay()
    expect(screen.queryByText('Alice')).not.toBeInTheDocument()
    unmount()

    // The page hands the pairing back as `hovered` — from hover or from focus,
    // which is what a finger and the keyboard produce instead.
    renderOverlay(false, null, faces(), 1)

    // The name label rides on the box; if it caught pointer events, clicking the
    // person's name would not select the face (and would kill the swipe gesture).
    const named = screen.getByRole('button', { name: 'Alice' })
    const label = screen.getByText('Alice')
    expect(named).toContainElement(label)
    expect(label).toHaveStyle({ pointerEvents: 'none' })
    // It also has to sit over its neighbours, or a box drawn later paints its
    // border straight through the label — and over the dimming veil, which is
    // the layer between the quiet boxes and the lit one.
    expect(named).toHaveStyle({ zIndex: '2' })
  })

  it('haloes the box being pointed at, so a panel row finds its face', () => {
    // Pointing at a row in the panel arrives here as `hovered`; at the minimum
    // size a border a pixel thicker is not something anybody can spot among
    // eighteen markers, so the pointed-at one takes a ring of its own.
    const { unmount } = renderOverlay(false, null, faces(), 1)
    expect(screen.getByRole('button', { name: 'Alice' })).toHaveStyle({
      boxShadow: '0 0 0 2px rgba(255, 255, 255, 0.7)',
    })
    // And the ones nobody is pointing at stay plain.
    expect(screen.getByRole('button', { name: 'Unnamed face 1' })).not.toHaveStyle({
      boxShadow: '0 0 0 2px rgba(255, 255, 255, 0.7)',
    })
    unmount()

    // The selected one keeps its own primary tint instead.
    renderOverlay(false, 1)
    expect(screen.getByRole('button', { name: 'Alice' })).toHaveStyle({
      boxShadow: '0 0 0 3px rgba(var(--bs-primary-rgb), 0.35)',
    })
  })

  it('names the selected box as well, since selection is where naming happens', () => {
    renderOverlay(false, 1)
    expect(screen.getByText('Alice')).toBeInTheDocument()
  })

  it('reports the hovered box so the panel can highlight its row', async () => {
    const user = userEvent.setup()
    const { onHover } = renderOverlay()

    await user.hover(screen.getByRole('button', { name: 'Unnamed face 1' }))
    expect(onHover).toHaveBeenCalledWith(0)

    await user.unhover(screen.getByRole('button', { name: 'Unnamed face 1' }))
    expect(onHover).toHaveBeenLastCalledWith(null)
  })

  it('pairs a focused box with its row too, since a finger never hovers', async () => {
    const user = userEvent.setup()
    const { onHover } = renderOverlay()

    await user.tab()
    expect(screen.getByRole('button', { name: 'Unnamed face 1' })).toHaveFocus()
    expect(onHover).toHaveBeenLastCalledWith(0)

    // Moving on drops the first box's pairing before lighting the next one, so
    // exactly one row is ever highlighted.
    await user.tab()
    expect(onHover).toHaveBeenNthCalledWith(2, null)
    expect(onHover).toHaveBeenLastCalledWith(1)
  })

  it('carries the touch hit-box hook without resizing the drawn outline', () => {
    renderOverlay()

    // The finger-sized hit area is a transparent `::after` on this class, only
    // on a coarse pointer (`app.css`, guarded in `styles/tapTargets.test.ts`) —
    // the box itself stays exactly the bbox, or the outline would slide off the
    // face it traces.
    const box = screen.getByRole('button', { name: 'Alice' })
    expect(box).toHaveClass('kk-face-box')
    // [0.5, 0.5, 0.2, 0.2] is centred on (0.6, 0.6) and keeps its own size: the
    // floor in `faceOverlay.css` only ever raises it, never shrinks it.
    expect(geometry(box)).toEqual({ x: '60%', y: '60%', w: '20%', h: '20%' })
  })

  it('turns every box with the photo, so the rectangles stay on the faces', () => {
    // Detection ran on the upright original; a quarter turn clockwise moves the
    // top-left corner to the top-right, so [x, y, w, h] becomes
    // [1 - y - h, x, h, w] — here [0.1, 0.2, 0.3, 0.4] → [0.4, 0.1, 0.4, 0.3].
    render(
      <I18nextProvider i18n={i18n}>
        <FaceOverlay
          faces={faces()}
          selected={null}
          onSelect={vi.fn()}
          rotation={90}
          frameRatio={2}
        />
      </I18nextProvider>,
    )

    expect(geometry(screen.getByRole('button', { name: 'Unnamed face 1' }))).toEqual({
      x: '60%',
      y: '25%',
      w: '40%',
      h: '30%',
    })
    // The layer takes the turned photo's own box — the wrapper's height by its
    // width, centred — since that is what the rotated image paints over.
    expect(screen.getByTestId('face-overlay')).toHaveStyle({
      left: '50%',
      top: '50%',
      width: '50%',
      height: '200%',
      transform: 'translate(-50%, -50%)',
    })
  })

  it('keeps the numbers and names upright on a turned photo', () => {
    // The layer is never given a `rotate()`: the rotation lives in the boxes'
    // coordinates, so a 180° photo does not hand the reader upside-down text.
    render(
      <I18nextProvider i18n={i18n}>
        <FaceOverlay
          faces={faces()}
          selected={1}
          onSelect={vi.fn()}
          rotation={180}
          frameRatio={2}
        />
      </I18nextProvider>,
    )

    const layer = screen.getByTestId('face-overlay')
    expect(layer).not.toHaveStyle({ transform: 'rotate(180deg)' })
    // A half turn keeps the frame's shape, so the layer simply fills the wrapper.
    expect(layer).toHaveStyle({ width: '100%', height: '100%' })
    // [0.5, 0.5, 0.2, 0.2] mirrored through the centre, i.e. centred on (0.4, 0.4).
    expect(geometry(screen.getByRole('button', { name: 'Alice' }))).toEqual({
      x: '40%',
      y: '40%',
      w: '20%',
      h: '20%',
    })
  })

  it('lights the selected face and dims the rest of the photograph', () => {
    // The review game already singles a face out of a crowd this way, and this is
    // the same treatment rather than a second one: a huge translucent spread with
    // a hole on the face, clipped by its own container.
    const { unmount } = renderOverlay()
    expect(screen.queryByTestId('face-dim')).not.toBeInTheDocument()
    unmount()

    renderOverlay(false, 1)
    const veil = screen.getByTestId('face-dim')
    // Decoration: the selected face already has a pressed button and an open
    // naming panel saying what it is, so the veil says nothing of its own.
    expect(veil).toHaveAttribute('aria-hidden', 'true')

    const hole = veil.firstElementChild as HTMLElement
    expect(hole).toHaveClass('kk-face-dim__hole')
    // The hole sits on the selected face, from the same geometry as its marker —
    // so the lit patch and the outline in it can never disagree.
    expect(geometry(hole)).toEqual({ x: '60%', y: '60%', w: '20%', h: '20%' })
    // And the selected marker is above the veil, where the others are below it.
    expect(screen.getByRole('button', { name: 'Alice' })).toHaveStyle({ zIndex: '3' })
    expect(screen.getByRole('button', { name: 'Unnamed face 1' })).not.toHaveStyle({
      zIndex: '3',
    })
  })

  it('dims nothing while the frame under it is still provisional', () => {
    // A selection made on the previous photo must not veil a photograph whose
    // boxes are not being drawn yet: the hole would be over nothing.
    render(
      <I18nextProvider i18n={i18n}>
        <FaceOverlay faces={faces()} selected={1} onSelect={vi.fn()} measured={false} />
      </I18nextProvider>,
    )
    expect(screen.queryByTestId('face-dim')).not.toBeInTheDocument()
  })

  it('keeps a top-edge box its number badge instead of drawing it off the photo', () => {
    // The badge normally hangs above the box; on a face at the very top of the
    // frame that puts it outside the photograph, where the container's overflow
    // eats it. It is drawn inside the box there instead.
    const top: FaceView[] = [{ ...faces()[0], bbox: [0.1, 0.01, 0.1, 0.1] }]
    renderOverlay(false, null, top)

    expect(screen.getByText('1')).not.toHaveStyle({ transform: 'translate(-2px, -100%)' })
  })

  it('guarantees a marker a minimum drawn size, raised to the touch floor', () => {
    // jsdom has no layout, so the promise "never a handful of pixels" is asserted
    // where it lives: the stylesheet. Same shape as the outlier card's marker —
    // a floor on the size, centre-anchored and clamped so hitting it grows the
    // box around the face and never past the edge of the frame.
    const css = readCss('src/components/people/faceOverlay.css')
    const box = ruleBody(css, /\.kk-face-box\s*(?=\{)/) ?? ''

    expect(box).toMatch(/width:\s*max\(var\(--kk-face-w[^)]*\),\s*var\(--kk-face-min\)\)/)
    expect(box).toMatch(/height:\s*max\(var\(--kk-face-h[^)]*\),\s*var\(--kk-face-min\)\)/)
    expect(box).toMatch(/left:\s*clamp\(/)
    expect(box).toMatch(/top:\s*clamp\(/)
    expect(box).toMatch(/transform:\s*translate\(-50%,\s*-50%\)/)

    // The floor is inherited from the layer, so the veil's hole uses the same
    // number, and a coarse pointer raises it to the touch floor: what the reader
    // sees is then what they can hit.
    const layer = ruleBody(css, /\.kk-face-layer\s*(?=\{)/) ?? ''
    expect(layer).toMatch(/--kk-face-min:\s*32px/)
    const coarse = ruleBody(css, /@media \(pointer: coarse\)\s*(?=\{)/) ?? ''
    expect(coarse).toMatch(/--kk-face-min:\s*44px/)
  })

  it("dims with the review game's own treatment, and never takes its clicks", () => {
    const css = readCss('src/components/people/faceOverlay.css')
    const veil = ruleBody(css, /\.kk-face-dim\s*(?=\{)/) ?? ''
    const hole = ruleBody(css, /\.kk-face-dim__hole\s*(?=\{)/) ?? ''

    // Its own clipping container, so the layer above it keeps the overflow a
    // quarter-turned photograph and the outside-the-box labels need.
    expect(veil).toMatch(/overflow:\s*hidden/)
    expect(veil).toMatch(/pointer-events:\s*none/)
    // Faded in on the shared duration token, which `prefers-reduced-motion`
    // collapses to an instant change (`tokens.css`).
    expect(veil).toMatch(/animation:[^;]*var\(--kk-duration-base\)/)
    // The spread that dims everything outside the hole — `review.css` does it
    // with exactly this.
    expect(hole).toMatch(/box-shadow:[\s\S]*0 0 0 9999px/)
  })

  it('draws no box while the frame under it is still provisional', () => {
    render(
      <I18nextProvider i18n={i18n}>
        <FaceOverlay faces={faces()} selected={null} onSelect={vi.fn()} measured={false} />
      </I18nextProvider>,
    )

    // The layer stays (the faces view is on), but percentages are only as good as
    // the box they are percentages of: against a wrapper sized from the catalogue
    // row rather than from the loaded image, a box can land off its face and then
    // jump. Better a box a moment late than a box on the wrong part of the photo.
    expect(screen.getByTestId('face-overlay')).toBeInTheDocument()
    expect(screen.queryAllByRole('button')).toHaveLength(0)
  })

  it('carries the class a host stylesheet stacks it by', () => {
    // `.kk-face-layer` is the hook `viewer.css` uses to paint the boxes above the
    // photograph; renaming it here without renaming it there gives back the bug
    // where the photo covered every box and swallowed its clicks.
    render(
      <I18nextProvider i18n={i18n}>
        <FaceOverlay faces={faces()} selected={null} onSelect={vi.fn()} />
      </I18nextProvider>,
    )

    expect(screen.getByTestId('face-overlay')).toHaveClass('kk-face-layer')
  })
})

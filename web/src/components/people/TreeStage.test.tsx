import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { TreeStage } from './TreeStage'

/**
 * These tests guard **when** the stage captures the pointer, which is not a
 * detail: capture retargets every mouse event derived from that pointer to the
 * capturing element, so a capture taken on `pointerdown` sends the `mouseup` —
 * and with it the `click` — to the `<svg>` instead of to the person, fold circle
 * or empty slot the reader pressed. Every control inside the drawing then looks
 * fine and does nothing, for a mouse only.
 *
 * jsdom cannot reproduce that retargeting — `src/test/setup.ts` stubs the whole
 * Pointer Capture API as inert no-ops, so a simulated click on a child passes
 * either way. The capture *call* is therefore what a test can hold on to, and
 * the reason these assertions look indirect.
 */
describe('TreeStage pointer handling', () => {
  /** A stage with one clickable person in it, as both canvases draw. */
  function renderStage(onPerson: () => void) {
    return render(
      <TreeStage content={{ width: 400, height: 300 }} resetKey="root">
        <a
          href="/people/p1/tree"
          aria-label="Anna"
          onClick={(event) => {
            event.preventDefault()
            onPerson()
          }}
        >
          <rect width={100} height={40} />
        </a>
      </TreeStage>,
    )
  }

  /** The transformed group's current `transform`, which says where the view is. */
  function transformOf(container: HTMLElement): string {
    return container.querySelector('svg > g')?.getAttribute('transform') ?? ''
  }

  function stageSvg(container: HTMLElement): SVGSVGElement {
    const svg = container.querySelector('svg.kk-tree-stage__svg')
    if (svg === null) {
      throw new Error('the stage has no svg')
    }
    return svg as SVGSVGElement
  }

  it('does not capture the pointer merely because a button went down', () => {
    // The regression: capturing here killed every control inside the drawing.
    const capture = vi.spyOn(Element.prototype, 'setPointerCapture')
    const { container } = renderStage(vi.fn())

    fireEvent.pointerDown(stageSvg(container), {
      pointerId: 7,
      button: 0,
      buttons: 1,
      clientX: 100,
      clientY: 100,
    })

    expect(capture).not.toHaveBeenCalled()
  })

  it('captures the pointer once the travel says this is a drag, and only once', () => {
    const capture = vi.spyOn(Element.prototype, 'setPointerCapture')
    const { container } = renderStage(vi.fn())
    const svg = stageSvg(container)

    fireEvent.pointerDown(svg, { pointerId: 7, button: 0, buttons: 1, clientX: 100, clientY: 100 })
    fireEvent.pointerMove(svg, { pointerId: 7, buttons: 1, clientX: 140, clientY: 100 })
    fireEvent.pointerMove(svg, { pointerId: 7, buttons: 1, clientX: 180, clientY: 120 })

    expect(capture).toHaveBeenCalledTimes(1)
    expect(capture).toHaveBeenCalledWith(7)
    // And the capture belongs to the stage, not to whatever the drag passed over.
    expect(capture.mock.contexts[0]).toBe(svg)
  })

  it('leaves a press that never passed the slop as the click it was', () => {
    const onPerson = vi.fn()
    const { container } = renderStage(onPerson)
    const svg = stageSvg(container)
    const person = screen.getByLabelText('Anna')

    fireEvent.pointerDown(svg, { pointerId: 7, button: 0, buttons: 1, clientX: 100, clientY: 100 })
    fireEvent.pointerMove(svg, { pointerId: 7, buttons: 1, clientX: 102, clientY: 101 })
    fireEvent.pointerUp(svg, { pointerId: 7, clientX: 102, clientY: 101 })
    fireEvent.click(person)

    expect(onPerson).toHaveBeenCalledTimes(1)
  })

  it('swallows the click a drag happens to end on', () => {
    // Panning the stage is one gesture, not a gesture and a visit to a person.
    const onPerson = vi.fn()
    const { container } = renderStage(onPerson)
    const svg = stageSvg(container)
    const person = screen.getByLabelText('Anna')

    fireEvent.pointerDown(svg, { pointerId: 7, button: 0, buttons: 1, clientX: 100, clientY: 100 })
    fireEvent.pointerMove(svg, { pointerId: 7, buttons: 1, clientX: 160, clientY: 130 })
    fireEvent.pointerUp(svg, { pointerId: 7, clientX: 160, clientY: 130 })
    fireEvent.click(person)

    expect(onPerson).not.toHaveBeenCalled()
  })

  it("ignores a right-click, which is the browser's own menu", () => {
    const capture = vi.spyOn(Element.prototype, 'setPointerCapture')
    const { container } = renderStage(vi.fn())
    const svg = stageSvg(container)
    const before = transformOf(container)

    fireEvent.pointerDown(svg, { pointerId: 7, button: 2, buttons: 2, clientX: 100, clientY: 100 })
    fireEvent.pointerMove(svg, { pointerId: 7, buttons: 2, clientX: 200, clientY: 200 })

    expect(capture).not.toHaveBeenCalled()
    expect(transformOf(container)).toBe(before)
  })

  it('stops panning when the button turns out to have been released off-stage', () => {
    // Before the capture is taken a release outside the stage is never heard;
    // without this the drawing would follow the bare cursor around.
    const { container } = renderStage(vi.fn())
    const svg = stageSvg(container)

    fireEvent.pointerDown(svg, { pointerId: 7, button: 0, buttons: 1, clientX: 100, clientY: 100 })
    fireEvent.pointerMove(svg, { pointerId: 7, buttons: 1, clientX: 102, clientY: 100 })
    const panned = transformOf(container)

    fireEvent.pointerMove(svg, { pointerId: 7, buttons: 0, clientX: 300, clientY: 300 })
    fireEvent.pointerMove(svg, { pointerId: 7, buttons: 0, clientX: 400, clientY: 400 })

    expect(transformOf(container)).toBe(panned)
  })

  it('pans the drawing while the button is held', () => {
    const { container } = renderStage(vi.fn())
    const svg = stageSvg(container)
    const before = transformOf(container)

    fireEvent.pointerDown(svg, { pointerId: 7, button: 0, buttons: 1, clientX: 100, clientY: 100 })
    fireEvent.pointerMove(svg, { pointerId: 7, buttons: 1, clientX: 160, clientY: 130 })

    expect(transformOf(container)).not.toBe(before)
  })
})

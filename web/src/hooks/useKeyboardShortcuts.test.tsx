import { fireEvent, render } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { type ShortcutMap, useKeyboardShortcuts } from './useKeyboardShortcuts'

/** A tiny harness that binds the given shortcuts and renders a text input. */
function Harness({ handlers, enabled }: { handlers: ShortcutMap; enabled?: boolean }) {
  useKeyboardShortcuts(handlers, { enabled })
  return (
    <>
      <input aria-label="field" />
      <button type="button">Select all</button>
    </>
  )
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('useKeyboardShortcuts', () => {
  it('dispatches to the handler for a matched key and prevents default', () => {
    const onF = vi.fn()
    render(<Harness handlers={{ f: onF }} />)

    const event = new KeyboardEvent('keydown', { key: 'f', cancelable: true })
    document.dispatchEvent(event)

    expect(onF).toHaveBeenCalledTimes(1)
    expect(event.defaultPrevented).toBe(true)
  })

  it('ignores keys with no registered handler', () => {
    const onF = vi.fn()
    render(<Harness handlers={{ f: onF }} />)
    fireEvent.keyDown(document, { key: 'g' })
    expect(onF).not.toHaveBeenCalled()
  })

  it('does not fire while the event target is a text input (typing)', () => {
    const onF = vi.fn()
    const { getByLabelText } = render(<Harness handlers={{ f: onF }} />)
    const input = getByLabelText('field')
    input.focus()
    fireEvent.keyDown(input, { key: 'f' })
    expect(onF).not.toHaveBeenCalled()
  })

  it('does not fire while a modal containing a form is open', () => {
    const onF = vi.fn()
    render(<Harness handlers={{ f: onF }} />)
    document.body.insertAdjacentHTML(
      'beforeend',
      '<div class="modal show"><input aria-label="modal-field" /></div>',
    )
    fireEvent.keyDown(document, { key: 'f' })
    expect(onF).not.toHaveBeenCalled()
  })

  it('ignores modifier chords (Ctrl/Meta/Alt) but allows Shift', () => {
    const onQuestion = vi.fn()
    render(<Harness handlers={{ '?': onQuestion }} />)

    fireEvent.keyDown(document, { key: '?', ctrlKey: true })
    expect(onQuestion).not.toHaveBeenCalled()

    // `?` is produced with Shift held; that must still fire.
    fireEvent.keyDown(document, { key: '?', shiftKey: true })
    expect(onQuestion).toHaveBeenCalledTimes(1)
  })

  it('normalizes the space bar to one token', () => {
    const onSpace = vi.fn()
    render(<Harness handlers={{ ' ': onSpace }} />)

    fireEvent.keyDown(document, { key: ' ' })
    fireEvent.keyDown(document, { key: 'Spacebar' })
    expect(onSpace).toHaveBeenCalledTimes(2)
  })

  it('leaves Space and Enter to the focused button, which owns them', () => {
    const onSpace = vi.fn()
    const onEnter = vi.fn()
    const onF = vi.fn()
    const { getByRole } = render(<Harness handlers={{ ' ': onSpace, Enter: onEnter, f: onF }} />)
    const button = getByRole('button', { name: 'Select all' })
    button.focus()

    const space = new KeyboardEvent('keydown', { key: ' ', cancelable: true, bubbles: true })
    button.dispatchEvent(space)
    fireEvent.keyDown(button, { key: 'Enter' })

    expect(onSpace).not.toHaveBeenCalled()
    expect(onEnter).not.toHaveBeenCalled()
    // The default is left alone too, so the browser still clicks the button.
    expect(space.defaultPrevented).toBe(false)

    // Only those two keys stand aside; the rest still work from a button.
    fireEvent.keyDown(button, { key: 'f' })
    expect(onF).toHaveBeenCalledTimes(1)
  })

  it('is inert when disabled', () => {
    const onF = vi.fn()
    render(<Harness handlers={{ f: onF }} enabled={false} />)
    fireEvent.keyDown(document, { key: 'f' })
    expect(onF).not.toHaveBeenCalled()
  })
})

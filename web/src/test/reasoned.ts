import { expect } from 'vitest'

/**
 * Asserts that a {@link ReasonedButton} is off, and — when a sentence is given —
 * that it is the sentence the reader gets.
 *
 * Why not `toBeDisabled()`: the button marks itself `aria-disabled` instead of
 * taking the native `disabled` attribute, so it keeps its place in the tab order
 * and its `title` on hover. jest-dom's matcher only looks at the attribute, so it
 * would report such a button as live.
 */
export function expectOff(button: HTMLElement, reason?: string): void {
  expect(button).toHaveAttribute('aria-disabled', 'true')
  if (reason !== undefined) {
    expect(button).toHaveAttribute('title', reason)
    const note = document.getElementById(button.getAttribute('aria-describedby') ?? '')
    expect(note).toHaveTextContent(reason)
  }
}

/** Asserts that a {@link ReasonedButton} is live — pressing it does something. */
export function expectLive(button: HTMLElement): void {
  expect(button).not.toHaveAttribute('aria-disabled')
  expect(button).toBeEnabled()
}

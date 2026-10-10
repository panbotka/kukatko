import { expect } from 'vitest'

/**
 * Asserts that a pressed-state toggle group marks its chosen option the way
 * every toggle in the app does: each option is a quiet button
 * (`btn-outline-secondary`), only the chosen one carries `.active` and
 * `aria-pressed="true"`. The accent fill itself is painted by
 * `.btn-outline-secondary.active` in `bootstrapBridge.css`, so an option on a
 * one-off variant (`light`, `secondary`, …) would escape it and look different.
 */
export function expectChosenToggle(options: readonly HTMLElement[], chosen: HTMLElement): void {
  expect(options).toContain(chosen)
  for (const option of options) {
    expect(option).toHaveClass('btn', 'btn-outline-secondary')
    const isChosen = option === chosen
    expect(option).toHaveAttribute('aria-pressed', String(isChosen))
    if (isChosen) {
      expect(option).toHaveClass('active')
    } else {
      expect(option).not.toHaveClass('active')
    }
  }
}

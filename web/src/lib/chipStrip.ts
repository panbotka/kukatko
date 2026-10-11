/**
 * Scrolls a chip strip so its first pressed chip sits in the middle — the state
 * filter, since those chips come first and one of them is always on. Centred
 * rather than merely in view, so its neighbours show that there is more either
 * side. A strip that does not overflow (every desktop) is left alone.
 */
export function revealPressedChip(strip: HTMLElement): void {
  if (strip.scrollWidth <= strip.clientWidth) {
    return
  }
  const chip = strip.querySelector<HTMLElement>('[aria-pressed="true"]')
  if (chip === null) {
    return
  }
  const stripBox = strip.getBoundingClientRect()
  const chipBox = chip.getBoundingClientRect()
  const chipStart = chipBox.left - stripBox.left + strip.scrollLeft
  strip.scrollLeft = Math.max(0, chipStart - (strip.clientWidth - chipBox.width) / 2)
}

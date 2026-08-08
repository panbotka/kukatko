import { fireEvent } from '@testing-library/react'

/**
 * Makes an `<img>` behave as if it had finished loading at the given natural
 * (post-EXIF-orientation) size: jsdom fetches nothing, so `naturalWidth`/
 * `naturalHeight` stay at zero and no `load` event ever fires.
 *
 * Anything that draws a box over a full-frame photo waits for exactly that pair
 * (`useImageFrame`), so a test about a box has to hand it over — and handing over
 * a size that contradicts the catalogue row is how the transposed-row bug is
 * reproduced at all.
 */
export function loadImageAs(img: HTMLImageElement, width: number, height: number): void {
  Object.defineProperty(img, 'naturalWidth', { value: width, configurable: true })
  Object.defineProperty(img, 'naturalHeight', { value: height, configurable: true })
  fireEvent.load(img)
}

/**
 * The inline `aspect-ratio` of a wrapper element as a number, so two frames can
 * be compared by shape rather than by the exact string they were written as —
 * `1440 / 1920`, `3000 / 4000` and `0.75` are all the same frame. Takes a
 * possibly-null `querySelector` result and throws on it, which keeps the call site
 * free of a cast.
 */
export function frameRatio(el: HTMLElement | null): number {
  if (el === null) {
    throw new Error('frameRatio: no element')
  }
  // Both CSS forms are in use: a `W / H` pair, and the bare number the review
  // stage needs (it multiplies the same ratio into a `calc()` height cap).
  const parts = el.style.aspectRatio.split('/')
  return parts.length > 1 ? Number(parts[0]) / Number(parts[1]) : Number(parts[0])
}

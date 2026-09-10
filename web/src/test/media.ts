import { fireEvent } from '@testing-library/react'
import { vi, type MockInstance } from 'vitest'

/** A `<video>` made drivable by {@link stubPlayableMedia}, with its transport spies. */
export interface PlayableMedia {
  /** The element itself, now answering `paused` / `duration` / `currentTime`. */
  readonly video: HTMLVideoElement
  /** The stubbed `play`, for asserting how many times a key actually toggled. */
  readonly play: MockInstance<() => Promise<void>>
  /** The stubbed `pause`, likewise. */
  readonly pause: MockInstance<() => void>
}

/**
 * Gives a `<video>` the playable surface jsdom has none of: it implements
 * neither playback nor a media clock, so `play`/`pause` are stubbed into
 * spies that flip `paused` and fire the matching event, and `duration` /
 * `currentTime` become ordinary settable properties. What is left is an element
 * a test can drive exactly the way the browser drives one — which is what the
 * player's transport and its keyboard are asserted against. The two spies come
 * back with it: a test that has to prove one press made **one** toggle counts
 * them, and reaching for `video.play` itself would be an unbound method.
 */
export function stubPlayableMedia(video: HTMLVideoElement, duration = 60): PlayableMedia {
  let paused = true
  let currentTime = 0
  Object.defineProperty(video, 'paused', { configurable: true, get: () => paused })
  Object.defineProperty(video, 'duration', { configurable: true, get: () => duration })
  Object.defineProperty(video, 'currentTime', {
    configurable: true,
    get: () => currentTime,
    set: (value: number) => {
      currentTime = value
    },
  })
  const play = vi.spyOn(video, 'play').mockImplementation(() => {
    paused = false
    fireEvent.play(video)
    return Promise.resolve()
  })
  const pause = vi.spyOn(video, 'pause').mockImplementation(() => {
    paused = true
    fireEvent.pause(video)
  })
  return { video, play, pause }
}

/**
 * Gives jsdom the fullscreen API it has none of at all — `document.fullscreenElement`
 * is not even defined there — as a readable element plus request/exit that flip
 * it and fire `fullscreenchange`, which is the sequence a real browser puts the
 * video player through. Returns the undo, which the caller must run: these are
 * properties on shared prototypes, not mocks the runner restores by itself.
 */
export function stubFullscreen(): () => void {
  let element: Element | null = null
  // `element = this` inside the prototype stub would be an aliased `this`; the
  // setter takes it as an argument instead, and both directions run one path.
  const settle = (next: Element | null): Promise<void> => {
    element = next
    document.dispatchEvent(new Event('fullscreenchange'))
    return Promise.resolve()
  }
  Object.defineProperty(document, 'fullscreenElement', {
    configurable: true,
    get: () => element,
  })
  Element.prototype.requestFullscreen = function requestFullscreen(this: Element) {
    return settle(this)
  }
  document.exitFullscreen = () => settle(null)
  return () => {
    Reflect.deleteProperty(document, 'fullscreenElement')
    Reflect.deleteProperty(Element.prototype, 'requestFullscreen')
    Reflect.deleteProperty(document, 'exitFullscreen')
  }
}

import '@testing-library/jest-dom/vitest'

import { cleanup } from '@testing-library/react'
import { afterEach, vi } from 'vitest'

// jsdom does not implement `window.matchMedia`, which react-bootstrap and any
// responsive component may touch. Provide a non-matching stub by default so
// components render at the "desktop" breakpoint; individual tests can override
// it (e.g. to simulate a phone) by reassigning `window.matchMedia`.
if (typeof window !== 'undefined' && typeof window.matchMedia !== 'function') {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    configurable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      // Deprecated listener API kept for libraries that still call it.
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  })
}

// jsdom does not implement `PointerEvent` either, and Testing Library then
// dispatches a bare `Event` for `pointerdown`/`pointermove` — one that carries no
// clientX/clientY. A drag test could only ever read NaN coordinates from it, so
// the drag it exercises would not be the one users perform. A MouseEvent
// subclass carries the coordinates and the few pointer properties components
// read.
if (typeof window !== 'undefined' && typeof window.PointerEvent !== 'function') {
  class StubPointerEvent extends MouseEvent {
    readonly pointerId: number
    readonly pointerType: string
    readonly isPrimary: boolean

    constructor(type: string, init: PointerEventInit = {}) {
      super(type, init)
      this.pointerId = init.pointerId ?? 0
      this.pointerType = init.pointerType ?? 'mouse'
      this.isPrimary = init.isPrimary ?? true
    }
  }
  Object.defineProperty(window, 'PointerEvent', {
    writable: true,
    configurable: true,
    value: StubPointerEvent,
  })
}

// jsdom does not implement the Pointer Capture API, so components that call
// setPointerCapture / hasPointerCapture / releasePointerCapture during a drag
// (e.g. the timeline scrubber) would throw. Provide inert no-op stubs so the
// production code can call them unconditionally.
if (typeof Element !== 'undefined') {
  const proto = Element.prototype as unknown as {
    setPointerCapture?: (pointerId: number) => void
    releasePointerCapture?: (pointerId: number) => void
    hasPointerCapture?: (pointerId: number) => boolean
  }
  proto.setPointerCapture ??= () => undefined
  proto.releasePointerCapture ??= () => undefined
  proto.hasPointerCapture ??= () => false
}

// jsdom implements no Blob URL store, so `URL.createObjectURL` is missing
// entirely and a component that previews a picked file locally — the upload
// queue's thumbnails — would throw on mount. Hand out a unique `blob:` URL and
// let revoking forget it; a test can assert the `src` and spy on the revoke,
// which is all there is to observe without a real image decoder.
if (typeof URL !== 'undefined' && typeof URL.createObjectURL !== 'function') {
  let issued = 0
  Object.defineProperty(URL, 'createObjectURL', {
    writable: true,
    configurable: true,
    value: (): string => {
      issued += 1
      return `blob:kukatko/${String(issued)}`
    },
  })
  Object.defineProperty(URL, 'revokeObjectURL', {
    writable: true,
    configurable: true,
    value: (): void => undefined,
  })
}

// jsdom does not implement scrollIntoView, which keyboard-driven grids call to keep
// the focused item on screen. Provide an inert stub so that production code can call
// it unconditionally.
if (typeof Element !== 'undefined') {
  const proto = Element.prototype as unknown as { scrollIntoView?: () => void }
  proto.scrollIntoView ??= () => undefined
}

// React Testing Library does not auto-clean between tests under Vitest's
// default config, so unmount rendered trees after each test to avoid leakage.
//
// Mock restoration is deliberately NOT done here (nor in any test file):
// `restoreMocks: true` in vite.config.ts restores every mock *before* each
// test, which is the only ordering that is safe. Restoring in an `afterEach`
// empties the module mocks while the tree is still mounted — the `cleanup()`
// below then unmounts it, React flushes the pending passive effects, and a
// service mock with no implementation returns `undefined`, so the calling
// hook's `.then` throws. That surfaced as a suite-order-dependent flake that
// blocked a release build, and an ESLint rule now bans the call in tests.
afterEach(() => {
  cleanup()
})

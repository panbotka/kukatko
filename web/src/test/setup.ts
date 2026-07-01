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

// React Testing Library does not auto-clean between tests under Vitest's
// default config, so unmount rendered trees after each test to avoid leakage.
afterEach(() => {
  cleanup()
})

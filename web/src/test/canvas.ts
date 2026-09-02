import { vi } from 'vitest'

/** The data URL {@link stubBlurCanvas} hands back for every decoded hash. */
export const STUB_CANVAS_DATA_URL = 'data:image/png;base64,kukatko-blur'

/**
 * Gives jsdom just enough `<canvas>` for the BlurHash placeholder to be painted.
 *
 * jsdom implements no 2D context at all (`getContext` returns null and complains
 * on the way), so `lib/blurPlaceholder` would correctly conclude that nothing can
 * be painted and every test would only ever see the fallback. This replaces the
 * two methods it uses — `getContext('2d')` and `toDataURL` — with stubs that do
 * nothing but hand back a fixed URL, leaving the real BlurHash decode (and every
 * decision around it) under test.
 *
 * Returns the `getContext` spy, whose call count is how a test tells a fresh
 * decode from a cache hit. Vitest's `restoreMocks` puts both methods back before
 * the next test.
 */
export function stubBlurCanvas(dataUrl: string = STUB_CANVAS_DATA_URL) {
  const context = {
    createImageData: (width: number, height: number) => ({
      data: new Uint8ClampedArray(width * height * 4),
      width,
      height,
      colorSpace: 'srgb',
    }),
    putImageData: () => undefined,
  }
  vi.spyOn(HTMLCanvasElement.prototype, 'toDataURL').mockReturnValue(dataUrl)
  return vi
    .spyOn(HTMLCanvasElement.prototype, 'getContext')
    .mockReturnValue(context as unknown as CanvasRenderingContext2D)
}

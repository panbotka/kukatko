/**
 * The MIME type of an HLS playlist. A browser that plays HLS itself — Safari on
 * every Apple device, and the Android stock browsers — answers `canPlayType`
 * with something other than the empty string for it.
 */
export const HLS_MIME_TYPE = 'application/vnd.apple.mpegurl'

/**
 * How a `<video>` element is fed:
 *
 * - `native` — the master playlist goes straight into `src` and the browser
 *   demuxes it; no library is loaded at all.
 * - `library` — hls.js is attached to the element and feeds it through Media
 *   Source Extensions; the element itself carries no `src`.
 * - `progressive` — the original file from the range-capable `/video` endpoint,
 *   exactly as before streaming existed.
 */
export type VideoDelivery = 'native' | 'library' | 'progressive'

/**
 * Asks the browser whether it plays HLS on its own.
 *
 * The probe is a detached `<video>` rather than the player's element, because
 * the answer has to be known while the first render decides what to put in
 * `src`: a player that guessed "progressive", rendered, and corrected itself in
 * an effect would already have started downloading the original.
 *
 * @returns Whether the browser claims it can play an HLS playlist.
 */
export function nativeHlsSupported(): boolean {
  if (typeof document === 'undefined') {
    return false
  }
  return document.createElement('video').canPlayType(HLS_MIME_TYPE) !== ''
}

/**
 * Chooses how to deliver a clip.
 *
 * A photo with no encoded rendition can only be played progressively, whatever
 * the browser supports. With a rendition, a browser that handles HLS itself is
 * left to it — loading a JavaScript demuxer to duplicate what the platform does
 * natively is a megabyte spent to make playback worse.
 *
 * @param streaming Whether the photo reports an encoded HLS rendition.
 * @param native Whether the browser plays HLS itself ({@link nativeHlsSupported}).
 * @returns The delivery the player should start with.
 */
export function videoDelivery(streaming: boolean, native: boolean): VideoDelivery {
  if (!streaming) {
    return 'progressive'
  }
  return native ? 'native' : 'library'
}

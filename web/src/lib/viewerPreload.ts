/**
 * Which images the photo viewer keeps warm around the one on stage.
 *
 * Paging through a library is the most repeated gesture in the app, and the wait
 * it used to carry was entirely avoidable: the next photograph is known long
 * before anybody asks for it, so its bytes can be on the machine — decoded —
 * before the arrow is even pressed. This module is only the *decision* of which
 * addresses that is; the fetching and the bounded release belong to
 * `useImagePreloader`.
 *
 * It is deliberately pure and deliberately small: one photo each side is the
 * whole window. A wider one buys nothing (nobody presses faster than a decode)
 * and costs somebody on a phone connection a page of bytes they never look at.
 */

/** The little a photo must state for the viewer to decide whether to warm it. */
export interface PreloadCandidate {
  /** The photo's UID. */
  uid: string
  /** Media kind (`image`, `video`, `live`); absent counts as an image. */
  mediaType?: string
}

/**
 * Whether a neighbour is worth warming — everything except a video.
 *
 * A video's stage is a player that streams the file itself, in ranges, when it
 * is opened; nothing this preloader could fetch shortens that, while fetching it
 * eagerly would pull a whole clip down the wire for a photo the user may never
 * step to. A live photo does count: what it shows at rest is its still, and the
 * motion clip is only fetched once somebody holds it.
 */
export function isPreloadable(candidate: PreloadCandidate | null): candidate is PreloadCandidate {
  return candidate !== null && candidate.mediaType !== 'video'
}

/**
 * The UIDs the viewer keeps warm: the photo on stage first, then the previous
 * and next ones, skipping videos and anything absent (a list end). The photo on
 * stage is in the window on purpose — it is being fetched anyway, so it costs no
 * request, and its presence is what lets the caller ask the preloader whether
 * the full-size image is already decoded before deciding to paint a smaller one
 * under it.
 *
 * Duplicates are dropped, so a one-photo list yields a single entry rather than
 * three copies of it.
 */
export function preloadUids(
  current: string,
  prev: PreloadCandidate | null,
  next: PreloadCandidate | null,
): string[] {
  const uids: string[] = current === '' ? [] : [current]
  for (const candidate of [prev, next]) {
    if (isPreloadable(candidate) && candidate.uid !== '' && !uids.includes(candidate.uid)) {
      uids.push(candidate.uid)
    }
  }
  return uids
}

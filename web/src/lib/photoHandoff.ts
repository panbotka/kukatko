/**
 * What a photo grid hands the viewer as it opens a photograph.
 *
 * The tile the user just clicked has an image on the glass, fully downloaded and
 * decoded. The viewer wants exactly that image as its first frame — but it
 * cannot rebuild the address: with originals in the object store a rendition URL
 * is *signed*, minted per response and stamped with its own expiry, so the URL
 * the viewer would compute for the same rendition of the same photo is a
 * different string and therefore a fresh download.
 *
 * So the grid passes the address along in the navigation state. That is the
 * whole trick: one string, carried across a route change, turning the viewer's
 * first paint from a request into a cache hit.
 *
 * Only an **aspect-preserving** rendition may be handed over. The square grid's
 * tile is a centre crop; painted under a fitted photograph it would show the
 * wrong part of it and then jump. A grid drawing squares hands over nothing and
 * the viewer falls back to the rendition its own payload names.
 */

/** The navigation state a grid attaches to the link into the viewer. */
export interface PhotoHandoff {
  /** UID of the photo the grid opened — the state is ignored for any other. */
  uid: string
  /** The exact aspect-preserving rendition URL the tile painted. */
  previewUrl: string
}

/** Whether an opaque history state is a well-formed {@link PhotoHandoff}. */
function isHandoff(state: unknown): state is PhotoHandoff {
  if (typeof state !== 'object' || state === null) {
    return false
  }
  const candidate = state as Partial<Record<keyof PhotoHandoff, unknown>>
  return typeof candidate.uid === 'string' && typeof candidate.previewUrl === 'string'
}

/**
 * The already-loaded rendition URL a grid handed over for `uid`, or undefined
 * when it handed over none.
 *
 * The UID is checked rather than trusted: history state outlives the navigation
 * that created it, so Back/Forward — and the viewer's own `replace` paging from
 * one photo to the next — can present the previous photo's handoff. Painting
 * that under the photograph on stage would be showing the wrong photograph, so a
 * mismatch is discarded. An empty address is discarded too: an `<img>` pointed
 * at "" re-requests the page itself.
 */
export function handoffPreviewUrl(state: unknown, uid: string): string | undefined {
  if (!isHandoff(state) || state.uid !== uid || state.previewUrl === '') {
    return undefined
  }
  return state.previewUrl
}

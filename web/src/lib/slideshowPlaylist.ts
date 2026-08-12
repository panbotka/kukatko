/**
 * The list a slideshow actually plays, and the seed that decides a shuffled
 * one's order.
 *
 * Kept free of React so both rules are directly unit-testable: the playlist is a
 * pure function of what has already been seen and what has been loaded, and the
 * seed is the one impure line, isolated here.
 */

/** How many random characters a shuffle seed carries. */
const SEED_LENGTH = 8

/**
 * A fresh seed for a shuffled show. The server derives the random order from the
 * photo uid and this string, so the seed has to stay the same for the whole life
 * of the show: page two of a show whose seed changed would be a different
 * permutation, overlapping page one and dropping whatever fell between them.
 *
 * It only has to differ between shows, so `Math.random` is exactly the right
 * source — nothing here needs to resist anybody.
 */
export function newShuffleSeed(): string {
  return Math.random()
    .toString(36)
    .slice(2, 2 + SEED_LENGTH)
}

/** The minimum a playlist entry has to carry: an identity to deduplicate on. */
interface Identified {
  uid: string
}

/**
 * The photos the show plays: everything already seen in this pass, in the order
 * it was seen, followed by every loaded photo still to come.
 *
 * It exists for one moment — the reader turning shuffle on or off mid-show. The
 * order comes from the server, so changing it reloads the list from its first
 * page in the new order: photos already played would come round again, and the
 * photo on screen might not even be in the new first page. Keeping the seen ones
 * at the front, and filtering them out of what is still to come, means the
 * reordering applies to the future only — the cursor keeps pointing at the photo
 * the reader is looking at, and nothing repeats before everything else has had
 * its turn.
 *
 * With nothing carried (the ordinary case, and every show that never touches
 * shuffle) it returns the loaded list itself, so the common path costs nothing.
 */
export function playlistOf<T extends Identified>(carried: readonly T[], loaded: T[]): T[] {
  if (carried.length === 0) {
    return loaded
  }
  const seen = new Set(carried.map((item) => item.uid))
  return [...carried, ...loaded.filter((item) => !seen.has(item.uid))]
}

/**
 * The photos seen so far in this pass, extended to include everything up to and
 * including `index`. It never shrinks: stepping back with ← revisits a photo but
 * does not un-see it, and the pass is only over when the show wraps.
 *
 * Returns `seen` itself when it already covers the cursor, so a caller holding it
 * in state can compare identities and skip a pointless update.
 */
export function extendSeen<T extends Identified>(seen: T[], playlist: T[], index: number): T[] {
  const reached = Math.min(index + 1, playlist.length)
  return reached > seen.length ? playlist.slice(0, reached) : seen
}

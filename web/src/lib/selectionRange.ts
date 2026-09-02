/**
 * The Shift+click range of a grid selection: which items lie between the
 * selection anchor and the tile just clicked. A pure function over a list of
 * UIDs — it knows nothing of React, of the catalogue or of which tiles happen
 * to be mounted, which is exactly why a virtualized wall can use it: the grid
 * hands over the order of the photos it has *loaded*, and the range follows
 * that order whether or not the tiles in between are on screen.
 */

/**
 * The contiguous run of UIDs between `anchor` and `target` in `orderedUids`,
 * both ends included and always in grid order, regardless of which of the two
 * comes first. Returns `null` when there is no range to speak of — no anchor
 * yet, or an end that is not in the list (the photo left the grid, or its page
 * is not loaded) — which the caller reads as "treat this as a plain toggle".
 *
 * The range is only ever a set of items to add: deciding what to do with it is
 * the selection's business, not the geometry's.
 */
export function rangeBetween(
  anchor: string | null,
  target: string,
  orderedUids: readonly string[],
): string[] | null {
  if (anchor === null) {
    return null
  }
  const from = orderedUids.indexOf(anchor)
  const to = orderedUids.indexOf(target)
  if (from < 0 || to < 0) {
    return null
  }
  const [start, end] = from <= to ? [from, to] : [to, from]
  return orderedUids.slice(start, end + 1)
}

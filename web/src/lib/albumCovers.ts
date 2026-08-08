import { type AlbumSummary } from '../services/organize'

/**
 * How many photos a collage cover shows — a 2 × 2 grid. Four is the smallest
 * arrangement that still reads as "several pictures" at tile size; nine turns
 * each cell into a thumbnail of a thumbnail.
 */
export const ALBUM_COLLAGE_TILES = 4

/** The album has nothing to show; the tile keeps its empty state. */
export interface NoAlbumCover {
  kind: 'none'
}

/** The tile shows one photo, filling the whole cover. */
export interface SingleAlbumCover {
  kind: 'single'
  /** The photo to render. */
  photoUid: string
}

/** The tile shows {@link ALBUM_COLLAGE_TILES} photos as a 2 × 2 grid. */
export interface CollageAlbumCover {
  kind: 'collage'
  /** Exactly {@link ALBUM_COLLAGE_TILES} distinct photos, in reading order. */
  photoUids: string[]
}

/** What an album tile should draw, as decided by {@link albumCover}. */
export type AlbumCover = NoAlbumCover | SingleAlbumCover | CollageAlbumCover

/** Shared empty set for the unclaimed case, so callers need not allocate one. */
const NOTHING_TAKEN: ReadonlySet<string> = new Set<string>()

/** The photos a cover draws, in order; empty for {@link NoAlbumCover}. */
export function coverPhotoUids(cover: AlbumCover): string[] {
  switch (cover.kind) {
    case 'single':
      return [cover.photoUid]
    case 'collage':
      return cover.photoUids
    case 'none':
      return []
  }
}

/**
 * The photos this album may be drawn with, best first: the server's candidate
 * list (`cover_uids` — newest photo first), or the single effective cover when
 * the list is absent, which keeps the tile working against an older payload or a
 * fixture that only sets `cover_uid`.
 */
function candidates(album: AlbumSummary): string[] {
  const list = album.cover_uids?.filter((uid) => uid !== '')
  if (list !== undefined && list.length > 0) {
    return list
  }
  const single = album.cover_uid
  return single !== undefined && single !== '' ? [single] : []
}

/**
 * Picks the collage's photos, preferring ones no earlier tile has taken and
 * topping the selection up from the album's own order when too few are left.
 *
 * Topping up is deliberate: a fourth album drawn from the same photos as three
 * before it *will* repeat something, and a collage of three pictures and a hole
 * would advertise that far louder than a repeated picture does.
 */
function collagePhotos(list: string[], taken: ReadonlySet<string>): string[] {
  const picked = list.filter((uid) => !taken.has(uid)).slice(0, ALBUM_COLLAGE_TILES)
  if (picked.length === ALBUM_COLLAGE_TILES) {
    return picked
  }
  const rest = list.filter((uid) => !picked.includes(uid))
  return [...picked, ...rest].slice(0, ALBUM_COLLAGE_TILES)
}

/**
 * Decides what one album's tile draws, avoiding the photos in `taken` — the
 * covers the tiles before it already used.
 *
 * The album index is a grid of cards, and a grid of cards only says more than a
 * list while the cards differ. They did not: the cover was each album's newest
 * photo, so four albums holding the same scanned title page all showed that
 * page, and on a phone two identical tiles fit per row. Hence two rules, in this
 * order:
 *
 * 1. An album with enough photos is drawn as a 2 × 2 collage. Four pictures
 *    collide far less often than one — two albums built from the very same
 *    photos can still show eight different ones between them — and the collage
 *    also says at a glance that an album is more than one picture.
 * 2. Whatever it draws, it prefers photos no earlier tile took. A smaller album
 *    degrades to a single image (a collage padded with repeats is worse than an
 *    honest single photo), and that image too steps to the album's next photo
 *    when the first is already on screen.
 *
 * A hand-picked cover overrules both: somebody answered "this is what the album
 * looks like", and a tile that quietly showed something else — because a
 * neighbour got there first — would be overruling a decision with a guess. It is
 * shown alone, whole, and never as one cell of a collage.
 *
 * `taken` is only read, never written; {@link planAlbumCovers} does the claiming.
 */
export function albumCover(
  album: AlbumSummary,
  taken: ReadonlySet<string> = NOTHING_TAKEN,
): AlbumCover {
  const picked = album.cover_photo_uid
  if (picked !== undefined && picked !== '') {
    return { kind: 'single', photoUid: picked }
  }

  const list = candidates(album)
  if (list.length === 0) {
    return { kind: 'none' }
  }
  if (list.length >= ALBUM_COLLAGE_TILES) {
    return { kind: 'collage', photoUids: collagePhotos(list, taken) }
  }
  return { kind: 'single', photoUid: list.find((uid) => !taken.has(uid)) ?? list[0] }
}

/**
 * Plans the cover of every album in the rendered order, keyed by album uid, so
 * neighbouring tiles do not repeat the same photograph. Each album takes the
 * photos it draws out of circulation for the ones after it, which is why the
 * whole list is planned at once rather than tile by tile.
 *
 * Plan the *whole* list the grid was given, not the slice currently on screen:
 * the grid is virtualized, and a plan recomputed per visible window would deal a
 * tile a different cover every time it scrolled back into view. The result is a
 * pure function of the list, so memoizing it against that list is enough to keep
 * a cover still for as long as the album is on the page.
 */
export function planAlbumCovers(albums: AlbumSummary[]): Map<string, AlbumCover> {
  const taken = new Set<string>()
  const covers = new Map<string, AlbumCover>()
  for (const album of albums) {
    const cover = albumCover(album, taken)
    covers.set(album.uid, cover)
    for (const uid of coverPhotoUids(cover)) {
      taken.add(uid)
    }
  }
  return covers
}

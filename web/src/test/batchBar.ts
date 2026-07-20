import { type AlbumCount } from '../services/organize'

/**
 * The batch actions every photo list offers, by accessible name and in bar
 * order. Every page-level test asserts the whole vocabulary rather than a
 * sample: the point of the shared `BatchActionBar` is that no page quietly
 * falls back to a stripped-down toolbar of its own, and only a full list
 * catches that.
 */
export const BATCH_ACTIONS = [
  'Clear selection',
  'Select all',
  'Add to album',
  'Labels',
  'Favorite',
  'Archive',
  'Download ZIP',
  'Stack selected',
  'More edits',
]

/** A minimal album for the bar's add-to-album picker, listed by `fetchAlbums`. */
export function albumOption(uid: string, title: string): AlbumCount {
  return {
    uid,
    slug: title.toLowerCase(),
    title,
    description: '',
    type: 'album',
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: 0,
  }
}

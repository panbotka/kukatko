import { type OrganizeLoadState } from '../../hooks/useUploadOrganize'
import { pendingName } from '../../lib/pendingCreate'

/**
 * The album field's id. It is the label↔input contract of the `MultiSelect` in
 * {@link import('./UploadOrganize').UploadOrganize}, and the upload page reuses
 * it to put the caret back in the picker when the reader jumps there from the
 * queue's sticky header — the field is two components away, and an id already
 * published for accessibility beats threading a ref through.
 */
export const UPLOAD_ALBUMS_FIELD_ID = 'upload-albums'

/**
 * The human names behind an album/label selection, in picking order (albums,
 * then labels), so the choice can be echoed somewhere the picker itself is not —
 * the upload queue's sticky header does exactly that. A pending `create:` marker
 * reads as the typed name, and a value whose catalog entry is missing (or has
 * not loaded yet) falls back to the raw value rather than vanishing: a recap
 * that silently drops a pick would be worse than an ugly one.
 */
export function organizeSelectionNames(
  load: OrganizeLoadState,
  albums: string[],
  labels: string[],
): string[] {
  const names = new Map<string, string>()
  if (load.status === 'ready') {
    for (const album of load.albums) {
      names.set(album.uid, album.title)
    }
    for (const label of load.labels) {
      names.set(label.uid, label.name)
    }
  }
  return [...albums, ...labels].map((value) => pendingName(value) ?? names.get(value) ?? value)
}

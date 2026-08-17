import { type OrganizeLoadState } from '../../hooks/useUploadOrganize'
import { pendingName } from '../../lib/pendingCreate'

/**
 * The album field's id — the label↔input contract of the `MultiSelect` in
 * {@link import('./UploadOrganize').UploadOrganize}, named here so it stays one
 * literal rather than a string repeated across the flow.
 */
export const UPLOAD_ALBUMS_FIELD_ID = 'upload-albums'

/**
 * The human names behind an album/label selection, in picking order (albums,
 * then labels), so the choice can be said in words rather than only shown as
 * chips — the finished batch's outcome sentence ("20 photos uploaded, added to
 * Pouť 2026") is built from exactly this. A pending `create:` marker reads as
 * the typed name, and a value whose catalog entry is missing (or has not loaded
 * yet) falls back to the raw value rather than vanishing: a sentence that
 * silently drops a pick would be worse than an ugly one.
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

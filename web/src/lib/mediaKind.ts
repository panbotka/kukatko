import { type Photo } from '../services/photos'

/**
 * What kind of media item a catalogue row is, asked of the one field that
 * answers it. `media_type` is the backend's own classification (`image`,
 * `video`, `live`), and an absent one means the row predates it and is an image
 * — which is why nothing here reads `file_mime`: a live photo's file is a still
 * (`image/heic`) that nevertheless carries a motion clip, so the MIME type
 * answers a different question than the one anybody is asking.
 */

/**
 * Whether the item is a video clip: a file that is nothing but moving pictures,
 * and the only kind a player is expected to play from beginning to end.
 */
export function isVideo(photo: Photo): boolean {
  return photo.media_type === 'video'
}

/**
 * Whether the item carries a clip at all — a video, or a live photo's two
 * seconds of motion beside its still. This is what the grid tile's play mark
 * means: "there is something here to play", not "this is a film".
 */
export function isPlayableClip(photo: Photo): boolean {
  return isVideo(photo) || photo.media_type === 'live'
}

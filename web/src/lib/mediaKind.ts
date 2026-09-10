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

/**
 * Which noun a count of catalogue items may honestly use: the stills-only
 * `photos`, the clips-only `videos`, or `mixed` for a set holding both.
 *
 * The library is a photo library and its counts say so, which is right until the
 * seven things being counted are seven films. This is the whole judgement behind
 * that: a set with no clips in it reads exactly as it always has, a set that is
 * nothing but clips is called what it is, and a set holding both names both
 * rather than picking the majority and hoping.
 *
 * `videos` counts standalone clips only. A live photo counts with the stills —
 * it is a photograph that carries two seconds of motion, and calling a shelf of
 * them "videos" would swap one wrong word for another.
 */
export function mediaCountKind(total: number, videos: number): 'photos' | 'videos' | 'mixed' {
  if (videos <= 0) {
    return 'photos'
  }
  if (videos >= total) {
    return 'videos'
  }
  return 'mixed'
}

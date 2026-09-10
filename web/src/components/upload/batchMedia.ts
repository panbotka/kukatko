import { type NamedFile, previewKind } from '../../lib/mediaFiles'
import { mediaCountKind } from '../../lib/mediaKind'

/**
 * What a batch of picked files is made of: how many stills, how many clips, and
 * which noun a sentence about it may honestly use.
 */
export interface BatchMedia {
  /** Files that are not video containers — stills, including HEIC and RAW. */
  photos: number
  /** Video containers. */
  videos: number
  /** Which wording the batch takes (see `lib/mediaKind` `mediaCountKind`). */
  kind: 'photos' | 'videos' | 'mixed'
}

/**
 * Splits picked files into stills and clips, so the upload flow can say what it
 * actually uploaded instead of congratulating you on a photo you never sent.
 *
 * The classification is the client's own and needs no help from the backend: the
 * queue holds the picked `File`, and `lib/mediaFiles` `previewKind` already
 * decides image/video/neither from the extension (with the MIME type as the
 * fallback) — it is what puts a play glyph on a clip's row. Only `video` counts
 * as a clip here; `none` is HEIC/TIFF/RAW, which are photographs a browser
 * merely cannot paint, and lumping them in with films would be the same lie in
 * the other direction.
 *
 * `kind` is the shared judgement of `mediaCountKind`, so the upload page and the
 * library count line reach for the video wording under exactly the same rule. An
 * empty set is `photos`: with nothing counted there is nothing to name, and the
 * caller has no sentence to build anyway.
 */
export function batchMedia(files: readonly NamedFile[]): BatchMedia {
  let videos = 0
  for (const file of files) {
    if (previewKind(file) === 'video') {
      videos += 1
    }
  }
  return {
    photos: files.length - videos,
    videos,
    kind: mediaCountKind(files.length, videos),
  }
}

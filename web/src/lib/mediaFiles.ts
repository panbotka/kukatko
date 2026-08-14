/**
 * The app's vocabulary of "this file is a photo or a video".
 *
 * Two places need it and must agree: the file picker's `accept` hint on the
 * upload page, and the triage of files handed over by the phone's share sheet
 * (see `pwa/shareTarget.ts`), which arrive with whatever type the sending app
 * chose to label them with.
 *
 * The list is deliberately generous. It is never the last word — the backend
 * decides what it can ingest, and drag-and-drop bypasses it entirely — so a
 * missing entry can only hide a file the user meant to add, while a superfluous
 * one costs nothing.
 */

/**
 * Images an `<img>` paints on its own, with no decoder and no server round trip
 * — which is exactly the set the upload queue can preview locally from the
 * picked `File` (see {@link previewKind}).
 */
const BROWSER_IMAGE_EXTENSIONS = ['jpg', 'jpeg', 'png', 'gif', 'webp', 'avif', 'bmp'] as const

/**
 * Images the library ingests but a browser will not display: TIFF, which only
 * Safari paints, and the HEIC/HEIF an iPhone produces, which nothing but Safari
 * decodes. The backend converts them (`internal/imgconvert`); the client cannot.
 */
const DECODED_IMAGE_EXTENSIONS = ['tif', 'tiff', 'heic', 'heif'] as const

/** RAW, by vendor — never previewable in a browser either. */
const RAW_EXTENSIONS = [
  'cr2',
  'cr3',
  'nef',
  'nrw',
  'arw',
  'srf',
  'sr2',
  'dng',
  'raf',
  'orf',
  'rw2',
  'pef',
  'srw',
  '3fr',
  'iiq',
  'x3f',
  'kdc',
  'mrw',
  'mef',
] as const

/** The video containers a phone or a camcorder writes. */
const VIDEO_EXTENSIONS = [
  'mp4',
  'm4v',
  'mov',
  'avi',
  'mkv',
  'webm',
  'mpg',
  'mpeg',
  'mts',
  'm2ts',
  '3gp',
  '3g2',
  'wmv',
  'flv',
] as const

/**
 * Extensions (lower case, no dot) of the media kinds Kukátko ingests: common
 * web/phone images, the HEIC/HEIF an iPhone produces, the RAW formats of the
 * usual camera vendors, and the video containers a phone or a camcorder writes.
 *
 * Extensions matter because MIME types cannot be relied on: a phone gallery
 * hands over `image/*` and `video/*`, but a file manager, a messenger or a
 * cloud-drive share routinely labels the very same photo `application/octet-stream`
 * — and RAW and HEIC are commonly typed as nothing at all.
 */
export const MEDIA_EXTENSIONS = [
  ...BROWSER_IMAGE_EXTENSIONS,
  ...DECODED_IMAGE_EXTENSIONS,
  ...RAW_EXTENSIONS,
  ...VIDEO_EXTENSIONS,
] as const

/** The extensions as a set, for a cheap membership test. */
const EXTENSION_SET: ReadonlySet<string> = new Set<string>(MEDIA_EXTENSIONS)

/**
 * The `accept` attribute for a media file input. `image/*,video/*` is what
 * groups the phone gallery; the explicit extensions are appended because many
 * browsers hide RAW and HEIC from a picker that only asks for `image/*`.
 */
export const PICKER_ACCEPT = [
  'image/*',
  'video/*',
  ...MEDIA_EXTENSIONS.map((extension) => `.${extension}`),
].join(',')

/** The lower-cased extension of a file name, without the dot ('' if it has none). */
export function fileExtension(name: string): string {
  const dot = name.lastIndexOf('.')
  if (dot <= 0 || dot === name.length - 1) {
    return ''
  }
  return name.slice(dot + 1).toLowerCase()
}

/** The minimum of a `File` this module needs, so callers can test it with a literal. */
export interface NamedFile {
  name: string
  type: string
}

/**
 * Reports whether a file looks like a photo or a video: either its MIME type
 * says so, or — for the many senders that do not label files properly — its
 * extension is one Kukátko knows.
 */
export function isMediaFile(file: NamedFile): boolean {
  if (file.type.startsWith('image/') || file.type.startsWith('video/')) {
    return true
  }
  return EXTENSION_SET.has(fileExtension(file.name))
}

/**
 * What a preview of a file can show *before* it is uploaded: `image` when the
 * browser paints it from an object URL, `video` for a clip (whose first frame
 * would need decoding the container, so the queue draws a play glyph instead),
 * and `none` for HEIC/TIFF/RAW — everything only the server's converter opens.
 */
export type PreviewKind = 'image' | 'video' | 'none'

/** Extension → kind, for the two kinds a local preview distinguishes. */
const BROWSER_IMAGE_SET: ReadonlySet<string> = new Set<string>(BROWSER_IMAGE_EXTENSIONS)
const VIDEO_SET: ReadonlySet<string> = new Set<string>(VIDEO_EXTENSIONS)

/** The MIME types an `<img>` paints, for a file whose name carries no extension. */
const BROWSER_IMAGE_TYPES: ReadonlySet<string> = new Set([
  'image/jpeg',
  'image/png',
  'image/gif',
  'image/webp',
  'image/avif',
  'image/bmp',
])

/** The kind a MIME type alone implies, when the file name says nothing. */
function kindFromType(type: string): PreviewKind {
  if (BROWSER_IMAGE_TYPES.has(type)) {
    return 'image'
  }
  return type.startsWith('video/') ? 'video' : 'none'
}

/**
 * Decides how a picked file can be previewed locally. The extension decides
 * first — for the same reason {@link isMediaFile} leans on it, senders label
 * files carelessly — and the MIME type is the fallback for a name without one.
 * Anything unrecognised is `none`: drawing a placeholder is cheap, while an
 * `<img>` pointed at a RAW file only ever ends in a broken-image glyph.
 */
export function previewKind(file: NamedFile): PreviewKind {
  const extension = fileExtension(file.name)
  if (BROWSER_IMAGE_SET.has(extension)) {
    return 'image'
  }
  if (VIDEO_SET.has(extension)) {
    return 'video'
  }
  if (extension !== '') {
    // A known-but-undecodable extension (HEIC, RAW) must not fall through to a
    // MIME type its sender guessed at, e.g. `image/heic` labelled `image/jpeg`.
    return EXTENSION_SET.has(extension) ? 'none' : kindFromType(file.type)
  }
  return kindFromType(file.type)
}

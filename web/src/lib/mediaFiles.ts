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
  // Images the browser itself understands.
  'jpg',
  'jpeg',
  'png',
  'gif',
  'webp',
  'avif',
  'bmp',
  'tif',
  'tiff',
  // Phone-camera images.
  'heic',
  'heif',
  // RAW, by vendor.
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
  // Video containers.
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

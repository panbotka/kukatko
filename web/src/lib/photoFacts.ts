/**
 * Derived, presentation-only facts about a photo's file: the aspect ratio and
 * resolution the app computes from the stored dimensions, plus the small
 * vocabularies (MIME type, EXIF orientation, capture-date source) it maps to human
 * labels. The technical-details card reads a lot from the photo payload; keeping
 * the arithmetic and the vocabularies here — pure, React-free, i18n-free — makes
 * each of them directly unit-testable, and leaves the component to do nothing but
 * lay the values out.
 *
 * The functions that format a number take the active locale, because Czech is the
 * default and writes a decimal comma. The ones that classify a value return a
 * narrow union rather than a translation key, so the caller's `t()` stays
 * type-checked against the resource bundle.
 */

/** True for a finite, strictly positive dimension. */
function isPositive(value: number): boolean {
  return Number.isFinite(value) && value > 0
}

/** Formats a number in the active locale with a fixed number of decimals. */
function formatDecimal(value: number, locale: string, digits: number): string {
  return new Intl.NumberFormat(locale, {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).format(value)
}

/** The greatest common divisor of two positive integers (Euclid). */
function gcd(a: number, b: number): number {
  let x = a
  let y = b
  while (y !== 0) {
    const rest = x % y
    x = y
    y = rest
  }
  return x
}

/**
 * The largest term a reduced ratio may have before it stops being a ratio anyone
 * reads. `16 : 9`, `4 : 3` and `21 : 9` say something; `1001 : 667` says nothing —
 * that one is really "about three by two" and is better shown as a decimal.
 */
const MAX_RATIO_TERM = 32

/**
 * The photo's aspect ratio as a reduced fraction, e.g. `4000×3000` → `"4 : 3"` and
 * `1920×1080` → `"16 : 9"`. A ratio that does not reduce to small terms — a
 * cropped or scanned image whose sides share no useful divisor — falls back to a
 * decimal against 1, e.g. `"1,50 : 1"` (Czech) / `"1.50 : 1"` (English). Returns
 * undefined when either dimension is missing, so the caller renders no row at all.
 */
export function aspectRatio(width: number, height: number, locale: string): string | undefined {
  if (!isPositive(width) || !isPositive(height)) {
    return undefined
  }
  const w = Math.round(width)
  const h = Math.round(height)
  const divisor = gcd(w, h)
  const left = w / divisor
  const right = h / divisor
  if (left <= MAX_RATIO_TERM && right <= MAX_RATIO_TERM) {
    return `${String(left)} : ${String(right)}`
  }
  return `${formatDecimal(w / h, locale, 2)} : 1`
}

/**
 * The photo's resolution in megapixels, to one decimal in the active locale, e.g.
 * `4000×3056` → `"12,2"` (Czech). The unit is the caller's to add — it is a
 * translated label. Returns undefined when either dimension is missing.
 */
export function megapixels(width: number, height: number, locale: string): string | undefined {
  if (!isPositive(width) || !isPositive(height)) {
    return undefined
  }
  return formatDecimal((width * height) / 1_000_000, locale, 1)
}

/**
 * Short format labels for the MIME types the library actually stores. The value
 * type admits undefined because a lookup miss is the normal case — an unlisted
 * type falls back to its subtype rather than to nothing.
 */
const MIME_LABELS: Record<string, string | undefined> = {
  'image/jpeg': 'JPEG',
  'image/png': 'PNG',
  'image/gif': 'GIF',
  'image/webp': 'WebP',
  'image/heic': 'HEIC',
  'image/heif': 'HEIF',
  'image/avif': 'AVIF',
  'image/tiff': 'TIFF',
  'image/x-adobe-dng': 'DNG',
  'image/x-canon-cr2': 'CR2',
  'image/x-nikon-nef': 'NEF',
  'video/mp4': 'MP4',
  'video/quicktime': 'MOV',
  'video/x-matroska': 'MKV',
  'video/webm': 'WebM',
}

/**
 * A MIME type as the short format label a person recognises: `image/jpeg` → `JPEG`,
 * `video/quicktime` → `MOV`. An unlisted type degrades to its upper-cased subtype
 * (`image/jxl` → `JXL`, `image/svg+xml` → `SVG`, vendor `x-` prefix dropped) rather
 * than to nothing, so a format the app has never seen still reads as a format.
 * Returns the empty string for an empty input, which the caller drops.
 */
export function formatMime(mime: string): string {
  const key = mime.trim().toLowerCase()
  if (key === '') {
    return ''
  }
  const known = MIME_LABELS[key]
  if (known !== undefined) {
    return known
  }
  const parts = key.split('/')
  if (parts.length < 2 || parts[1] === '') {
    return mime
  }
  return parts[1].replace(/^x-/, '').split('+')[0].toUpperCase()
}

/** The EXIF orientation values (1–8), the raw tag as the file carries it. */
export const ORIENTATIONS = [1, 2, 3, 4, 5, 6, 7, 8] as const

/** One EXIF orientation value. */
export type Orientation = (typeof ORIENTATIONS)[number]

/**
 * Narrows a stored `file_orientation` to a known EXIF orientation, so the caller
 * can look up its label with a type-checked key. Anything outside 1–8 — a missing
 * tag (0) or a corrupt one — returns undefined and renders no row.
 */
export function orientation(value: number | undefined): Orientation | undefined {
  return ORIENTATIONS.find((known) => known === value)
}

/** Where a photo's capture date came from, mirroring `photos.taken_at_source`. */
export type TakenAtSource = 'exif' | 'filename' | 'manual' | 'unknown'

/** The recognised capture-date sources. */
const TAKEN_AT_SOURCES: readonly TakenAtSource[] = ['exif', 'filename', 'manual', 'unknown']

/**
 * Narrows a stored `taken_at_source` to a known source. An empty value returns
 * undefined (the photo simply has no source recorded, so no row is rendered),
 * while an unrecognised one reads as `unknown` — it is a source, just not one this
 * version of the app knows a name for.
 */
export function takenAtSource(value: string | undefined): TakenAtSource | undefined {
  if (value === undefined || value.trim() === '') {
    return undefined
  }
  const found = TAKEN_AT_SOURCES.find((known) => known === value.trim().toLowerCase())
  return found ?? 'unknown'
}

/**
 * The IPTC keywords, which are stored verbatim as one comma-separated string, split
 * into the individual keywords the card renders as chips. Blank entries and
 * surrounding whitespace are dropped, so `"beach, , sunset "` yields two chips.
 */
export function splitKeywords(value: string | undefined): string[] {
  if (value === undefined) {
    return []
  }
  return value
    .split(',')
    .map((keyword) => keyword.trim())
    .filter((keyword) => keyword !== '')
}

/** How many leading characters of a hash are shown before the ellipsis. */
const HASH_PREFIX = 12

/**
 * A SHA256 shortened to its leading characters for display. The full value is not
 * lost — the caller keeps it in a `title` tooltip and behind a copy action — but a
 * 64-character hex string in a definition list forces the page sideways.
 */
export function shortHash(hash: string): string {
  return hash.length > HASH_PREFIX ? `${hash.slice(0, HASH_PREFIX)}…` : hash
}

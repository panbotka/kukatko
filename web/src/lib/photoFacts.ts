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

/**
 * Codec names that are the same fact as a format label under another spelling, so
 * that a file whose codec column says `jpg` is not announced as "JPEG (JPG)".
 */
const CODEC_ALIASES: Record<string, string | undefined> = {
  jpg: 'jpeg',
  tif: 'tiff',
}

/**
 * The file's format as one line: the MIME label, plus the stored image codec in
 * brackets when it is genuinely a second fact — `image/heic` encoded with HEVC
 * reads "HEIC (HEVC)".
 *
 * The two columns usually say the same thing twice ("Formát: JPEG" over "Kodek
 * obrazu: jpeg"), which is noise in a table a person reads to learn something. A
 * container and its codec do diverge, though — HEIC/HEVC, MOV/H.264 — and losing
 * that would be losing a fact, so the codec survives exactly when it differs.
 * Comparison is case-insensitive and forgives the spellings in {@link CODEC_ALIASES}.
 *
 * Returns the codec alone when the MIME type is empty, and the empty string when
 * neither is known — which the caller drops.
 */
export function fileFormat(mime: string, codec: string | undefined): string {
  const label = formatMime(mime)
  const raw = codec?.trim() ?? ''
  if (raw === '') {
    return label
  }
  const shown = raw.toUpperCase()
  if (label === '') {
    return shown
  }
  const normalized = CODEC_ALIASES[raw.toLowerCase()] ?? raw.toLowerCase()
  return normalized === label.toLowerCase() ? label : `${label} (${shown})`
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

/**
 * The chip list written back into the single comma-separated string the column
 * stores. The separator carries a space, so the value reads like the IPTC keyword
 * lists the importers write ("beach, sunset") and round-trips through
 * {@link splitKeywords} unchanged.
 */
export function joinKeywords(keywords: string[]): string {
  return keywords.join(', ')
}

/**
 * The number of Unicode code points in a string — the same unit Go's
 * `utf8.RuneCountInString` counts, so a length compared here against a backend cap
 * agrees with the backend rune for rune. Spreading a string yields its code points
 * (unlike `.length`, which counts UTF-16 units and so double-counts an astral
 * character); that decomposition is exactly the behaviour the disabled rule warns
 * about and exactly the behaviour we want.
 */
function runeCount(value: string): number {
  // eslint-disable-next-line @typescript-eslint/no-misused-spread -- code points match Go runes; see above
  return [...value].length
}

/**
 * Adds the keywords in `raw` — which may itself be a comma-separated list, so a
 * pasted "beach, sunset" becomes two chips — to `keywords`, returning the new list.
 * Each is trimmed, blanks are dropped, and a keyword already on the photo is not
 * added a second time.
 *
 * `maxRunes` mirrors the backend's cap on the joined string (`creditLimits` in
 * `internal/photoapi`): once the list would exceed it the rest is refused, so the
 * editor stops accepting keywords instead of building a value the PATCH would
 * answer with a 400. Runes, not UTF-16 units, because the backend counts runes.
 */
export function addKeywords(keywords: string[], raw: string, maxRunes: number): string[] {
  const next = [...keywords]
  for (const token of raw.split(',')) {
    const keyword = token.trim()
    if (keyword === '' || next.includes(keyword)) {
      continue
    }
    if (runeCount(joinKeywords([...next, keyword])) > maxRunes) {
      break
    }
    next.push(keyword)
  }
  return next
}

/**
 * Whether two keyword lists hold the same keywords in the same order — what decides
 * whether the editor's chips are still the photo's own, and so whether `keywords`
 * belongs in the PATCH at all.
 */
export function sameKeywords(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((keyword, index) => keyword === b[index])
}

/**
 * The placeholders the importers wrote where the source file said nothing —
 * lower-cased, because they are compared case-insensitively. PhotoPrism stores
 * the literal string `Unknown` in `camera_model`/`lens_model` for a scan that
 * never had a camera, so the value is not empty and a naive row renders an
 * English word in the middle of a Czech table.
 */
const PLACEHOLDER_VALUES: readonly string[] = ['unknown']

/**
 * One stored metadata value as the detail card should show it: trimmed, or
 * undefined when there is nothing to show — the field is empty, or holds one of
 * the importers' placeholders. Undefined is what every row of the technical
 * table treats as "render nothing", so a photo with no camera shows one row
 * fewer rather than the word `Unknown`.
 *
 * @example
 *   metaValue('Canon EOS 5D') // 'Canon EOS 5D'
 *   metaValue('Unknown')      // undefined
 *   metaValue('  ')           // undefined
 */
export function metaValue(value: string | undefined): string | undefined {
  if (value === undefined) {
    return undefined
  }
  const trimmed = value.trim()
  if (trimmed === '' || PLACEHOLDER_VALUES.includes(trimmed.toLowerCase())) {
    return undefined
  }
  return trimmed
}

/** The marker the photo-sorter import appended to an automatic description. */
const AI_MODEL_PREFIX = 'AI_MODEL:'

/** An automatic description split into what it says and what wrote it. */
export interface AiNote {
  /** The description itself, without the model trailer. Empty when there is none. */
  text: string
  /** The model that wrote it, empty when the note does not name one. */
  model: string
}

/**
 * Splits a stored `ai_note` into the description and the model that produced it.
 *
 * Some 2500 photos carry the shape the photo-sorter import wrote: the Czech
 * sentence, a blank line, then `AI_MODEL: gemini-2.5-flash`. Printed verbatim
 * under "Automatický popis" that trailer reads as a bug in the one place where
 * the app tells the user something about the photo by itself — so the caption
 * shows `text` and the technical details show `model`, which is where a fact
 * about the machinery belongs.
 *
 * Only a *trailing* marker line is taken (blank lines around it and all): a note
 * that merely mentions the string mid-sentence is somebody's text and is left
 * exactly as it is. The stored value is never changed — this is a display rule.
 */
export function splitAiNote(note: string | undefined): AiNote {
  if (note === undefined || note === '') {
    return { text: note ?? '', model: '' }
  }
  const lines = note.split('\n')
  let last = lines.length
  while (last > 0 && lines[last - 1].trim() === '') {
    last--
  }
  if (last === 0) {
    return { text: note, model: '' }
  }
  const marker = lines[last - 1].trim()
  if (!marker.toUpperCase().startsWith(AI_MODEL_PREFIX)) {
    return { text: note, model: '' }
  }
  let end = last - 1
  while (end > 0 && lines[end - 1].trim() === '') {
    end--
  }
  return {
    text: lines.slice(0, end).join('\n'),
    model: marker.slice(AI_MODEL_PREFIX.length).trim(),
  }
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

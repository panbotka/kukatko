/**
 * Tolerant latitude/longitude parsing for the metadata location picker.
 *
 * The metadata editor accepts coordinates in the notations mapy.cz understands,
 * so a user can paste whatever they have to hand:
 *
 *  - decimal degrees — `49.1234, 16.5678` (comma **or** whitespace separated,
 *    optional `+`/`-` signs, optional `N`/`S`/`E`/`W` suffixes);
 *  - degrees-minutes-seconds — `49°7'24.2"N 16°34'12.5"E`;
 *  - degrees-decimal-minutes — `49°7.4'N, 16°34.2'E`.
 *
 * Everything here is a pure function (no DOM, no I/O) so it is exhaustively
 * unit-tested; the picker component wires it to the map and the PATCH flow.
 */

/** A decoded coordinate pair in canonical decimal degrees. */
export interface Coordinates {
  lat: number
  lng: number
}

/**
 * Why a coordinate string could not be understood:
 *  - `empty`  — nothing but whitespace;
 *  - `format` — the notation was not recognised (or both parts named the same
 *    axis, e.g. two `N`/`S` hemispheres);
 *  - `range`  — parsed fine but latitude/longitude are out of bounds.
 */
export type CoordinateParseError = 'empty' | 'format' | 'range'

/** Discriminated result of {@link parseCoordinates}. */
export type CoordinateParseResult =
  | { ok: true; value: Coordinates }
  | { ok: false; error: CoordinateParseError }

/** Which axis a component's hemisphere suffix pins it to, if any. */
type Axis = 'lat' | 'lng'

/** A single parsed coordinate component: its signed value plus any axis hint. */
interface Component {
  value: number
  axis: Axis | undefined
}

/**
 * Normalises the various Unicode primes/quotes/degree glyphs people paste into
 * the plain ASCII `°`, `'`, `"` the component grammar expects, and folds a pair
 * of straight single quotes (`''`) into a double quote so seconds written that
 * way still parse.
 */
function normalizeSymbols(input: string): string {
  return input
    .replace(/[′’‘]/g, "'") // ′ ’ ‘  → '
    .replace(/[″”“]/g, '"') // ″ ” “  → "
    .replace(/º/g, '°') // º (ordinal) → ° (degree)
    .replace(/''/g, '"') // '' → "
}

/**
 * A single coordinate component in any supported notation: an optional sign,
 * degrees (possibly decimal), then optional minutes (`'`), optional seconds
 * (`"`) and an optional hemisphere letter — all whitespace-tolerant.
 */
const COMPONENT_RE =
  /^([+-])?\s*(\d+(?:\.\d+)?)\s*°?\s*(?:(\d+(?:\.\d+)?)\s*')?\s*(?:(\d+(?:\.\d+)?)\s*")?\s*([NSEW])?$/i

/**
 * Splits the normalised input into its two coordinate components. A comma is the
 * unambiguous separator; without one we split just after the first hemisphere
 * letter (keeping DMS parts that contain spaces intact), falling back to a plain
 * whitespace split for bare decimal pairs. Returns `null` when no clean two-part
 * split is possible.
 */
function splitComponents(input: string): [string, string] | null {
  const commaIdx = input.indexOf(',')
  if (commaIdx !== -1) {
    // Exactly one comma is expected to act as the separator.
    if (input.includes(',', commaIdx + 1)) {
      return null
    }
    const a = input.slice(0, commaIdx).trim()
    const b = input.slice(commaIdx + 1).trim()
    return a === '' || b === '' ? null : [a, b]
  }

  // A hemisphere letter (not part of a number like `1e2`) ends the first
  // component, so DMS with internal spaces stays whole.
  const hemi = /[NSEW](?!\d)/i.exec(input)
  if (hemi !== null) {
    const a = input.slice(0, hemi.index + 1).trim()
    const b = input.slice(hemi.index + 1).trim()
    return a === '' || b === '' ? null : [a, b]
  }

  const tokens = input.split(/\s+/).filter((token) => token !== '')
  return tokens.length === 2 ? [tokens[0], tokens[1]] : null
}

/**
 * Parses one coordinate component into a signed decimal degree plus, when a
 * hemisphere letter is present, the axis it belongs to. Returns `null` if the
 * component does not match the grammar or a minute/second field is ≥ 60.
 */
function parseComponent(raw: string): Component | null {
  const match = COMPONENT_RE.exec(raw.trim())
  if (match === null) {
    return null
  }
  // TS types every capture group as `string`, but optional groups are actually
  // `undefined` at runtime; widen so the presence checks below are honest.
  const [, sign, degStr, minStr, secStr, hemiRaw] = match as unknown as (string | undefined)[]
  const minutes = minStr !== undefined ? Number(minStr) : 0
  const seconds = secStr !== undefined ? Number(secStr) : 0
  if (minutes >= 60 || seconds >= 60) {
    return null
  }

  let value = Number(degStr) + minutes / 60 + seconds / 3600
  const hemi = hemiRaw?.toUpperCase()
  // A hemisphere letter is authoritative for the sign; otherwise honour the
  // leading +/-.
  if (hemi === 'S' || hemi === 'W' || (hemi === undefined && sign === '-')) {
    value = -value
  }
  const axis: Axis | undefined =
    hemi === 'N' || hemi === 'S' ? 'lat' : hemi === 'E' || hemi === 'W' ? 'lng' : undefined
  return { value, axis }
}

/**
 * Parses a free-form coordinate string in any of the supported notations into
 * canonical decimal degrees, or reports why it could not. Components may appear
 * in either order when hemisphere letters disambiguate them; otherwise the first
 * component is taken as latitude.
 */
export function parseCoordinates(input: string): CoordinateParseResult {
  const normalized = normalizeSymbols(input).trim()
  if (normalized === '') {
    return { ok: false, error: 'empty' }
  }

  const parts = splitComponents(normalized)
  if (parts === null) {
    return { ok: false, error: 'format' }
  }
  const first = parseComponent(parts[0])
  const second = parseComponent(parts[1])
  if (first === null || second === null) {
    return { ok: false, error: 'format' }
  }

  // Both components naming the same axis is contradictory.
  if (first.axis === second.axis && first.axis !== undefined) {
    return { ok: false, error: 'format' }
  }
  // Swap when the axes say the order is lng,lat rather than the default lat,lng.
  const swap = first.axis === 'lng' || second.axis === 'lat'
  const lat = swap ? second.value : first.value
  const lng = swap ? first.value : second.value

  if (lat < -90 || lat > 90 || lng < -180 || lng > 180) {
    return { ok: false, error: 'range' }
  }
  return { ok: true, value: { lat, lng } }
}

/**
 * Formats a coordinate pair as canonical decimal degrees for the text field
 * after the marker moves, e.g. `49.123400, 16.567800`.
 *
 * This is a display format and it is lossy: six decimals are ~10 cm on the ground,
 * but a stored `16.7083583333333` comes back as `16.708358`. Never round-trip the
 * result into a PATCH for a coordinate the user did not touch — the metadata form
 * therefore omits an unchanged coordinate from its payload entirely.
 */
export function formatCoordinates(coords: Coordinates, precision = 6): string {
  return `${coords.lat.toFixed(precision)}, ${coords.lng.toFixed(precision)}`
}

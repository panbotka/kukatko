/**
 * Frontend knowledge of the search query language (`q=`): the filter keys the
 * backend understands (kept in sync with `internal/query`), key autocomplete
 * for the search box, and the rows the help modal lists.
 *
 * Parsing itself is the backend's job — the frontend sends `q` verbatim. What
 * lives here is the key list (for discoverability) and a token *scanner* that
 * only answers "which known filter keys does this query use", so a control can
 * admit the query already sets it instead of silently disagreeing with the
 * results.
 */

/** Every filter key of the query language, including aliases, alphabetical. */
export const FILTER_KEYS = [
  'added',
  'after',
  'album',
  'alt',
  'archived',
  'before',
  'camera',
  'city',
  'codec',
  'country',
  'day',
  'description',
  'dist',
  'f',
  'face',
  'faces',
  'favorite',
  'filename',
  'flag',
  'geo',
  'hidden',
  'iso',
  'keywords',
  'label',
  'landscape',
  'lens',
  'mm',
  'month',
  'mp',
  'near',
  'notes',
  'panorama',
  'person',
  'portrait',
  'private',
  'rating',
  'square',
  'subject',
  'taken',
  'title',
  'type',
  'uid',
  'year',
] as const

/** Fast membership test for {@link FILTER_KEYS}. */
const KNOWN_KEYS: ReadonlySet<string> = new Set<string>(FILTER_KEYS)

/** One scanned character of a token, and whether it was quoted or escaped. */
interface TokenChar {
  /** The character itself, one UTF-16 code unit (quotes and escapes dropped). */
  r: string
  /** True when the character was quoted or backslash-escaped, so it is literal. */
  lit: boolean
}

/** One whitespace-separated token: its verbatim source and its scanned characters. */
interface ScannedToken {
  /** The token exactly as typed, quotes included. */
  raw: string
  /** The token's characters with their literal flags. */
  chars: TokenChar[]
}

/**
 * Splits an input into whitespace-separated tokens the way `internal/query`'s
 * scanner does: double quotes make their content literal (and are dropped from
 * the characters, kept in `raw`), a backslash makes the next character literal,
 * and an unterminated quote closes at the end of the input.
 *
 * It walks UTF-16 code units rather than code points. Every character it acts
 * on — the quote, the backslash, the colon, whitespace — is ASCII, so a
 * surrogate half can never be mistaken for one; it only ever lands in a value,
 * which is copied out of `input` verbatim.
 */
function scanTokens(input: string): ScannedToken[] {
  const tokens: ScannedToken[] = []
  let i = 0
  while (i < input.length) {
    if (/\s/.test(input[i])) {
      i++
      continue
    }
    const start = i
    const chars: TokenChar[] = []
    let inQuote = false
    while (i < input.length) {
      const r = input[i]
      if (r === '\\' && i + 1 < input.length) {
        chars.push({ r: input[i + 1], lit: true })
        i += 2
        continue
      }
      if (r === '"') {
        inQuote = !inQuote
        i++
        continue
      }
      if (!inQuote && /\s/.test(r)) {
        break
      }
      chars.push({ r, lit: inQuote })
      i++
    }
    tokens.push({ raw: input.slice(start, i), chars })
  }
  return tokens
}

/**
 * The lowercased filter key of a token, or null when the token is not
 * filter-shaped. Mirrors the backend's rule: the split happens at the first
 * unescaped, unquoted colon, at least one character must precede it, and every
 * character of the key must be an unescaped ASCII letter — so `title:x` is a
 * filter while `12:30`, `-year:1965` and a quoted `"a:b"` are free text.
 */
function tokenKey(chars: TokenChar[]): string | null {
  for (let i = 0; i < chars.length; i++) {
    const c = chars[i]
    if (c.r !== ':' || c.lit) {
      continue
    }
    if (i === 0) {
      return null
    }
    let key = ''
    for (const kc of chars.slice(0, i)) {
      if (kc.lit || !/^[a-zA-Z]$/.test(kc.r)) {
        return null
      }
      key += kc.r
    }
    return key.toLowerCase()
  }
  return null
}

/**
 * Groups a query's *recognised* filter tokens by their lowercased key, each
 * value the token exactly as typed. Only keys the language knows are reported:
 * an unknown key degrades to free text server-side, so it filters nothing and
 * must not be presented as a filter here.
 *
 * This is not a parser — the backend remains the only thing that parses `q`.
 * It exists so the UI can tell when a control's own state has been overtaken by
 * the query (`year:1960-1969` typed into the search box while the Year picker
 * still reads "any year") and say so instead of contradicting itself.
 */
export function queryFilterTokens(input: string): Map<string, string[]> {
  const out = new Map<string, string[]>()
  for (const token of scanTokens(input)) {
    const key = tokenKey(token.chars)
    if (key === null || !KNOWN_KEYS.has(key)) {
      continue
    }
    const existing = out.get(key)
    if (existing === undefined) {
      out.set(key, [token.raw])
    } else {
      existing.push(token.raw)
    }
  }
  return out
}

/**
 * The filter keys behind each facet picker, including aliases. A query using
 * any of them sets that facet without the picker knowing, which is what
 * {@link queryFilterTokens} lets the filter bar flag.
 */
export const FACET_QUERY_KEYS = {
  year: ['year'],
  album: ['album'],
  label: ['label'],
  person: ['person', 'subject'],
} as const

/**
 * The filter tokens of `input` that drive the named facet, joined for display —
 * `''` when the query leaves that facet alone.
 */
export function facetQueryTokens(
  tokens: ReadonlyMap<string, string[]>,
  keys: readonly string[],
): string {
  return keys.flatMap((key) => tokens.get(key) ?? []).join(' ')
}

/** A key-autocomplete proposal for the token being typed. */
export interface KeySuggestion {
  /** The matching filter keys, in alphabetical order. */
  keys: string[]
  /** Index in the input where the token (and thus the replacement) starts. */
  start: number
}

/** Maximum number of keys the autocomplete dropdown offers at once. */
const MAX_KEY_SUGGESTIONS = 8

/**
 * Suggests filter keys for the token currently being typed at the end of the
 * input: when the trailing token is one or more plain letters (no colon or
 * quote yet), the keys sharing that prefix are proposed. Returns null when
 * there is nothing sensible to suggest — mid-value, inside quotes, or an
 * already-completed key.
 */
export function suggestFilterKeys(input: string): KeySuggestion | null {
  // An odd number of quotes means the caret sits inside a quoted value.
  const quotes = input.split('"').length - 1
  if (quotes % 2 === 1) {
    return null
  }
  const start = Math.max(input.lastIndexOf(' '), input.lastIndexOf('\t')) + 1
  const token = input.slice(start)
  if (token === '' || !/^[a-zA-Z]+$/.test(token)) {
    return null
  }
  const prefix = token.toLowerCase()
  const keys = FILTER_KEYS.filter((k) => k.startsWith(prefix) && k !== prefix).slice(
    0,
    MAX_KEY_SUGGESTIONS,
  )
  if (keys.length === 0) {
    return null
  }
  return { keys, start }
}

/**
 * Applies a chosen key suggestion: replaces the trailing token with `key:` so
 * the user continues straight into the value.
 */
export function applyFilterKey(input: string, suggestion: KeySuggestion, key: string): string {
  return input.slice(0, suggestion.start) + key + ':'
}

/**
 * The help rows' ids, as a literal union so `search.help.desc.<id>` stays a
 * valid typed-i18n key (a plain string would widen it to an unknown key).
 */
export type QueryHelpRowId =
  | 'text'
  | 'uid'
  | 'filename'
  | 'keywords'
  | 'album'
  | 'label'
  | 'person'
  | 'state'
  | 'hidden'
  | 'rating'
  | 'flag'
  | 'date'
  | 'takenAdded'
  | 'beforeAfter'
  | 'place'
  | 'geo'
  | 'alt'
  | 'near'
  | 'camera'
  | 'optics'
  | 'type'
  | 'codec'
  | 'orientation'
  | 'faces'

/** One row of the query-language help: related keys, a worked example. */
export interface QueryHelpRow {
  /** i18n suffix under `search.help.desc.` describing the row. */
  id: QueryHelpRowId
  /** The literal filter keys the row documents (not translated). */
  keys: string
  /** A worked example query fragment (not translated). */
  example: string
}

/**
 * The filter rows of the help modal, grouped by concern so the list stays
 * scannable. Descriptions live in i18n under `search.help.desc.<id>`.
 */
export const QUERY_HELP_ROWS: QueryHelpRow[] = [
  { id: 'text', keys: 'title: description: notes:', example: 'title:svatba' },
  { id: 'uid', keys: 'uid:', example: 'uid:ph7lpul2io09bcg2rvp2rljsr6' },
  { id: 'filename', keys: 'filename:', example: 'filename:IMG_*' },
  { id: 'keywords', keys: 'keywords:', example: 'keywords:beach' },
  { id: 'album', keys: 'album:', example: 'album:"Léto 2024"' },
  { id: 'label', keys: 'label:', example: 'label:cat|dog' },
  { id: 'person', keys: 'person: subject:', example: 'person:Anna' },
  { id: 'state', keys: 'favorite: private: archived:', example: 'favorite:yes' },
  { id: 'hidden', keys: 'hidden:', example: 'hidden:yes' },
  { id: 'rating', keys: 'rating:', example: 'rating:4-5' },
  { id: 'flag', keys: 'flag:', example: 'flag:pick' },
  { id: 'date', keys: 'year: month: day:', example: 'year:2020-2023' },
  { id: 'takenAdded', keys: 'taken: added:', example: 'taken:2024-05' },
  { id: 'beforeAfter', keys: 'before: after:', example: 'after:2024-05-01' },
  { id: 'place', keys: 'country: city:', example: 'city:Praha' },
  { id: 'geo', keys: 'geo:', example: 'geo:no' },
  { id: 'alt', keys: 'alt:', example: 'alt:300-500' },
  { id: 'near', keys: 'near: dist:', example: 'near:pht… dist:2' },
  { id: 'camera', keys: 'camera: lens:', example: 'camera:"Canon EOS R6"' },
  { id: 'optics', keys: 'iso: f: mm: mp:', example: 'iso:100-400 f:2.8-4' },
  { id: 'type', keys: 'type:', example: 'type:video' },
  { id: 'codec', keys: 'codec:', example: 'codec:hevc' },
  {
    id: 'orientation',
    keys: 'portrait: landscape: square: panorama:',
    example: 'portrait:yes',
  },
  { id: 'faces', keys: 'faces: face:new', example: 'faces:2' },
]

/** The operator rows' ids, a literal union for the same typed-i18n reason. */
export type QueryHelpOperatorId = 'and' | 'or' | 'not' | 'notText' | 'range' | 'quotes' | 'wildcard'

/** One operator row of the help modal, described under `search.help.op.<id>`. */
export interface QueryHelpOperator {
  /** i18n suffix under `search.help.op.` describing the operator. */
  id: QueryHelpOperatorId
  /** A worked example (not translated). */
  example: string
}

/** The operator rows of the help modal. */
export const QUERY_HELP_OPERATORS: QueryHelpOperator[] = [
  { id: 'and', example: 'iso:100-400 faces:2' },
  { id: 'or', example: 'label:cat|dog' },
  { id: 'not', example: 'label:!blurry' },
  { id: 'notText', example: '-rozmazané' },
  { id: 'range', example: 'iso:800-  iso:-200' },
  { id: 'quotes', example: 'camera:"Canon EOS R6"' },
  { id: 'wildcard', example: 'filename:IMG_*' },
]

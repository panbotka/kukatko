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

import { PERIOD_QUERY_KEYS } from './period'
import { foldText } from './text'

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
  'text',
  'title',
  'type',
  'uid',
  'uploader',
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
  /**
   * Index in the input where this character's source begins — the backslash for
   * an escaped one. Quotes are not characters, so the opening quote of a value
   * lies *before* the index of its first character.
   */
  at: number
}

/** One whitespace-separated token: its verbatim source and its scanned characters. */
interface ScannedToken {
  /** Index in the input where the token begins. */
  start: number
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
        chars.push({ r: input[i + 1], lit: true, at: i })
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
      chars.push({ r, lit: inQuote, at: i })
      i++
    }
    tokens.push({ start, raw: input.slice(start, i), chars })
  }
  return tokens
}

/** A filter-shaped token split at its operator colon. */
interface SplitToken {
  /** The lowercased filter key. */
  key: string
  /** Index in `chars` of the colon that separates key from value. */
  colon: number
}

/**
 * Splits a token at its operator colon, or returns null when the token is not
 * filter-shaped. Mirrors the backend's rule: the split happens at the first
 * unescaped, unquoted colon, at least one character must precede it, and every
 * character of the key must be an unescaped ASCII letter — so `title:x` is a
 * filter while `12:30`, `-year:1965` and a quoted `"a:b"` are free text.
 */
function splitToken(chars: TokenChar[]): SplitToken | null {
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
    return { key: key.toLowerCase(), colon: i }
  }
  return null
}

/**
 * The lowercased filter key of a token, or null when the token is not
 * filter-shaped ({@link splitToken}).
 */
function tokenKey(chars: TokenChar[]): string | null {
  return splitToken(chars)?.key ?? null
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
 * The filter keys behind each control of the filter bar, aliases included. A
 * query using any of them sets that filter without the control knowing, which is
 * what {@link queryFilterTokens} lets the filter bar flag.
 */
export const FACET_QUERY_KEYS = {
  // The period control covers the whole time axis, so every key that scopes it
  // counts — a `before:1950` narrows the grid exactly as a `year:` does.
  period: PERIOD_QUERY_KEYS,
  album: ['album'],
  label: ['label'],
  person: ['person', 'subject'],
  uploader: ['uploader'],
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
 * The facets whose *values* the search box can complete. They are the three
 * key:value filters whose values are names a person actually has to remember —
 * an album title, a label, a person — as opposed to a number, a date or a
 * yes/no, which nothing can usefully propose.
 */
export type ValueFacet = 'album' | 'label' | 'person'

/**
 * Which facet each completable filter key draws its values from. `subject:` is
 * `person:`'s alias in the query language, so it completes from the same list.
 */
const VALUE_FACET_BY_KEY: Readonly<Record<string, ValueFacet | undefined>> = {
  album: 'album',
  label: 'label',
  person: 'person',
  subject: 'person',
}

/** Maximum number of values the autocomplete dropdown offers at once. */
const MAX_VALUE_SUGGESTIONS = 8

/** A value-autocomplete proposal for the `key:value` token being typed. */
export interface ValueSuggestion {
  /** The facet whose names are the candidates. */
  facet: ValueFacet
  /** The value text typed so far; `''` immediately after the colon. */
  prefix: string
  /**
   * Index in the input where the value being typed starts — right after the
   * colon (or after a `|` alternative separator or a `!` negation). Everything
   * from here to the end of the input is replaced by the chosen value, opening
   * quote included, which is what lets the replacement re-quote it correctly.
   */
  start: number
}

/** The plain text of a token's characters, dropping quotes and escapes. */
function charsText(chars: readonly TokenChar[]): string {
  return chars.map((c) => c.r).join('')
}

/**
 * Suggests values for the `key:value` token being typed at the end of the
 * input: `person:an` proposes the people whose name starts with "an",
 * `album:` proposes every album. Returns null whenever the trailing token is
 * not a completable filter — free text, a key that takes no names, or a token
 * the caret has already left (a trailing space).
 *
 * Within the value it honours the query language's own structure: a `|`
 * alternative separator and a leading `!` negation start a fresh value, so
 * `label:cat|do` completes `do` and leaves `cat|` alone. An unterminated quote
 * is *not* a reason to bail out — `album:"Léto 2` is exactly when a title with
 * spaces most needs completing.
 */
export function suggestFilterValues(input: string): ValueSuggestion | null {
  const tokens = scanTokens(input)
  const token = tokens.at(-1)
  // The caret must still be inside the token: a trailing space (outside quotes)
  // ends it, and whatever comes next is a new token, not this value.
  if (token === undefined || token.start + token.raw.length !== input.length) {
    return null
  }
  const split = splitToken(token.chars)
  if (split === null) {
    return null
  }
  const facet = VALUE_FACET_BY_KEY[split.key]
  if (facet === undefined) {
    return null
  }
  return { facet, ...valueBounds(token, split.colon) }
}

/**
 * Where inside `token` the value currently being typed starts, and the text of
 * it so far. `colon` is the index in `token.chars` of the operator colon.
 *
 * The start is expressed as an index into the whole input and always points
 * *after* a one-character delimiter (`:`, `|` or `!`), never at a character of
 * the value: an opening quote is not a character at all, so anchoring on the
 * delimiter is what makes the replacement swallow it.
 */
function valueBounds(token: ScannedToken, colon: number): { prefix: string; start: number } {
  let delimiter = colon
  for (let i = colon + 1; i < token.chars.length; i++) {
    if (token.chars[i].r === '|' && !token.chars[i].lit) {
      delimiter = i
    }
  }
  const next = delimiter + 1
  if (next < token.chars.length && token.chars[next].r === '!' && !token.chars[next].lit) {
    delimiter = next
  }
  return {
    prefix: charsText(token.chars.slice(delimiter + 1)),
    // Every delimiter is an unescaped, unquoted single character, so its source
    // is one character wide.
    start: token.chars[delimiter].at + 1,
  }
}

/** Characters that force a filter value to be quoted to survive the parser. */
const VALUE_NEEDS_QUOTES = /[\s"\\|*]/

/**
 * Renders a value so the query language reads it back verbatim: quoted when it
 * holds whitespace or an operator character (`|` splits alternatives, `*` is a
 * wildcard, a leading `!`/`-` negates), with any quote or backslash inside
 * backslash-escaped. Everything inside double quotes is literal to the parser,
 * so a title such as `Léto | 2024` survives intact.
 */
export function quoteFilterValue(value: string): string {
  const plain =
    value !== '' &&
    !VALUE_NEEDS_QUOTES.test(value) &&
    !value.startsWith('!') &&
    !value.startsWith('-')
  if (plain) {
    return value
  }
  return `"${value.replace(/[\\"]/g, '\\$&')}"`
}

/**
 * Applies a chosen value suggestion: replaces the partially typed value with
 * the properly quoted one and adds a trailing space, so the next token starts
 * clean and the dropdown closes on its own.
 */
export function applyFilterValue(
  input: string,
  suggestion: ValueSuggestion,
  value: string,
): string {
  return input.slice(0, suggestion.start) + quoteFilterValue(value) + ' '
}

/** One completable value: the name a query would carry, and its photo tally. */
export interface FilterValue {
  /** The name exactly as a query must spell it (an album title, a label, a person). */
  name: string
  /** How many photos carry it — what ranks the proposals. */
  count: number
}

/**
 * The values matching a typed prefix, best first: a prefix match after folding
 * both sides (so `namesti` finds `Náměstí` and `anna` finds `Anna`), ranked by
 * photo count and then alphabetically, and capped at
 * {@link MAX_VALUE_SUGGESTIONS}. An empty prefix matches everything, which is
 * what makes a bare `person:` offer the people who appear most.
 *
 * Values that fold to the same name are collapsed: a query matches by name, so
 * two identically titled albums are one and the same proposal.
 */
export function matchFilterValues(
  values: readonly FilterValue[],
  prefix: string,
  limit: number = MAX_VALUE_SUGGESTIONS,
): FilterValue[] {
  const folded = foldText(prefix)
  const matches = values.filter((value) => {
    const name = foldText(value.name)
    return name !== '' && name.startsWith(folded)
  })
  // Sorted before the fold-dedup so the survivor of two identically named values
  // is the one with the higher count.
  matches.sort((a, b) => b.count - a.count || a.name.localeCompare(b.name))

  const seen = new Set<string>()
  const out: FilterValue[] = []
  for (const value of matches) {
    const name = foldText(value.name)
    if (seen.has(name)) {
      continue
    }
    seen.add(name)
    out.push(value)
    if (out.length === limit) {
      break
    }
  }
  return out
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
  | 'photoText'
  | 'album'
  | 'label'
  | 'person'
  | 'uploader'
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
  { id: 'photoText', keys: 'text:', example: 'text:veselice' },
  { id: 'album', keys: 'album:', example: 'album:"Léto 2024"' },
  { id: 'label', keys: 'label:', example: 'label:cat|dog' },
  { id: 'person', keys: 'person: subject:', example: 'person:me' },
  { id: 'uploader', keys: 'uploader:', example: 'uploader:me' },
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

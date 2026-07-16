/**
 * Frontend knowledge of the search query language (`q=`): the filter keys the
 * backend understands (kept in sync with `internal/query`), key autocomplete
 * for the search box, and the rows the help modal lists.
 *
 * Parsing itself is the backend's job — the frontend sends `q` verbatim and
 * only needs the key list for discoverability.
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
  'year',
] as const

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
  | 'filename'
  | 'keywords'
  | 'album'
  | 'label'
  | 'person'
  | 'state'
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
  { id: 'filename', keys: 'filename:', example: 'filename:IMG_*' },
  { id: 'keywords', keys: 'keywords:', example: 'keywords:beach' },
  { id: 'album', keys: 'album:', example: 'album:"Léto 2024"' },
  { id: 'label', keys: 'label:', example: 'label:cat|dog' },
  { id: 'person', keys: 'person: subject:', example: 'person:Anna' },
  { id: 'state', keys: 'favorite: private: archived:', example: 'favorite:yes' },
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

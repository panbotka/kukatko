/**
 * What the command palette should offer for the filter token a reader is
 * currently typing.
 *
 * The palette understands the same `key:value` language as the search page, but
 * nobody remembers fifty keys — so the token being typed is matched against the
 * language itself and the matches are offered as ordinary palette rows. The key
 * list is **not** kept here: it arrives from the server
 * (`GET /search/schema`, minted from the parser's own registry), so a filter
 * added to the parser is offered the moment the server ships it, and one removed
 * stops being offered without anybody editing this file.
 *
 * Only the *trailing* token is ever completed — the one the caret is in after
 * typing — so the filters already standing in front of it are left exactly as
 * they were. Locating that token is {@link keyTokenAt}/{@link valueTokenAt}'s
 * job in `queryLanguage`, shared with the search page's own autocomplete.
 */

import {
  applyFilterKey,
  applyFilterValue,
  keyTokenAt,
  type ValueFacet,
  valueFacetForKey,
  valueTokenAt,
} from './queryLanguage'
import { type QueryFilterKey } from '../services/search'

/** How many suggestions the palette offers at once. */
export const MAX_QUERY_SUGGESTIONS = 8

/** Filter keys matching the bare word being typed (`alb` → `album`). */
export interface KeySuggestions {
  mode: 'keys'
  /** The lowercased letters typed so far. */
  prefix: string
  /** The matching keys, in the schema's order, capped. */
  keys: QueryFilterKey[]
}

/** The words a closed-vocabulary key accepts (`type:` → image/video/live). */
export interface ValueSuggestions {
  mode: 'values'
  /** The filter key being valued. */
  key: string
  /** The value text typed so far. */
  prefix: string
  /** The matching words, capped. */
  values: string[]
}

/**
 * A key whose values are *names* from the library (an album, a label, a person).
 * There is nothing to list here — the names have to be looked up — so this only
 * says which facet to ask for and with what prefix.
 */
export interface NameSuggestions {
  mode: 'names'
  /** The filter key being valued, alias included (`subject:` is `person:`). */
  key: string
  /** Which of the library's name lists the values come from. */
  facet: ValueFacet
  /** The value text typed so far; never blank. */
  prefix: string
}

/** What the palette should offer for the token being typed, if anything. */
export type QuerySuggestions = KeySuggestions | ValueSuggestions | NameSuggestions

/**
 * What to offer for the trailing token of `input`, given the filter keys the
 * server published — or null when there is nothing to offer: free text, a key
 * whose values only the reader knows (a title, a number, a date), or a token the
 * caret has already left.
 *
 * A value token wins over a key token whenever the trailing token carries a
 * colon, which is what makes `type:im` propose `image` rather than re-proposing
 * keys. A name-valued key (`album:`, `label:`, `person:`) is only reported once
 * at least one character has been typed after the colon: the names come from a
 * search, and the whole library is not a suggestion.
 */
export function suggestQuery(
  input: string,
  schema: readonly QueryFilterKey[],
): QuerySuggestions | null {
  const valued = valueTokenAt(input)
  if (valued !== null) {
    const prefix = valued.prefix.toLowerCase()
    const entry = schema.find((candidate) => candidate.key === valued.key)
    if (entry?.values !== undefined) {
      const values = entry.values
        .filter((value) => value.startsWith(prefix))
        .slice(0, MAX_QUERY_SUGGESTIONS)
      return values.length === 0
        ? null
        : { mode: 'values', key: valued.key, prefix: valued.prefix, values }
    }
    const facet = valueFacetForKey(valued.key)
    if (facet !== undefined && valued.prefix.trim() !== '') {
      return { mode: 'names', key: valued.key, facet, prefix: valued.prefix }
    }
    return null
  }

  const typed = keyTokenAt(input)
  if (typed === null) {
    return null
  }
  const keys = schema
    .filter((candidate) => candidate.key.startsWith(typed.prefix))
    .slice(0, MAX_QUERY_SUGGESTIONS)
  return keys.length === 0 ? null : { mode: 'keys', prefix: typed.prefix, keys }
}

/**
 * The input with the half-typed key replaced by `key:`, so the reader carries
 * straight on into the value (and the palette straight on into suggesting one).
 */
export function applySuggestedKey(input: string, key: string): string {
  const token = keyTokenAt(input)
  return token === null ? input : applyFilterKey(input, token, key)
}

/**
 * The input with the half-typed value replaced by the chosen one, quoted if the
 * query language needs it to be, and a trailing space: the filter is finished,
 * so the next thing typed is a new token.
 */
export function applySuggestedValue(input: string, value: string): string {
  const token = valueTokenAt(input)
  return token === null ? input : applyFilterValue(input, token, value)
}

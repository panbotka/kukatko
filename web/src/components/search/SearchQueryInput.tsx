import type { ParseKeys } from 'i18next'
import { useMemo, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'

import { useFilterValues } from '../../hooks/useFilterValues'
import { useSearchHistory } from '../../hooks/useSearchHistory'
import {
  applyFilterKey,
  applyFilterValue,
  type KeySuggestion,
  matchFilterValues,
  suggestFilterKeys,
  suggestFilterValues,
  type ValueSuggestion,
} from '../../lib/queryLanguage'
import { foldText } from '../../lib/text'
import { type SearchHistoryEntry } from '../../services/searchHistory'
import { Icon } from '../Icon'

import { SearchSuggestions, type SuggestionOption } from './SearchSuggestions'

/** Props for {@link SearchQueryInput}. */
export interface SearchQueryInputProps {
  /** Unique id tying the input and its suggestion listbox together. */
  id: string
  /** The current query text (controlled). */
  value: string
  /** Called with the new query text on every change (typed or completed). */
  onChange: (value: string) => void
  /**
   * Called when a row of the dropdown is a whole query to run now — picking a
   * recent search. Without it a recent search is only put into the box, and the
   * surrounding form decides when to run it.
   */
  onRun?: (value: string) => void
  /** Placeholder shown while empty. */
  placeholder?: string
  /** Focus the input on mount (the search page's primary control). */
  autoFocus?: boolean
}

/**
 * How many recent searches a typed prefix may bring along.
 *
 * The empty box offers the whole history — that is all it has to offer — but
 * once something is typed the history shares the dropdown with the completions
 * for it, and a handful of them is what keeps both visible without scrolling.
 */
const HISTORY_MATCH_LIMIT = 5

/** One row of the dropdown: how it is drawn, and what picking it does. */
type Row =
  | { kind: 'history'; option: SuggestionOption; query: string }
  | { kind: 'keys'; option: SuggestionOption; suggestion: KeySuggestion; filterKey: string }
  | { kind: 'values'; option: SuggestionOption; suggestion: ValueSuggestion; value: string }

/** The accessible name of a listbox holding one kind of row. */
const ROW_LABEL_KEY: Record<Row['kind'], ParseKeys> = {
  history: 'search.history.label',
  keys: 'search.keySuggestions',
  values: 'search.valueSuggestions',
}

/**
 * The recent searches worth offering for what is currently typed: the ones that
 * start with it — folded, so `snih` finds `sníh` — minus the one that *is* it,
 * which the reader has evidently already typed. An empty box gets the whole
 * history, since that is the only thing it can be offered.
 */
function matchHistory(entries: readonly SearchHistoryEntry[], value: string): SearchHistoryEntry[] {
  const folded = foldText(value)
  if (folded === '') {
    return [...entries]
  }
  return entries
    .filter((entry) => {
      const query = foldText(entry.query)
      return query !== folded && query.startsWith(folded)
    })
    .slice(0, HISTORY_MATCH_LIMIT)
}

/**
 * The accessible name for a set of rows: the one kind's own name, or the generic
 * one when the panel mixes recent searches with completions. Rows are never
 * empty when the listbox exists — an empty panel draws its "nothing matches"
 * line instead, and only a value token ever produces one.
 */
function panelLabelKey(rows: readonly Row[]): ParseKeys {
  const first = rows.at(0)
  if (first === undefined) {
    return 'search.valueSuggestions'
  }
  return rows.every((row) => row.kind === first.kind)
    ? ROW_LABEL_KEY[first.kind]
    : 'search.suggestions'
}

/**
 * The search box that speaks the query language: a plain text input plus a
 * dropdown offering, depending on what is typed and where the caret is:
 *
 * - **Recent searches**, whenever the box is focused. Empty, they are the whole
 *   history; once something is typed, those of them that start with it — so the
 *   `s` of a reader who searched for `svatba` last week offers `svatba` back,
 *   rather than only the English filter keys `square:` and `subject:`. They come
 *   from the server, so a query composed on one device is offered on the next.
 * - **Filter keys**, while the trailing token could still become one — typing
 *   `ca` offers `camera:`, `city:`, ….
 * - **Filter values**, once a completable key is typed — `person:an` offers the
 *   people whose name starts with "an", diacritics-insensitively, ranked by how
 *   many photos they are on, and inserts the name properly quoted. Only
 *   `album:`, `label:`, `person:`/`subject:` complete: a number or a date has no
 *   list to propose from.
 *
 * Recent searches come first and wear a clock, the completions follow — a query
 * the reader has already run is a stronger proposal than a word they may be in
 * the middle of turning into a filter, and the two are told apart by their glyph
 * as much as by their order. The "forget these" action belongs to a panel that
 * is *only* history: under a list that also completes filters it would read as
 * clearing the wrong thing.
 *
 * ArrowUp/Down move, Tab or Enter accept, Escape closes. Tab is the completion
 * key: it takes the first *completion* untouched, never a recent search, which
 * is a whole query rather than something to complete a word into. Enter with
 * nothing highlighted belongs to the surrounding form, so the box never swallows
 * a search the reader meant to run. Nothing here blocks typing: the value lists
 * are fetched at most once per facet and matched client-side, so no keystroke
 * costs a request, and a facet with nothing to offer still opens the dropdown
 * with a "nothing matches" line rather than vanishing mid-word.
 */
export function SearchQueryInput({
  id,
  value,
  onChange,
  onRun,
  placeholder,
  autoFocus,
}: SearchQueryInputProps) {
  const { t } = useTranslation()
  // Where the reader has moved the highlight, or null while they have not touched
  // it — which is what lets each panel decide its own resting position.
  const [moved, setMoved] = useState<number | null>(null)
  const [dismissed, setDismissed] = useState(false)
  const [focused, setFocused] = useState(false)

  const open = focused && !dismissed
  const empty = value.trim() === ''

  // The three sources, each only consulted when its rows could be on screen. The
  // history is fetched once per focus and matched here, so offering it while the
  // reader types costs no request.
  const history = useSearchHistory(open)
  const keySuggestion = useMemo(() => (empty ? null : suggestFilterKeys(value)), [empty, value])
  const valueSuggestion = useMemo(() => (empty ? null : suggestFilterValues(value)), [empty, value])
  const facetValues = useFilterValues(
    open && valueSuggestion !== null ? valueSuggestion.facet : null,
  )
  const matches = useMemo(
    () => (valueSuggestion === null ? [] : matchFilterValues(facetValues, valueSuggestion.prefix)),
    [facetValues, valueSuggestion],
  )

  const historyMatches = useMemo(
    () => (open ? matchHistory(history.entries, value) : []),
    [open, history.entries, value],
  )

  // The rows, in the order they are offered: what the reader has searched for
  // before, then what the trailing token could still be completed into. Row keys
  // carry their kind, so a remembered query that reads like a filter key cannot
  // collide with the key's own row.
  const rows = useMemo<Row[]>(() => {
    if (!open) {
      return []
    }
    const built: Row[] = historyMatches.map((entry) => ({
      kind: 'history',
      query: entry.query,
      option: { key: `history:${entry.query}`, label: entry.query, icon: 'clock-history' },
    }))
    // Values win over keys whenever both could apply — they cannot, in fact:
    // a completed `key:` rules out a key suggestion and vice versa.
    if (valueSuggestion !== null) {
      for (const match of matches) {
        built.push({
          kind: 'values',
          suggestion: valueSuggestion,
          value: match.name,
          option: { key: `value:${match.name}`, label: match.name, detail: String(match.count) },
        })
      }
    } else if (keySuggestion !== null) {
      for (const key of keySuggestion.keys) {
        built.push({
          kind: 'keys',
          suggestion: keySuggestion,
          filterKey: key,
          option: { key: `key:${key}`, label: `${key}:`, icon: 'funnel' },
        })
      }
    }
    return built
  }, [open, historyMatches, keySuggestion, matches, valueSuggestion])

  // With nothing to offer the dropdown stays away entirely, rather than
  // announcing that there is nothing in it: the search page focuses this box on
  // arrival, so a first-time reader would meet an empty popover over the filters.
  // The one exception is a value token that matched nothing — see below.
  const showEmpty = open && rows.length === 0 && valueSuggestion !== null
  const showPanel = open && (rows.length > 0 || showEmpty)
  // "Forget these" belongs under a list that is nothing but history: below rows
  // that also complete filter keys it would read as clearing those too.
  const historyOnly = rows.length > 0 && rows.every((row) => row.kind === 'history')

  // No panel rests on a row: the highlight exists only once the reader has put it
  // there, which is what keeps Enter theirs. `svatba u` + Enter must search for
  // those two words rather than complete `u` into `uid:` — Czech words and
  // endings prefix these English keys constantly — just as `person:Anna` must not
  // turn into the busier name on top, and Enter on a freshly focused box must not
  // run the newest recent search. Once moved, the index is clamped into the rows,
  // so it can never point past a list that shrank as the reader typed.
  const count = rows.length
  const activeIndex = moved === null || count === 0 ? -1 : Math.min(moved, count - 1)
  const listboxId = `${id}-suggestions`
  const listboxOpen = count > 0

  /** Applies the row at `index`, leaving the caret ready for what comes next. */
  const choose = (index: number) => {
    // `at` counts from the end for a negative index, which no caller can mean:
    // -1 is "nothing highlighted", not "the last row".
    const row = index < 0 ? undefined : rows.at(index)
    if (row === undefined) {
      return
    }
    switch (row.kind) {
      case 'history':
        onChange(row.query)
        // A recent search is a whole query, so picking one runs it — that is what
        // makes the row a shortcut rather than a paste buffer.
        onRun?.(row.query)
        setDismissed(true)
        break
      case 'keys':
        onChange(applyFilterKey(value, row.suggestion, row.filterKey))
        break
      case 'values':
        onChange(applyFilterValue(value, row.suggestion, row.value))
        break
    }
    setMoved(null)
  }

  const onKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (!showPanel) {
      return
    }
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault()
        setMoved(count === 0 ? null : (Math.max(activeIndex, -1) + 1) % count)
        break
      case 'ArrowUp':
        event.preventDefault()
        setMoved(count === 0 ? null : (Math.max(activeIndex, 0) - 1 + count) % count)
        break
      case 'Enter':
        // Nothing highlighted means the reader is submitting their own query, not
        // accepting a proposal: let the form have the key.
        if (activeIndex >= 0) {
          event.preventDefault()
          choose(activeIndex)
        }
        break
      case 'Tab': {
        // Tab completes, so it takes the first row that *is* a completion even
        // when the highlight has not moved — a recent search is a whole query,
        // and running one is not what a reader reaching for Tab asked for. With
        // only recent searches on offer, Tab does what Tab does and leaves the
        // field. A highlight the reader put there is honoured whatever it is on.
        const target =
          activeIndex >= 0 ? activeIndex : rows.findIndex((row) => row.kind !== 'history')
        if (target >= 0) {
          event.preventDefault()
          choose(target)
        }
        break
      }
      case 'Escape':
        event.preventDefault()
        setDismissed(true)
        break
      default:
        break
    }
  }

  return (
    <div className="position-relative">
      <Form.Control
        id={id}
        type="search"
        value={value}
        autoFocus={autoFocus}
        placeholder={placeholder}
        role="combobox"
        aria-autocomplete="list"
        aria-expanded={listboxOpen}
        // Only pointed at a listbox that exists: the panel may be showing a
        // "nothing matches" line instead, and a dangling id is worse than none.
        aria-controls={listboxOpen ? listboxId : undefined}
        aria-activedescendant={
          listboxOpen && activeIndex >= 0 ? `${listboxId}-opt-${activeIndex}` : undefined
        }
        autoComplete="off"
        onChange={(event) => {
          setDismissed(false)
          setMoved(null)
          onChange(event.target.value)
        }}
        onKeyDown={onKeyDown}
        onFocus={() => {
          setFocused(true)
          setMoved(null)
        }}
        onBlur={() => {
          setFocused(false)
        }}
      />
      {showPanel && (
        <SearchSuggestions
          id={listboxId}
          label={t(panelLabelKey(rows))}
          options={rows.map((row) => row.option)}
          activeIndex={activeIndex}
          emptyMessage={valueSuggestion !== null ? t('search.values.empty') : undefined}
          onChoose={choose}
          footer={
            historyOnly ? (
              <>
                <hr className="dropdown-divider" />
                <div className="px-3 pb-1">
                  <Button
                    variant="link"
                    size="sm"
                    className="text-decoration-none p-0"
                    onClick={history.clear}
                  >
                    <Icon name="trash" /> {t('search.history.clear')}
                  </Button>
                </div>
              </>
            ) : undefined
          }
        />
      )}
    </div>
  )
}

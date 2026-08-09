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

/** What the dropdown is currently offering, and what picking a row does. */
type Panel =
  | { kind: 'history'; options: SuggestionOption[] }
  | { kind: 'keys'; options: SuggestionOption[]; suggestion: KeySuggestion }
  | { kind: 'values'; options: SuggestionOption[]; suggestion: ValueSuggestion }

/**
 * Where the highlight starts when a panel opens.
 *
 * Key completion pre-selects its first row: a bare `ca` is not a search anyone
 * means to run, so Enter completing it to `camera:` is the helpful reading. A
 * fully typed value and a recent search are the opposite — `person:Anna` + Enter
 * means "search for that", and Enter on a freshly focused empty box must not run
 * whatever happens to sit at the top of the history. Those panels therefore start
 * with nothing highlighted, and Enter falls through to the form until the reader
 * arrows into the list (Tab still completes the first row).
 */
const INITIAL_ACTIVE: Record<Panel['kind'], number> = { history: -1, keys: 0, values: -1 }

/** The accessible name of each panel's listbox. */
const PANEL_LABEL_KEY: Record<Panel['kind'], ParseKeys> = {
  history: 'search.history.label',
  keys: 'search.keySuggestions',
  values: 'search.valueSuggestions',
}

/**
 * The search box that speaks the query language: a plain text input plus a
 * dropdown that offers, depending on where the caret is, one of three things.
 *
 * - **Recent searches**, while the box is focused and empty. They come from the
 *   server, so a query composed on one device is offered on the next, and the
 *   panel carries its own "forget these" action.
 * - **Filter keys**, while the trailing token could still become one — typing
 *   `ca` offers `camera:`, `city:`, ….
 * - **Filter values**, once a completable key is typed — `person:an` offers the
 *   people whose name starts with "an", diacritics-insensitively, ranked by how
 *   many photos they are on, and inserts the name properly quoted. Only
 *   `album:`, `label:`, `person:`/`subject:` complete: a number or a date has no
 *   list to propose from.
 *
 * ArrowUp/Down move, Enter or Tab accept, Escape closes. Enter with nothing
 * highlighted belongs to the surrounding form, so the box never swallows a search
 * the reader meant to run. Nothing here blocks typing: the value lists are
 * fetched at most once per facet and matched client-side, so no keystroke costs a
 * request, and a facet with nothing to offer still opens the dropdown with a
 * "nothing matches" line rather than vanishing mid-word.
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

  // The three sources, each only consulted when its panel could be on screen.
  const history = useSearchHistory(open && empty)
  const keySuggestion = useMemo(() => (empty ? null : suggestFilterKeys(value)), [empty, value])
  const valueSuggestion = useMemo(() => (empty ? null : suggestFilterValues(value)), [empty, value])
  const facetValues = useFilterValues(
    open && valueSuggestion !== null ? valueSuggestion.facet : null,
  )
  const matches = useMemo(
    () => (valueSuggestion === null ? [] : matchFilterValues(facetValues, valueSuggestion.prefix)),
    [facetValues, valueSuggestion],
  )

  const panel = useMemo<Panel | null>(() => {
    if (!open) {
      return null
    }
    if (empty) {
      // No history means no panel at all, rather than a dropdown announcing that
      // there is nothing in it: the search page focuses this box on arrival, so a
      // first-time reader would meet an empty popover over the filters.
      if (history.entries.length === 0) {
        return null
      }
      return {
        kind: 'history',
        options: history.entries.map((entry) => ({
          key: entry.query,
          label: entry.query,
          icon: 'clock-history',
        })),
      }
    }
    // Values win over keys whenever both could apply — they cannot, in fact:
    // a completed `key:` rules out a key suggestion and vice versa.
    if (valueSuggestion !== null) {
      return {
        kind: 'values',
        suggestion: valueSuggestion,
        options: matches.map((match) => ({
          key: match.name,
          label: match.name,
          detail: String(match.count),
        })),
      }
    }
    if (keySuggestion !== null) {
      return {
        kind: 'keys',
        suggestion: keySuggestion,
        options: keySuggestion.keys.map((key) => ({ key, label: `${key}:` })),
      }
    }
    return null
  }, [open, empty, history.entries, keySuggestion, matches, valueSuggestion])

  // An untouched highlight rests wherever the panel wants it; either way it is
  // clamped into the rows, so it can never point past a list that shrank as the
  // reader typed.
  const count = panel === null ? 0 : panel.options.length
  const resting = panel === null ? -1 : INITIAL_ACTIVE[panel.kind]
  const activeIndex = count === 0 ? -1 : Math.min(moved ?? resting, count - 1)
  const listboxId = `${id}-suggestions`
  const listboxOpen = count > 0

  /** Applies the row at `index`, leaving the caret ready for what comes next. */
  const choose = (index: number) => {
    const option = panel?.options[index]
    if (panel === null || option === undefined) {
      return
    }
    switch (panel.kind) {
      case 'history':
        onChange(option.key)
        // A recent search is a whole query, so picking one runs it — that is what
        // makes the panel a shortcut rather than a paste buffer.
        onRun?.(option.key)
        setDismissed(true)
        break
      case 'keys':
        onChange(applyFilterKey(value, panel.suggestion, option.key))
        break
      case 'values':
        onChange(applyFilterValue(value, panel.suggestion, option.key))
        break
    }
    setMoved(null)
  }

  const onKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (panel === null) {
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
      case 'Tab':
        // Tab is the completion key, so it accepts the first row even when the
        // highlight has not moved.
        if (count > 0) {
          event.preventDefault()
          choose(Math.max(activeIndex, 0))
        }
        break
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
      {panel !== null && (
        <SearchSuggestions
          id={listboxId}
          label={t(PANEL_LABEL_KEY[panel.kind])}
          options={panel.options}
          activeIndex={activeIndex}
          emptyMessage={panel.kind === 'values' ? t('search.values.empty') : undefined}
          onChoose={choose}
          footer={
            panel.kind === 'history' ? (
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

import { type ReactNode } from 'react'

import { Icon, type IconName } from '../Icon'

/** One row the search box's dropdown offers. */
export interface SuggestionOption {
  /** Stable React key — the value the row stands for. */
  key: string
  /** The row's visible text. */
  label: string
  /** A dim trailing detail, such as how many photos carry the value. */
  detail?: string
  /** A leading glyph naming the kind of row this is. */
  icon?: IconName
}

/** Props for {@link SearchSuggestions}. */
export interface SearchSuggestionsProps {
  /** DOM id of the listbox, which the input references via `aria-controls`. */
  id: string
  /** Accessible name of the listbox. */
  label: string
  /** The rows to offer, in display order. */
  options: SuggestionOption[]
  /** Index of the highlighted row, or -1 when none is. */
  activeIndex: number
  /** Shown in place of the rows when there are none. */
  emptyMessage?: string
  /** Called with the index of the row the user picked. */
  onChoose: (index: number) => void
  /** An optional action rendered under the rows (the history's "clear"). */
  footer?: ReactNode
}

/**
 * The dropdown under the search box: a listbox of proposals — recent searches,
 * filter keys or filter values — plus an optional action row.
 *
 * It is deliberately presentational. What to offer, and what picking a row does,
 * belongs to whoever owns the query text; this component only draws the rows,
 * marks the highlighted one and reports clicks. Rows swallow the mousedown so the
 * input never loses focus mid-click, which is what keeps the dropdown from closing
 * out from under the pointer.
 *
 * With nothing to offer it still renders — `emptyMessage` rather than the rows.
 * A box that silently drops its dropdown mid-word reads as broken; one that says
 * "nothing matches" reads as an answer.
 */
export function SearchSuggestions({
  id,
  label,
  options,
  activeIndex,
  emptyMessage,
  onChoose,
  footer,
}: SearchSuggestionsProps) {
  return (
    <div
      className="dropdown-menu show mt-1 shadow overflow-auto w-100"
      style={{ maxHeight: '18rem' }}
      // Keep focus in the input for every press inside the panel, so blur never
      // closes the dropdown before the click lands.
      onMouseDown={(event) => {
        event.preventDefault()
      }}
    >
      {options.length === 0 ? (
        <p className="dropdown-item-text small text-secondary mb-0" role="status">
          {emptyMessage}
        </p>
      ) : (
        <ul id={id} role="listbox" aria-label={label} className="list-unstyled mb-0">
          {options.map((option, index) => (
            <li key={option.key} role="presentation">
              <button
                type="button"
                id={`${id}-opt-${index}`}
                role="option"
                aria-selected={index === activeIndex}
                className={`dropdown-item d-flex align-items-center gap-2${
                  index === activeIndex ? ' active' : ''
                }`}
                onClick={() => {
                  onChoose(index)
                }}
              >
                {option.icon !== undefined && <Icon name={option.icon} />}
                <span className="text-truncate">{option.label}</span>
                {option.detail !== undefined && (
                  <span className="ms-auto small opacity-75">{option.detail}</span>
                )}
              </button>
            </li>
          ))}
        </ul>
      )}
      {footer}
    </div>
  )
}

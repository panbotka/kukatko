import { useEffect, useMemo, useRef, useState } from 'react'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'

import { foldedIncludes } from '../../lib/text'

/** One selectable option in a {@link SearchableSelect}. */
export interface SelectOption {
  /** The value written to the view state; never the empty string. */
  value: string
  /** Human-readable text shown and filtered against. */
  label: string
  /** Optional photo count rendered beside the label. */
  count?: number
}

/** Props for {@link SearchableSelect}. */
export interface SearchableSelectProps {
  /** Unique id tying the label, input and listbox together. */
  id: string
  /** Visible field label (e.g. "Album"). */
  label: string
  /** The selected option's value, or `''` for "any". */
  value: string
  /** The options to choose from, in the order they should be offered. */
  options: SelectOption[]
  /** The "no filter" row's text (e.g. "Any album"), also the empty placeholder. */
  anyLabel: string
  /** Called with the chosen option's value, or `''` when the filter is cleared. */
  onChange: (value: string) => void
}

/** Cap on rendered suggestions so a catalog with thousands of labels stays responsive. */
const MAX_SUGGESTIONS = 50

/**
 * A single-choice facet select the reader can type into, for collections too
 * large for a plain `<select>` — albums and labels both grow without bound. It
 * shows the current choice at rest; focusing it opens the full list, and typing
 * narrows that list case- and accent-insensitively (so `namesti` finds `Náměstí`,
 * matching the backend's `immutable_unaccent`).
 *
 * A leading "any" row clears the facet, so the filter is removable from inside the
 * control as well as from its chip. Choosing an option — by click or keyboard
 * (Up/Down to move, Enter to select, Esc to close) — calls `onChange` with its
 * value; the control never creates options. Built on react-bootstrap primitives
 * with combobox/listbox ARIA roles and ~44px tap targets, mirroring
 * {@link import('../photo/AddAutocomplete').AddAutocomplete}.
 */
export function SearchableSelect({
  id,
  label,
  value,
  options,
  anyLabel,
  onChange,
}: SearchableSelectProps) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')
  const [open, setOpen] = useState(false)
  const [activeIndex, setActiveIndex] = useState(-1)
  const containerRef = useRef<HTMLDivElement>(null)

  const listboxId = `${id}-listbox`
  const selected = options.find((option) => option.value === value)

  // While the menu is open the input is a query field, emptied on open so the
  // reader types straight into it; the current choice stays legible as the
  // placeholder and is marked in the list. At rest the input shows that choice. A
  // value the options no longer carry (a deleted album still in the URL) falls
  // back to its raw value, so the filter never becomes invisible.
  const displayed = value === '' ? '' : (selected?.label ?? value)
  const text = open ? query : displayed

  const suggestions = useMemo(
    () => options.filter((option) => foldedIncludes(option.label, query)).slice(0, MAX_SUGGESTIONS),
    [options, query],
  )

  // Reset the keyboard highlight whenever the offered set changes.
  useEffect(() => {
    setActiveIndex(-1)
  }, [suggestions])

  function choose(next: string) {
    setQuery('')
    setOpen(false)
    setActiveIndex(-1)
    onChange(next)
  }

  function close() {
    setQuery('')
    setOpen(false)
    setActiveIndex(-1)
  }

  function handleKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault()
        setOpen(true)
        setActiveIndex((i) => Math.min(i + 1, suggestions.length - 1))
        break
      case 'ArrowUp':
        event.preventDefault()
        setActiveIndex((i) => Math.max(i - 1, -1))
        break
      case 'Enter': {
        event.preventDefault()
        if (activeIndex >= 0 && activeIndex < suggestions.length) {
          choose(suggestions[activeIndex].value)
        } else if (query.trim() !== '' && suggestions.length > 0) {
          // Nothing highlighted but something typed: take the best match.
          choose(suggestions[0].value)
        } else {
          // An empty, unhighlighted field points at the "any" row.
          choose('')
        }
        break
      }
      case 'Escape':
        close()
        break
      default:
        break
    }
  }

  return (
    <div
      ref={containerRef}
      className="position-relative"
      onBlur={(event) => {
        // Close only when focus leaves the whole widget (not on inner moves).
        if (!containerRef.current?.contains(event.relatedTarget)) {
          close()
        }
      }}
    >
      <Form.Label htmlFor={id} className="small mb-1">
        {label}
      </Form.Label>
      <Form.Control
        id={id}
        type="text"
        className="kukatko-tap-target"
        value={text}
        placeholder={open && displayed !== '' ? displayed : anyLabel}
        role="combobox"
        aria-expanded={open}
        aria-controls={listboxId}
        aria-autocomplete="list"
        autoComplete="off"
        onFocus={() => {
          setOpen(true)
        }}
        onKeyDown={handleKeyDown}
        onChange={(event) => {
          setQuery(event.target.value)
          setOpen(true)
        }}
      />

      {open && (
        <ul
          id={listboxId}
          role="listbox"
          aria-label={label}
          className="dropdown-menu show w-100 mt-1 shadow overflow-auto"
          style={{ top: '100%', maxHeight: '50vh' }}
        >
          <li>
            <button
              type="button"
              role="option"
              aria-selected={value === ''}
              className={`dropdown-item kukatko-tap-target d-flex align-items-center ${
                activeIndex === -1 ? 'active' : ''
              }`}
              onMouseDown={(event) => {
                event.preventDefault()
              }}
              onClick={() => {
                choose('')
              }}
            >
              {anyLabel}
            </button>
          </li>
          {suggestions.length === 0 && query !== '' && (
            <li className="dropdown-item-text text-secondary small">
              {t('photo.organize.noMatch')}
            </li>
          )}
          {suggestions.map((option, index) => (
            <li key={option.value}>
              <button
                type="button"
                role="option"
                aria-selected={option.value === value}
                className={`dropdown-item kukatko-tap-target d-flex align-items-center justify-content-between gap-2 ${
                  index === activeIndex ? 'active' : ''
                }`}
                // Keep the input focused so the blur handler does not close the
                // menu before the click lands.
                onMouseDown={(event) => {
                  event.preventDefault()
                }}
                onClick={() => {
                  choose(option.value)
                }}
              >
                <span className="text-truncate">{option.label}</span>
                {option.count !== undefined && (
                  <span className="text-secondary small flex-shrink-0">{option.count}</span>
                )}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

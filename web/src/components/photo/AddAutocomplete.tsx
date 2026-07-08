import { useEffect, useMemo, useRef, useState } from 'react'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'

import { foldedIncludes } from '../../lib/text'

/** One selectable option in an {@link AddAutocomplete}. */
export interface AutocompleteOption {
  /** Stable identifier passed back to {@link AddAutocompleteProps.onAdd}. */
  uid: string
  /** Human-readable text shown and filtered against. */
  label: string
}

/** Props for {@link AddAutocomplete}. */
export interface AddAutocompleteProps {
  /** The options the user may pick from (already excluding current members). */
  options: AutocompleteOption[]
  /** Called with the chosen option's uid; the input clears afterwards. */
  onAdd: (uid: string) => void
  /** Accessible name / placeholder for the field (e.g. "Add to album"). */
  label: string
  /** Unique id tying the visually-hidden label, input and listbox together. */
  id: string
  /** Disables the field while a mutation is in flight. */
  disabled?: boolean
}

/** Cap on rendered suggestions so hundreds of albums/labels stay responsive. */
const MAX_SUGGESTIONS = 50

/**
 * A type-to-filter autocomplete for adding the photo to an album or attaching a
 * label. As the user types, options whose label matches the query
 * (case- and accent-insensitively) appear in a dropdown; choosing one — by click
 * or keyboard (Up/Down to move, Enter to select, Esc to close) — calls
 * {@link AddAutocompleteProps.onAdd} and clears the input. A "nothing matches"
 * row is shown when a non-empty query filters everything out; the field never
 * creates new albums/labels. Built on react-bootstrap primitives (no extra
 * dependency) with combobox/listbox ARIA roles and ~44px tap targets.
 */
export function AddAutocomplete({ options, onAdd, label, id, disabled }: AddAutocompleteProps) {
  const { t } = useTranslation()
  const [text, setText] = useState('')
  const [open, setOpen] = useState(false)
  const [activeIndex, setActiveIndex] = useState(-1)
  const containerRef = useRef<HTMLDivElement>(null)

  const listboxId = `${id}-listbox`
  const trimmed = text.trim()

  const suggestions = useMemo(
    () => options.filter((option) => foldedIncludes(option.label, text)).slice(0, MAX_SUGGESTIONS),
    [options, text],
  )

  // A dropdown is only shown once the user has typed something.
  const showDropdown = open && trimmed !== ''

  // Reset the keyboard highlight whenever the filtered set changes.
  useEffect(() => {
    setActiveIndex(-1)
  }, [suggestions])

  function select(option: AutocompleteOption) {
    setText('')
    setOpen(false)
    setActiveIndex(-1)
    onAdd(option.uid)
  }

  function handleKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
    switch (event.key) {
      case 'ArrowDown':
        if (suggestions.length > 0) {
          event.preventDefault()
          setOpen(true)
          setActiveIndex((i) => Math.min(i + 1, suggestions.length - 1))
        }
        break
      case 'ArrowUp':
        if (suggestions.length > 0) {
          event.preventDefault()
          setActiveIndex((i) => Math.max(i - 1, -1))
        }
        break
      case 'Enter': {
        const option = activeIndex >= 0 ? suggestions[activeIndex] : undefined
        if (option) {
          event.preventDefault()
          select(option)
        }
        break
      }
      case 'Escape':
        setOpen(false)
        setActiveIndex(-1)
        break
      default:
        break
    }
  }

  return (
    <div
      ref={containerRef}
      className="position-relative mb-3"
      onBlur={(event) => {
        // Close only when focus leaves the whole widget (not on inner moves).
        if (!containerRef.current?.contains(event.relatedTarget)) {
          setOpen(false)
        }
      }}
    >
      <Form.Label htmlFor={id} className="visually-hidden">
        {label}
      </Form.Label>
      <Form.Control
        id={id}
        type="text"
        className="kukatko-tap-target"
        value={text}
        placeholder={label}
        role="combobox"
        aria-expanded={showDropdown}
        aria-controls={listboxId}
        aria-autocomplete="list"
        autoComplete="off"
        disabled={disabled}
        onFocus={() => {
          setOpen(true)
        }}
        onKeyDown={handleKeyDown}
        onChange={(event) => {
          setText(event.target.value)
          setOpen(true)
        }}
      />

      {showDropdown && (
        <ul
          id={listboxId}
          role="listbox"
          aria-label={label}
          className="dropdown-menu show w-100 mt-1 shadow overflow-auto"
          style={{ top: '100%', maxHeight: '50vh' }}
        >
          {suggestions.length === 0 && (
            <li className="dropdown-item-text text-secondary small">
              {t('photo.organize.noMatch')}
            </li>
          )}
          {suggestions.map((option, index) => (
            <li key={option.uid}>
              <button
                type="button"
                role="option"
                aria-selected={index === activeIndex}
                className={`dropdown-item text-truncate kukatko-tap-target d-flex align-items-center ${
                  index === activeIndex ? 'active' : ''
                }`}
                // Keep the input focused so the blur handler does not close the
                // menu before the click lands.
                onMouseDown={(event) => {
                  event.preventDefault()
                }}
                onClick={() => {
                  select(option)
                }}
              >
                {option.label}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

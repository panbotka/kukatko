import { useEffect, useMemo, useRef, useState } from 'react'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'

import { foldedEquals, foldedIncludes } from '../../lib/text'

/** One selectable option in an {@link AddAutocomplete}. */
export interface AutocompleteOption {
  /** Stable identifier passed back to {@link AddAutocompleteProps.onAdd}. */
  uid: string
  /** Human-readable text shown and filtered against. */
  label: string
  /**
   * Secondary text shown muted at the end of the row (e.g. how many photos a
   * person appears on). Not filtered against — the query only ever matches the
   * label, so a hint can carry numbers without hijacking the search.
   */
  hint?: string
}

/** Props for {@link AddAutocomplete}. */
export interface AddAutocompleteProps {
  /** The options the user may pick from (already excluding current members). */
  options: AutocompleteOption[]
  /** Called with the chosen option's uid; the input clears afterwards. */
  onAdd: (uid: string) => void
  /**
   * When set, a query matching no existing option offers to create an entry of
   * that name. Resolve to `true` once it exists and is attached (the input then
   * clears), or `false` when the mutation failed — the typed text is kept so the
   * user can retry. Leave unset for a pick-only field.
   */
  onCreate?: (name: string) => Promise<boolean>
  /** Accessible name / placeholder for the field (e.g. "Add to album"). */
  label: string
  /** Unique id tying the visually-hidden label, input and listbox together. */
  id: string
  /** Disables the field while a mutation is in flight. */
  disabled?: boolean
  /**
   * Focuses the field on mount. Set it where the field IS the task (naming the
   * selected face), not where it is one control among many.
   */
  autoFocus?: boolean
}

/** Cap on rendered suggestions so hundreds of albums/labels stay responsive. */
const MAX_SUGGESTIONS = 50

/**
 * A type-to-filter autocomplete for adding the photo to an album or attaching a
 * label. As the user types, options whose label matches the query
 * (case- and accent-insensitively) appear in a dropdown; choosing one — by click
 * or keyboard (Up/Down to move, Enter to select, Esc to close) — calls
 * {@link AddAutocompleteProps.onAdd} and clears the input.
 *
 * With {@link AddAutocompleteProps.onCreate} set, a query that names no existing
 * option gets a trailing "create «query»" row, so a photo can be given a label
 * that does not exist yet — including the very first one, when the option list
 * is empty. Without it the field only picks, and a non-empty query that filters
 * everything out shows a "nothing matches" row instead.
 *
 * Built on react-bootstrap primitives (no extra dependency) with
 * combobox/listbox ARIA roles and ~44px tap targets.
 */
export function AddAutocomplete({
  options,
  onAdd,
  onCreate,
  label,
  id,
  disabled,
  autoFocus = false,
}: AddAutocompleteProps) {
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

  // Creating is offered only for a name no option already carries; the create
  // row sits after the suggestions, so its index is `suggestions.length`.
  const canCreate =
    onCreate !== undefined &&
    trimmed !== '' &&
    !options.some((option) => foldedEquals(option.label, trimmed))
  const rowCount = suggestions.length + (canCreate ? 1 : 0)

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

  async function create() {
    if (onCreate === undefined || !(await onCreate(trimmed))) {
      return
    }
    setText('')
    setOpen(false)
    setActiveIndex(-1)
  }

  function handleKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
    switch (event.key) {
      case 'ArrowDown':
        if (rowCount > 0) {
          event.preventDefault()
          setOpen(true)
          setActiveIndex((i) => Math.min(i + 1, rowCount - 1))
        }
        break
      case 'ArrowUp':
        if (rowCount > 0) {
          event.preventDefault()
          setActiveIndex((i) => Math.max(i - 1, -1))
        }
        break
      case 'Enter': {
        // Nothing highlighted: Enter still creates when that is the only row,
        // so typing a brand-new name and confirming it just works.
        const creating =
          canCreate &&
          (activeIndex === suggestions.length || (activeIndex === -1 && rowCount === 1))
        if (creating) {
          event.preventDefault()
          void create()
          break
        }
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
        // Opt-in, and only ever set where the field is the whole purpose of the
        // panel that just opened (naming the selected face).
        autoFocus={autoFocus}
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
          {rowCount === 0 && (
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
                <span className="text-truncate">{option.label}</span>
                {option.hint !== undefined && (
                  <span className="ms-auto ps-2 small text-secondary">{option.hint}</span>
                )}
              </button>
            </li>
          ))}
          {canCreate && (
            <li>
              <button
                type="button"
                role="option"
                aria-selected={activeIndex === suggestions.length}
                // Guards against a second create while the first is in flight.
                disabled={disabled}
                className={`dropdown-item text-truncate kukatko-tap-target d-flex align-items-center ${
                  activeIndex === suggestions.length ? 'active' : ''
                }`}
                onMouseDown={(event) => {
                  event.preventDefault()
                }}
                onClick={() => {
                  void create()
                }}
              >
                {t('photo.organize.createOption', { name: trimmed })}
              </button>
            </li>
          )}
        </ul>
      )}
    </div>
  )
}

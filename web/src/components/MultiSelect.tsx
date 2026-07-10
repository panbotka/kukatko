import { useEffect, useMemo, useRef, useState } from 'react'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'

import { foldedEquals, foldedIncludes } from '../lib/text'

/** One selectable option in a {@link MultiSelect}. */
export interface MultiSelectOption {
  /** Stable identifier written to the selection; never the empty string. */
  value: string
  /** Human-readable text shown and filtered against. */
  label: string
  /** Optional photo count rendered beside the label. */
  count?: number
}

/** Props for {@link MultiSelect}. */
export interface MultiSelectProps {
  /** Unique id tying the label, input and listbox together. */
  id: string
  /** Visible field label (e.g. "Add to albums"). */
  label: string
  /** The options to choose from, in the order they should be offered. */
  options: MultiSelectOption[]
  /** The currently chosen values, in the order they were picked. */
  selected: string[]
  /** Called with the next selection whenever a value is added or removed. */
  onChange: (values: string[]) => void
  /** Placeholder / hint shown in the empty query field (e.g. "Type to filter…"). */
  placeholder: string
  /** Disables the input and the chip remove buttons while a mutation is in flight. */
  disabled?: boolean
  /**
   * Marks the field as destructive (removing from an album, dropping a label).
   * Chips are painted in the danger key so a removal never reads like an addition.
   */
  destructive?: boolean
  /**
   * When set, a non-empty query that names no existing option — compared case-
   * and accent-insensitively and ignoring surrounding whitespace, so `dovolena `
   * never duplicates `Dovolená` — appends a trailing "Create «name»" entry to
   * the list. Choosing it calls this with the trimmed name; the caller decides
   * what creation means (typically it registers the name and selects a value for
   * it via {@link MultiSelectProps.options} and {@link MultiSelectProps.selected}).
   * Leave unset for a pick-only field, e.g. for readers without write access.
   */
  onCreate?: (name: string) => void
}

/** Cap on rendered suggestions so a catalog with thousands of labels stays responsive. */
const MAX_SUGGESTIONS = 50

/**
 * A type-to-filter multi-select for collections that grow without bound — albums
 * and labels both do. Typing narrows the option list case- and accent-insensitively
 * (so `namesti` finds `Náměstí`, matching the backend's `immutable_unaccent`), and
 * every pick is added to the selection rather than replacing it, so one bulk apply
 * can touch several albums and several labels at once.
 *
 * Chosen options leave the list and reappear below the field as removable chips:
 * the list then only ever offers what is *not* yet chosen, which keeps a long list
 * short and makes the current selection readable at a glance without a checkmark
 * column. Keyboard: Up/Down to move, Enter to take the highlighted (or, with a
 * query typed, the best) match, Backspace on an empty field to drop the last chip,
 * Esc to close.
 *
 * Built on react-bootstrap primitives (no extra dependency) with combobox/listbox
 * ARIA roles and ~44px tap targets, mirroring
 * {@link import('./photo/AddAutocomplete').AddAutocomplete} and
 * {@link import('./library/SearchableSelect').SearchableSelect}. By default it
 * only picks from the options it is given; with
 * {@link MultiSelectProps.onCreate} set, a query that names no existing option
 * also offers to create an entry of that name.
 */
export function MultiSelect({
  id,
  label,
  options,
  selected,
  onChange,
  placeholder,
  disabled,
  destructive,
  onCreate,
}: MultiSelectProps) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')
  const [open, setOpen] = useState(false)
  const [activeIndex, setActiveIndex] = useState(-1)
  const containerRef = useRef<HTMLDivElement>(null)

  const listboxId = `${id}-listbox`

  // A value whose option has vanished (an album deleted while the modal is open)
  // still gets a chip, labelled by its raw value, so the selection never lies.
  const chips = useMemo(
    () =>
      selected.map(
        (value) => options.find((option) => option.value === value) ?? { value, label: value },
      ),
    [options, selected],
  )

  const suggestions = useMemo(
    () =>
      options
        .filter((option) => !selected.includes(option.value))
        .filter((option) => foldedIncludes(option.label, query))
        .slice(0, MAX_SUGGESTIONS),
    [options, selected, query],
  )

  // Creating is offered only for a name no option already carries — checked
  // against every option, selected ones included, so a name that differs only
  // by case, accents or surrounding whitespace never becomes a duplicate. The
  // create row sits after the suggestions, so its index is `suggestions.length`.
  const trimmed = query.trim()
  const canCreate =
    onCreate !== undefined &&
    trimmed !== '' &&
    !options.some((option) => foldedEquals(option.label, trimmed))
  const rowCount = suggestions.length + (canCreate ? 1 : 0)

  // Reset the keyboard highlight whenever the offered set changes.
  useEffect(() => {
    setActiveIndex(-1)
  }, [suggestions])

  function add(value: string) {
    setQuery('')
    setActiveIndex(-1)
    if (!selected.includes(value)) {
      onChange([...selected, value])
    }
  }

  function create() {
    if (onCreate === undefined) {
      return
    }
    setQuery('')
    setActiveIndex(-1)
    onCreate(trimmed)
  }

  function remove(value: string) {
    onChange(selected.filter((current) => current !== value))
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
        setActiveIndex((i) => Math.min(i + 1, rowCount - 1))
        break
      case 'ArrowUp':
        event.preventDefault()
        setActiveIndex((i) => Math.max(i - 1, -1))
        break
      case 'Enter': {
        // Never submit the surrounding form from this field.
        event.preventDefault()
        if (canCreate && activeIndex === suggestions.length) {
          create()
        } else if (activeIndex >= 0 && activeIndex < suggestions.length) {
          add(suggestions[activeIndex].value)
        } else if (trimmed !== '' && suggestions.length > 0) {
          // Nothing highlighted but something typed: take the best match.
          add(suggestions[0].value)
        } else if (canCreate) {
          // A brand-new name that matches nothing: Enter confirms creating it.
          create()
        }
        break
      }
      case 'Backspace':
        // Only when there is nothing to delete in the query itself — otherwise
        // this would eat the character the reader meant to erase.
        if (query === '' && selected.length > 0) {
          event.preventDefault()
          remove(selected[selected.length - 1])
        }
        break
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
      <Form.Label
        htmlFor={id}
        className={`kk-text-caption mb-1 ${destructive === true ? 'text-danger' : ''}`}
      >
        {label}
      </Form.Label>
      <Form.Control
        id={id}
        type="text"
        className="kukatko-tap-target"
        value={query}
        placeholder={placeholder}
        role="combobox"
        aria-expanded={open}
        aria-controls={listboxId}
        aria-autocomplete="list"
        autoComplete="off"
        disabled={disabled}
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
          aria-multiselectable="true"
          className="dropdown-menu show w-100 mt-1 shadow overflow-auto"
          style={{ top: '100%', maxHeight: '50vh' }}
        >
          {rowCount === 0 && (
            <li className="dropdown-item-text text-secondary kk-text-caption">
              {t('multiSelect.noMatch')}
            </li>
          )}
          {suggestions.map((option, index) => (
            <li key={option.value}>
              <button
                type="button"
                role="option"
                aria-selected={false}
                className={`dropdown-item kukatko-tap-target d-flex align-items-center justify-content-between gap-2 ${
                  index === activeIndex ? 'active' : ''
                }`}
                // Keep the input focused so the blur handler does not close the
                // menu before the click lands.
                onMouseDown={(event) => {
                  event.preventDefault()
                }}
                onClick={() => {
                  add(option.value)
                }}
              >
                <span className="text-truncate">{option.label}</span>
                {option.count !== undefined && (
                  <span className="text-secondary kk-text-caption flex-shrink-0">
                    {option.count}
                  </span>
                )}
              </button>
            </li>
          ))}
          {canCreate && (
            <li>
              <button
                type="button"
                role="option"
                aria-selected={false}
                className={`dropdown-item kukatko-tap-target d-flex align-items-center ${
                  activeIndex === suggestions.length ? 'active' : ''
                }`}
                disabled={disabled}
                onMouseDown={(event) => {
                  event.preventDefault()
                }}
                onClick={() => {
                  create()
                }}
              >
                <span className="text-truncate">{t('multiSelect.create', { name: trimmed })}</span>
              </button>
            </li>
          )}
        </ul>
      )}

      {chips.length > 0 && (
        <ul className="list-unstyled d-flex flex-wrap gap-1 mt-2 mb-0">
          {chips.map((chip) => (
            <li key={chip.value}>
              <span
                className={`badge rounded-pill kk-chip ${
                  destructive === true ? 'text-bg-danger' : 'text-bg-secondary'
                }`}
              >
                <span className="text-truncate">{chip.label}</span>
                <button
                  type="button"
                  className="btn-close btn-close-white ms-1"
                  disabled={disabled}
                  aria-label={t('multiSelect.remove', { name: chip.label })}
                  onClick={() => {
                    remove(chip.value)
                  }}
                />
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

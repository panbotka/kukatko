import { useEffect, useMemo, useRef, useState } from 'react'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'

import { useAnchoredMenu } from '../../hooks/useAnchoredMenu'
import { nicknameTag } from '../../lib/nickname'
import { foldedEquals, foldedIncludes } from '../../lib/text'

/** One selectable option in an {@link AddAutocomplete}. */
export interface AutocompleteOption {
  /** Stable identifier passed back to {@link AddAutocompleteProps.onAdd}. */
  uid: string
  /** Human-readable text shown and filtered against. */
  label: string
  /**
   * A second name the option also answers to, shown beside the label and filtered
   * against exactly like it. It is a person's nickname („Bohouš"): when the task
   * is naming a face, the handle the user remembers is very often the only one
   * they know, so a field that matched the label alone would find nobody.
   *
   * Unlike {@link hint} it takes part in the search, which is the whole
   * difference between the two — a hint carries a number, an alias carries
   * another way to say the same name. Absent when the option has none.
   */
  alias?: string
  /**
   * Secondary text shown muted at the end of the row (e.g. how many photos a
   * person appears on). Not filtered against — the query only matches the label
   * and the alias, so a hint can carry numbers without hijacking the search.
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
  /**
   * Names that exist but are deliberately not offered — an album the photo is
   * already in, a label it already carries. They are excluded from the options
   * (picking them again would be a no-op) yet creating them would collide with
   * the entry that exists, so typing one offers neither row.
   */
  existingNames?: readonly string[]
  /** Accessible name / placeholder for the field (e.g. "Add to album"). */
  label: string
  /** Unique id tying the visually-hidden label, input and listbox together. */
  id: string
  /** Disables the field while a mutation is in flight. */
  disabled?: boolean
  /**
   * Focuses the field on mount. Set it where the field IS the task the moment it
   * appears (a modal's one field, a field that opens on demand), not where it is
   * one control among many.
   */
  autoFocus?: boolean
  /**
   * Asks for the keyboard on demand: with a number here the field takes focus on
   * mount and again whenever the number changes — deferred while the field is
   * disabled, so a request made during a mutation lands the moment the field is
   * enabled. Unset (or null) asks for nothing. For a field whose panel can open
   * *without* the reader asking for it (the face the faces panel picks by itself
   * on load), where a mount-time `autoFocus` would claim the keyboard from a
   * reader who is only arrowing through.
   */
  focusKey?: number | null
  /**
   * Spacing classes for the wrapper. Defaults to `mb-3`, the gap a field stacked
   * in a panel wants; a form row that lays out its own gaps passes `mb-0`.
   */
  className?: string
}

/** Cap on rendered suggestions so hundreds of albums/labels stay responsive. */
const MAX_SUGGESTIONS = 50

/**
 * Reports whether an option answers to the typed text, by its label or by its
 * alias, both matched case- and accent-insensitively as a substring. An absent
 * alias widens nothing: the empty string contains no non-empty needle, and an
 * empty needle already matched through the label.
 */
function matchesOption(option: AutocompleteOption, text: string): boolean {
  return foldedIncludes(option.label, text) || foldedIncludes(option.alias ?? '', text)
}

/**
 * Reports whether the typed text is an option's whole name — its label or its
 * alias, case- and accent-insensitively. Such an option ranks first: it is the
 * one the user is spelling out, and the first row is what Enter picks.
 */
function namesOption(option: AutocompleteOption, text: string): boolean {
  return foldedEquals(option.label, text) || foldedEquals(option.alias ?? '', text)
}

/**
 * A type-to-filter autocomplete for adding the photo to an album, attaching a
 * label or naming a person. As the user types, options whose label — or alias, for
 * a person their nickname — matches the query (case- and accent-insensitively)
 * appear in a dropdown, an option the query names exactly first; choosing one —
 * by click or keyboard (Up/Down to move, Enter to select, Esc to close) — calls
 * {@link AddAutocompleteProps.onAdd} and clears the input. The first row is the
 * active option the moment the list opens (`aria-activedescendant`), so typing
 * a name and pressing Enter picks it, as the comment @-mention list does.
 *
 * With {@link AddAutocompleteProps.onCreate} set, a query that names no existing
 * option gets a trailing "create «query»" row, so a photo can be given a label
 * that does not exist yet — including the very first one, when the option list
 * is empty. Without it the field only picks, and a non-empty query that filters
 * everything out shows a "nothing matches" row instead. The offer is measured
 * against {@link AddAutocompleteProps.existingNames} as well, so a name that
 * merely dropped out of the options (the photo already carries it) is never
 * offered for creation.
 *
 * Built on react-bootstrap primitives (no extra dependency) with
 * combobox/listbox ARIA roles and ~44px tap targets. The suggestion list is
 * placed by {@link import('../../hooks/useAnchoredMenu').useAnchoredMenu}, so a
 * scrollable panel or modal body around the field cannot clip it.
 */
export function AddAutocomplete({
  options,
  onAdd,
  onCreate,
  existingNames,
  label,
  id,
  disabled,
  autoFocus = false,
  focusKey,
  className = 'mb-3',
}: AddAutocompleteProps) {
  const { t } = useTranslation()
  const [text, setText] = useState('')
  const [open, setOpen] = useState(false)
  const [activeIndex, setActiveIndex] = useState(0)
  const containerRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  const listboxId = `${id}-listbox`

  // A focus request outlives the render that made it: it is remembered here and
  // honoured by the first commit in which the field can actually take the focus.
  const pendingFocus = useRef(false)
  useEffect(() => {
    if (focusKey !== undefined && focusKey !== null) {
      pendingFocus.current = true
    }
  }, [focusKey])
  useEffect(() => {
    if (!pendingFocus.current || disabled === true) {
      return
    }
    pendingFocus.current = false
    inputRef.current?.focus()
  }, [focusKey, disabled])
  const trimmed = text.trim()

  // An exact name leads; the rest keep the caller's order (sort is stable).
  const suggestions = useMemo(() => {
    const needle = text.trim()
    return options
      .filter((option) => matchesOption(option, text))
      .sort((a, b) => Number(namesOption(b, needle)) - Number(namesOption(a, needle)))
      .slice(0, MAX_SUGGESTIONS)
  }, [options, text])

  // Creating is offered only for a name nothing already carries — neither an
  // offered option nor an entry held back because the photo has it — so the row
  // never promises to create what exists. The create row sits after the
  // suggestions, so its index is `suggestions.length`.
  const canCreate =
    onCreate !== undefined &&
    trimmed !== '' &&
    !options.some((option) => foldedEquals(option.label, trimmed)) &&
    !(existingNames ?? []).some((name) => foldedEquals(name, trimmed))
  const rowCount = suggestions.length + (canCreate ? 1 : 0)

  // A dropdown is only shown once the user has typed something.
  const showDropdown = open && trimmed !== ''

  // The field is nested in a scrollable panel or modal body more often than not
  // — the add-relative dialog is `modal-dialog-scrollable`, whose `overflow:
  // auto` used to leave a single clipped half-row visible. The shared hook lifts
  // the list out of that: a fixed overlay measured off the input on desktop, an
  // in-flow block inside the panel's own scroll on a phone.
  const menu = useAnchoredMenu(inputRef, showDropdown)

  // Each row's id, for `aria-activedescendant`; the create row has no uid.
  const optionId = (index: number): string =>
    index < suggestions.length ? `${listboxId}-${suggestions[index].uid}` : `${listboxId}-create`
  const activeId =
    showDropdown && activeIndex >= 0 && activeIndex < rowCount ? optionId(activeIndex) : undefined

  // Back to the first row whenever the filtered set changes, so Enter picks it.
  useEffect(() => {
    setActiveIndex(0)
  }, [suggestions])

  function select(option: AutocompleteOption) {
    setText('')
    setOpen(false)
    setActiveIndex(0)
    onAdd(option.uid)
  }

  async function create() {
    if (onCreate === undefined || !(await onCreate(trimmed))) {
      return
    }
    setText('')
    setOpen(false)
    setActiveIndex(0)
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
          setActiveIndex((i) => Math.max(i - 1, 0))
        }
        break
      case 'Enter': {
        // Only a visible list is picked from: an empty or dismissed field lets
        // Enter through untouched.
        if (activeId === undefined) {
          break
        }
        if (canCreate && activeIndex === suggestions.length) {
          event.preventDefault()
          void create()
          break
        }
        const option = suggestions.at(activeIndex)
        if (option) {
          event.preventDefault()
          select(option)
        }
        break
      }
      case 'Escape':
        setOpen(false)
        setActiveIndex(0)
        break
      default:
        break
    }
  }

  return (
    <div
      ref={containerRef}
      className={`position-relative ${className}`}
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
        ref={inputRef}
        id={id}
        type="text"
        className="kukatko-tap-target"
        value={text}
        placeholder={label}
        role="combobox"
        aria-expanded={showDropdown}
        aria-controls={listboxId}
        aria-autocomplete="list"
        aria-activedescendant={activeId}
        autoComplete="off"
        disabled={disabled}
        // Opt-in, and only ever set where the field is the whole purpose of the
        // panel that just opened.
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
          className={menu.className}
          style={menu.style}
        >
          {rowCount === 0 && (
            <li className="dropdown-item-text text-secondary small">
              {t('photo.organize.noMatch')}
            </li>
          )}
          {suggestions.map((option, index) => (
            <li key={option.uid}>
              <button
                id={optionId(index)}
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
                {option.alias !== undefined && option.alias !== '' && (
                  <span className="ps-1 small text-secondary text-truncate">
                    {nicknameTag(option.alias)}
                  </span>
                )}
                {option.hint !== undefined && (
                  <span className="ms-auto ps-2 small text-secondary">{option.hint}</span>
                )}
              </button>
            </li>
          ))}
          {canCreate && (
            <li>
              <button
                id={optionId(suggestions.length)}
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

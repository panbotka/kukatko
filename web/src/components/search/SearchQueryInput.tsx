import { useMemo, useRef, useState } from 'react'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'

import { applyFilterKey, type KeySuggestion, suggestFilterKeys } from '../../lib/queryLanguage'

/** Props for {@link SearchQueryInput}. */
export interface SearchQueryInputProps {
  /** Unique id tying the input and its suggestion listbox together. */
  id: string
  /** The current query text (controlled). */
  value: string
  /** Called with the new query text on every change (typed or completed). */
  onChange: (value: string) => void
  /** Placeholder shown while empty. */
  placeholder?: string
  /** Focus the input on mount (the search page's primary control). */
  autoFocus?: boolean
}

/**
 * The search box that speaks the query language: a plain text input plus
 * lightweight autocomplete for filter *keys* — typing `ca` at the start of a
 * token offers `camera:`, `city:`, …. Values are never completed (the backend
 * matches them); the dropdown only appears while the trailing token could
 * still become a key. ArrowUp/Down move, Enter or Tab accept, Escape closes;
 * with the dropdown closed, Enter submits the surrounding form as usual.
 */
export function SearchQueryInput({
  id,
  value,
  onChange,
  placeholder,
  autoFocus,
}: SearchQueryInputProps) {
  const { t } = useTranslation()
  const [active, setActive] = useState(0)
  const [dismissed, setDismissed] = useState(false)
  const [focused, setFocused] = useState(false)
  // Suppress the blur-close while a suggestion is being clicked.
  const choosingRef = useRef(false)

  const suggestion: KeySuggestion | null = useMemo(() => suggestFilterKeys(value), [value])
  const open = focused && !dismissed && suggestion !== null
  const keys = suggestion?.keys ?? []
  const activeIndex = Math.min(active, Math.max(keys.length - 1, 0))

  const choose = (key: string) => {
    if (suggestion === null) {
      return
    }
    onChange(applyFilterKey(value, suggestion, key))
    setActive(0)
  }

  const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (!open) {
      return
    }
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        setActive((n) => (n + 1) % keys.length)
        break
      case 'ArrowUp':
        e.preventDefault()
        setActive((n) => (n - 1 + keys.length) % keys.length)
        break
      case 'Enter':
      case 'Tab':
        e.preventDefault()
        choose(keys[activeIndex] ?? '')
        break
      case 'Escape':
        e.preventDefault()
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
        aria-expanded={open}
        aria-controls={`${id}-keys`}
        autoComplete="off"
        onChange={(e) => {
          setDismissed(false)
          setActive(0)
          onChange(e.target.value)
        }}
        onKeyDown={onKeyDown}
        onFocus={() => {
          setFocused(true)
        }}
        onBlur={() => {
          if (!choosingRef.current) {
            setFocused(false)
          }
        }}
      />
      {open && (
        <ul
          id={`${id}-keys`}
          role="listbox"
          aria-label={t('search.keySuggestions')}
          className="dropdown-menu show mt-1 shadow overflow-auto"
          style={{ maxHeight: '16rem' }}
        >
          {keys.map((key, i) => (
            <li key={key} role="presentation">
              <button
                type="button"
                role="option"
                aria-selected={i === activeIndex}
                className={`dropdown-item${i === activeIndex ? ' active' : ''}`}
                onMouseDown={() => {
                  choosingRef.current = true
                }}
                onMouseUp={() => {
                  choosingRef.current = false
                }}
                onClick={() => {
                  choose(key)
                }}
              >
                {key}:
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

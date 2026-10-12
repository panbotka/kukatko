import {
  type ChangeEvent,
  type FocusEvent,
  type KeyboardEvent,
  type SyntheticEvent,
  useEffect,
  useId,
  useRef,
  useState,
} from 'react'
import { useTranslation } from 'react-i18next'

import { useAnchoredMenu } from '../../hooks/useAnchoredMenu'
import { useDirectory } from '../../hooks/useDirectory'
import {
  filterMentionCandidates,
  findMentionQuery,
  insertMention,
  type MentionQuery,
} from '../../lib/mentions'
import { type DirectoryUser } from '../../services/directory'
import { PersonAvatar } from '../PersonAvatar'

/** Props for {@link MentionTextarea}. */
export interface MentionTextareaProps {
  id: string
  value: string
  /** Called with the whole new text — on typing, and after a mention is inserted. */
  onChange: (value: string) => void
  /**
   * The caller's own key handling (Enter sends, Shift+Enter breaks the line…).
   * It is not called for a key the open suggestion list consumed — an Enter
   * that picked a name must not also post the comment.
   */
  onKeyDown?: (event: KeyboardEvent<HTMLTextAreaElement>) => void
  onFocus?: (event: FocusEvent<HTMLTextAreaElement>) => void
  className?: string
  rows?: number
  maxLength?: number
  placeholder?: string
  disabled?: boolean
  autoFocus?: boolean
}

/**
 * The comment composer's text box, shared by the new-comment form and the
 * in-place editor of both threads (a photo's and a task's): a textarea that
 * offers the people of the library when the reader types `@`.
 *
 * The list opens at an `@` that starts a word and follows what is typed after
 * it (`lib/mentions` `findMentionQuery`), filtered accent- and
 * case-insensitively, so `@tom` finds „Tomáš". Up/Down move, Enter or Tab
 * pick, Escape closes; a tap or click picks **on mousedown**, before the
 * textarea can lose focus (and because a test's `.click()` on a row would
 * otherwise pick nothing). Picking replaces the `@query` with the stored
 * token, `@[Name](user:uid)` plus a space, and puts the caret after it, so
 * typing goes on. The token is what the reader sees in the box — it is the
 * one syntax the backend recognises, and a prettier WYSIWYG box would be a
 * second editor to keep honest.
 *
 * The directory is fetched the first time an `@` is typed, never on mount
 * (`useDirectory`). The list hangs off the textarea through `useAnchoredMenu`
 * like every other suggestion list, so a scrolling drawer cannot clip it and
 * on a phone it flows in place above the keyboard; the rows are tap targets
 * and the field itself reads at 16px on a phone (app.css), so iOS does not
 * zoom into it.
 */
export function MentionTextarea({
  id,
  value,
  onChange,
  onKeyDown,
  onFocus,
  className = 'form-control form-control-sm',
  rows = 2,
  maxLength,
  placeholder,
  disabled = false,
  autoFocus = false,
}: MentionTextareaProps) {
  const { t } = useTranslation()
  const ref = useRef<HTMLTextAreaElement>(null)
  const listboxId = `${useId()}-mentions`
  const [caret, setCaret] = useState(0)
  const [activeIndex, setActiveIndex] = useState(0)
  // The query Escape dismissed; the list stays shut until the reader types on.
  const [dismissed, setDismissed] = useState<string | null>(null)
  // Where the caret belongs once the text an insertion produced has rendered.
  const pendingCaret = useRef<number | null>(null)

  const query: MentionQuery | null = findMentionQuery(value, caret)
  const open = query !== null && dismissed !== `${query.start}:${query.query}`
  const { status, users } = useDirectory(open)
  const candidates = open ? filterMentionCandidates(users, query.query) : []
  const menu = useAnchoredMenu(ref, open)

  // A fresh query starts at the first row; the highlight never outlives the
  // list it belonged to.
  const queryKey = query === null ? '' : `${query.start}:${query.query}`
  useEffect(() => {
    setActiveIndex(0)
  }, [queryKey, users])

  // After an insertion the caret is put after the token — once React has
  // rendered the new value, or the browser would reset it to the end.
  useEffect(() => {
    if (pendingCaret.current === null || ref.current === null) {
      return
    }
    const at = pendingCaret.current
    pendingCaret.current = null
    ref.current.setSelectionRange(at, at)
    setCaret(at)
  }, [value])

  const syncCaret = (event: SyntheticEvent<HTMLTextAreaElement>) => {
    setCaret(event.currentTarget.selectionStart)
  }

  const pick = (user: DirectoryUser) => {
    if (query === null) {
      return
    }
    const inserted = insertMention(value, query, caret, user)
    pendingCaret.current = inserted.caret
    setDismissed(null)
    onChange(inserted.text)
  }

  const handleChange = (event: ChangeEvent<HTMLTextAreaElement>) => {
    setDismissed(null)
    setCaret(event.target.selectionStart)
    onChange(event.target.value)
  }

  // Returns true when the open list consumed the key.
  const handleListKey = (event: KeyboardEvent<HTMLTextAreaElement>): boolean => {
    if (!open) {
      return false
    }
    switch (event.key) {
      case 'ArrowDown':
        if (candidates.length > 0) {
          setActiveIndex((i) => (i + 1) % candidates.length)
        }
        return true
      case 'ArrowUp':
        if (candidates.length > 0) {
          setActiveIndex((i) => (i - 1 + candidates.length) % candidates.length)
        }
        return true
      case 'Enter':
      case 'Tab':
        if (activeIndex >= candidates.length) {
          return false
        }
        pick(candidates[activeIndex])
        return true
      case 'Escape':
        setDismissed(queryKey)
        return true
      default:
        return false
    }
  }

  return (
    <div className="kk-mention-field">
      <textarea
        ref={ref}
        id={id}
        className={className}
        rows={rows}
        maxLength={maxLength}
        placeholder={placeholder}
        value={value}
        disabled={disabled}
        autoFocus={autoFocus}
        aria-autocomplete="list"
        aria-controls={open ? listboxId : undefined}
        aria-expanded={open}
        aria-activedescendant={
          open && activeIndex < candidates.length
            ? `${listboxId}-${candidates[activeIndex].uid}`
            : undefined
        }
        onChange={handleChange}
        onKeyDown={(event) => {
          if (handleListKey(event)) {
            event.preventDefault()
            return
          }
          onKeyDown?.(event)
        }}
        onKeyUp={syncCaret}
        onClick={syncCaret}
        onSelect={syncCaret}
        onFocus={(event) => {
          syncCaret(event)
          onFocus?.(event)
        }}
      />
      {open && (
        <ul
          id={listboxId}
          role="listbox"
          aria-label={t('photo.comments.mention.listLabel')}
          className={`kk-mention-menu ${menu.className}`}
          style={menu.style}
        >
          {status === 'loading' && (
            <li className="dropdown-item-text text-secondary small">
              {t('photo.comments.mention.loading')}
            </li>
          )}
          {status !== 'loading' && candidates.length === 0 && (
            <li className="dropdown-item-text text-secondary small">
              {t('photo.comments.mention.noMatch')}
            </li>
          )}
          {candidates.map((user, index) => (
            <li key={user.uid}>
              <button
                type="button"
                id={`${listboxId}-${user.uid}`}
                role="option"
                aria-selected={index === activeIndex}
                className={`dropdown-item kukatko-tap-target d-flex align-items-center gap-2 ${
                  index === activeIndex ? 'active' : ''
                }`}
                // Picks on mousedown: the textarea keeps the focus and the
                // caret, and the row is chosen before any blur could close
                // the list.
                onMouseDown={(event) => {
                  event.preventDefault()
                  pick(user)
                }}
                onMouseEnter={() => {
                  setActiveIndex(index)
                }}
              >
                <PersonAvatar name={user.name} userUid={user.uid} />
                <span className="text-truncate">{user.name}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

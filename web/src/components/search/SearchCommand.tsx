import type { ParseKeys, TFunction } from 'i18next'
import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react'
import Modal from 'react-bootstrap/Modal'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'

import { useGlobalSearch } from '../../hooks/useGlobalSearch'
import { isTypingElement } from '../../lib/ratingHotkeys'
import { isFormModalOpen } from '../../lib/shortcuts'
import { thumbUrl } from '../../services/photos'
import { type GlobalSearchResult } from '../../services/search'
import { FadeInImage } from '../FadeInImage'
import { Icon, type IconName } from '../Icon'

/** Thumbnail size for the small entity avatars in the palette rows. */
const RESULT_THUMB_SIZE = 'tile_100'

/**
 * One selectable row in the command palette. It is the flattened, kind-agnostic
 * shape the keyboard navigation walks: a stable DOM `id` (for
 * `aria-activedescendant`), the route it opens, and the bits needed to render it.
 */
interface SearchItem {
  /** DOM id of the option element, referenced by the input's active descendant. */
  id: string
  /** Route this row navigates to when opened. */
  to: string
  /** The row's main line (a title, a name, or the "search everything" action). */
  primary: string
  /** An optional dimmer second line (a photo's capture date). */
  secondary?: string
  /** An optional trailing count (an album's / label's photo tally). */
  count?: number
  /** A cover/avatar photo UID to show as a thumbnail, when the entity has one. */
  thumbUid?: string
  /** Renders the thumbnail as a circle (people) rather than a rounded square. */
  circle?: boolean
  /** A glyph shown when there is no thumbnail (the action row, labels, gaps). */
  icon?: IconName
}

/** A titled block of {@link SearchItem}s (Photos / People / Albums / Labels). */
interface SearchGroup {
  /** Stable key for React and the group's DOM ids. */
  key: string
  /** i18n key for the visible heading, or `undefined` for the top action row. */
  headingKey?: ParseKeys
  items: SearchItem[]
}

/** Returns the first non-empty string among the candidates, or an empty string. */
function firstNonEmpty(...candidates: (string | undefined)[]): string {
  for (const candidate of candidates) {
    if (candidate !== undefined && candidate !== '') {
      return candidate
    }
  }
  return ''
}

/**
 * Formats a photo's capture timestamp as a short, localized date for the row's
 * second line, or an empty string when it is missing or unparseable.
 */
function formatPhotoDate(takenAt: string | undefined, lang: string): string {
  if (takenAt === undefined || takenAt === '') {
    return ''
  }
  const date = new Date(takenAt)
  if (Number.isNaN(date.getTime())) {
    return ''
  }
  return date.toLocaleDateString(lang, { year: 'numeric', month: 'short', day: 'numeric' })
}

/**
 * Builds the ordered, grouped palette rows for a query and its (possibly still
 * loading) result. The first row is always the "search everything" action, so a
 * user who just types and presses Enter lands on the full search page; the entity
 * groups (photos, people, albums, labels — the groups the global-search endpoint
 * returns) follow when they arrive. An empty query yields no rows (the idle hint
 * shows instead).
 */
function buildGroups(
  query: string,
  result: GlobalSearchResult | null,
  lang: string,
  t: TFunction,
): SearchGroup[] {
  const trimmed = query.trim()
  if (trimmed === '') {
    return []
  }

  const untitled = t('globalSearch.untitled')
  const searchAll = new URLSearchParams({ q: trimmed }).toString()
  const groups: SearchGroup[] = [
    {
      key: 'action',
      items: [
        {
          id: 'sc-opt-action',
          to: `/search?${searchAll}`,
          primary: t('searchCommand.seeAll', { query: trimmed }),
          icon: 'search',
        },
      ],
    },
  ]

  if (result === null) {
    return groups
  }

  if (result.photos.length > 0) {
    groups.push({
      key: 'photos',
      headingKey: 'globalSearch.groups.photos',
      items: result.photos.map((photo) => ({
        id: `sc-opt-photo-${photo.uid}`,
        to: `/photos/${photo.uid}`,
        primary: firstNonEmpty(photo.title, photo.original_name, photo.file_name),
        secondary: formatPhotoDate(photo.taken_at, lang),
        thumbUid: photo.uid,
        icon: 'images',
      })),
    })
  }
  if (result.people.length > 0) {
    groups.push({
      key: 'people',
      headingKey: 'globalSearch.groups.people',
      items: result.people.map((person) => ({
        id: `sc-opt-person-${person.uid}`,
        to: `/people/${person.uid}`,
        primary: person.name,
        thumbUid: person.cover,
        circle: true,
        icon: 'person-circle',
      })),
    })
  }
  if (result.albums.length > 0) {
    groups.push({
      key: 'albums',
      headingKey: 'globalSearch.groups.albums',
      items: result.albums.map((album) => ({
        id: `sc-opt-album-${album.uid}`,
        to: `/albums/${album.uid}`,
        primary: album.title || untitled,
        count: album.photo_count,
        thumbUid: album.cover,
        icon: 'collection',
      })),
    })
  }
  if (result.labels.length > 0) {
    groups.push({
      key: 'labels',
      headingKey: 'globalSearch.groups.labels',
      items: result.labels.map((label) => ({
        id: `sc-opt-label-${label.uid}`,
        to: `/labels/${label.uid}`,
        primary: label.name,
        count: label.photo_count,
        icon: 'tags',
      })),
    })
  }
  return groups
}

/** The leading thumbnail or glyph medallion for one result row. */
function ResultMedia({ item }: { item: SearchItem }) {
  if (item.thumbUid !== undefined && item.thumbUid !== '') {
    return (
      <FadeInImage
        src={thumbUrl(item.thumbUid, RESULT_THUMB_SIZE)}
        alt=""
        className={`kukatko-search-option__thumb${
          item.circle === true ? ' kukatko-search-option__thumb--circle' : ''
        }`}
      />
    )
  }
  return (
    <span
      className={`kukatko-search-option__icon${
        item.circle === true ? ' kukatko-search-option__thumb--circle' : ''
      }`}
    >
      <Icon name={item.icon ?? 'search'} />
    </span>
  )
}

/** Props of the internal palette dialog. */
interface DialogProps {
  show: boolean
  onClose: () => void
}

/**
 * The command palette itself: a top-anchored dialog with a live query field, the
 * grouped keyboard-navigable results, and a persistent key legend. It reuses
 * {@link useGlobalSearch} (debounced, race-safe) for the data and the app's Modal
 * for the focus trap, backdrop and Escape-to-close. Open/closed state and the
 * query live here in component state only — never in the URL — so opening the
 * palette and picking a result leaves the browser's Back behaviour untouched.
 */
function SearchCommandDialog({ show, onClose }: DialogProps) {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const inputRef = useRef<HTMLInputElement>(null)
  const listboxId = useId()

  const [query, setQuery] = useState('')
  const [activeIndex, setActiveIndex] = useState(0)
  const { status, result } = useGlobalSearch(query)

  const groups = useMemo(
    () => buildGroups(query, result, i18n.language, t),
    [query, result, i18n.language, t],
  )
  const flat = useMemo(() => groups.flatMap((group) => group.items), [groups])

  // A new query resets the cursor to the top row; a shrinking result set clamps
  // it back into range so the active id always points at a real row.
  useEffect(() => {
    setActiveIndex(0)
  }, [query])
  useEffect(() => {
    setActiveIndex((index) => (index >= flat.length ? 0 : index))
  }, [flat.length])

  const activeId = flat.at(activeIndex)?.id
  // Keep the keyboard-selected row scrolled into view as the cursor moves.
  useEffect(() => {
    if (activeId !== undefined) {
      document.getElementById(activeId)?.scrollIntoView({ block: 'nearest' })
    }
  }, [activeId])

  /** Navigates to a row's target and dismisses the palette. */
  const openItem = useCallback(
    (item: SearchItem | undefined) => {
      if (item === undefined) {
        return
      }
      onClose()
      void navigate(item.to)
    },
    [navigate, onClose],
  )

  function onInputKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
    // Escape closes from any state (even an empty field), so handle it before the
    // no-results short-circuit rather than leaning on the Modal's own key handling.
    if (event.key === 'Escape') {
      event.preventDefault()
      onClose()
      return
    }
    if (flat.length === 0) {
      return
    }
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault()
        setActiveIndex((index) => (index + 1) % flat.length)
        break
      case 'ArrowUp':
        event.preventDefault()
        setActiveIndex((index) => (index - 1 + flat.length) % flat.length)
        break
      case 'Home':
        event.preventDefault()
        setActiveIndex(0)
        break
      case 'End':
        event.preventDefault()
        setActiveIndex(flat.length - 1)
        break
      case 'Enter':
        event.preventDefault()
        openItem(flat.at(activeIndex))
        break
      default:
        break
    }
  }

  const trimmed = query.trim()

  // With a query typed, the listbox always carries at least the "search
  // everything" action row (entity groups stream in beneath it), so the palette
  // only falls back to a plain message when there is genuinely nothing to act on:
  // an empty field (idle) or a failed request (error).
  let message: string | null = null
  if (trimmed === '') {
    message = t('searchCommand.idle')
  } else if (status === 'error') {
    message = t('searchCommand.error')
  }
  const listboxOpen = message === null && flat.length > 0

  return (
    <Modal
      show={show}
      onHide={onClose}
      onEntered={() => inputRef.current?.focus()}
      onExited={() => {
        setQuery('')
        setActiveIndex(0)
      }}
      aria-label={t('searchCommand.dialogLabel')}
      dialogClassName="kukatko-search-dialog"
      contentClassName="kukatko-search-panel"
    >
      <div className="kukatko-search-field">
        <Icon name="search" />
        <input
          ref={inputRef}
          type="text"
          className="kukatko-search-input"
          value={query}
          placeholder={t('searchCommand.placeholder')}
          aria-label={t('searchCommand.dialogLabel')}
          role="combobox"
          aria-autocomplete="list"
          aria-expanded={listboxOpen}
          aria-controls={listboxOpen ? listboxId : undefined}
          aria-activedescendant={listboxOpen ? activeId : undefined}
          autoComplete="off"
          spellCheck={false}
          onChange={(event) => {
            setQuery(event.target.value)
          }}
          onKeyDown={onInputKeyDown}
        />
        {query !== '' && (
          <button
            type="button"
            className="btn-close"
            aria-label={t('searchCommand.clear')}
            onClick={() => {
              setQuery('')
              inputRef.current?.focus()
            }}
          />
        )}
      </div>

      {listboxOpen ? (
        <ul
          id={listboxId}
          role="listbox"
          aria-label={t('searchCommand.dialogLabel')}
          className="kukatko-search-results"
        >
          {groups.map((group) => (
            <li key={group.key} className="kukatko-search-group" role="presentation">
              {group.headingKey !== undefined && (
                <div className="kukatko-search-group__heading" aria-hidden="true">
                  {t(group.headingKey)}
                </div>
              )}
              <ul role="presentation" className="list-unstyled mb-0">
                {group.items.map((item) => (
                  <li key={item.id} role="presentation">
                    <button
                      type="button"
                      id={item.id}
                      role="option"
                      aria-selected={item.id === activeId}
                      className={`kukatko-search-option${item.id === activeId ? ' active' : ''}`}
                      onClick={() => {
                        openItem(item)
                      }}
                    >
                      <ResultMedia item={item} />
                      <span className="kukatko-search-option__text">
                        <span className="kukatko-search-option__primary">{item.primary}</span>
                        {item.secondary !== undefined && item.secondary !== '' && (
                          <span className="kukatko-search-option__secondary">{item.secondary}</span>
                        )}
                      </span>
                      {item.count !== undefined && (
                        <span className="kukatko-search-option__count">{item.count}</span>
                      )}
                    </button>
                  </li>
                ))}
              </ul>
            </li>
          ))}
        </ul>
      ) : (
        <p className="kukatko-search-status" role="status">
          {message}
        </p>
      )}

      <div className="kukatko-search-legend" aria-hidden="true">
        <span className="kukatko-search-legend__item">
          <kbd>↑</kbd>
          <kbd>↓</kbd>
          {t('searchCommand.legend.navigate')}
        </span>
        <span className="kukatko-search-legend__item">
          <kbd>↵</kbd>
          {t('searchCommand.legend.open')}
        </span>
        <span className="kukatko-search-legend__item">
          <kbd>esc</kbd>
          {t('searchCommand.legend.close')}
        </span>
      </div>
    </Modal>
  )
}

/**
 * The header's global search: a field-shaped trigger that opens a command
 * palette. The palette is reachable from anywhere with `/` or Cmd/Ctrl-K —
 * neither of which hijacks typing (the `/` shortcut is suppressed while a text
 * field or a form dialog has focus, mirroring the app's other shortcuts). The
 * open state is local component state, so the palette never touches the URL-driven
 * view state and Back keeps working.
 */
export function SearchCommand() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      // Cmd/Ctrl-K is the canonical command-palette chord: it toggles the palette
      // from anywhere, even while typing (that is the point of a modifier chord),
      // so it must be handled outside the shared shortcut hook, which ignores
      // modifiers by design.
      if ((event.metaKey || event.ctrlKey) && !event.altKey && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        setOpen((value) => !value)
        return
      }
      // `/` opens it too, but only when the user is not typing into a field and no
      // form dialog is up — so it never eats a slash the user meant to type.
      if (
        event.key === '/' &&
        !event.metaKey &&
        !event.ctrlKey &&
        !event.altKey &&
        !isTypingElement(event.target) &&
        !isFormModalOpen()
      ) {
        event.preventDefault()
        setOpen(true)
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [])

  return (
    <>
      <button
        type="button"
        className="kukatko-search-trigger"
        aria-label={t('searchCommand.open')}
        aria-keyshortcuts="/ Control+K Meta+K"
        onClick={() => {
          setOpen(true)
        }}
      >
        <Icon name="search" />
        <span className="kukatko-search-trigger__label">{t('searchCommand.triggerLabel')}</span>
        <kbd className="kukatko-search-trigger__hint" aria-hidden="true">
          {t('searchCommand.shortcutHint')}
        </kbd>
      </button>
      <SearchCommandDialog
        show={open}
        onClose={() => {
          setOpen(false)
        }}
      />
    </>
  )
}

import type { ParseKeys, TFunction } from 'i18next'
import { type ReactNode, useCallback, useEffect, useId, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'

import { useGlobalSearch } from '../../hooks/useGlobalSearch'
import { useRecordSearch, useSearchHistory } from '../../hooks/useSearchHistory'
import { albumDisplayTitle } from '../../i18n/albumNames'
import { localizeCountryNames } from '../../i18n/countryNames'
import {
  DIRECT_KIND_LABEL,
  DIRECT_TARGET_ICON,
  directHitSecondary,
  directHitTitle,
} from '../../lib/directHit'
import { isTypingElement } from '../../lib/ratingHotkeys'
import { isFormModalOpen } from '../../lib/shortcuts'
import { thumbUrl } from '../../services/photos'
import {
  directHitRoute,
  type GlobalSearchDirect,
  type GlobalSearchResult,
} from '../../services/search'
import { FadeInImage } from '../FadeInImage'
import { Icon, type IconName } from '../Icon'
import Modal from '../Modal'

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
  /**
   * Where to fetch the row's thumbnail, when there is one to draw. Entity hits
   * carry the address the backend minted for their cover (already the medallion
   * size, and signed when the library sits behind a media Worker); a photo row
   * builds its own from the uid. Absent — or failing to load — the row falls
   * back to {@link SearchItem.icon}.
   */
  thumbSrc?: string
  /** Renders the thumbnail as a circle (people) rather than a rounded square. */
  circle?: boolean
  /** A glyph shown when there is no thumbnail (the action row, labels, gaps). */
  icon?: IconName
  /**
   * Set on the rows that *run a search* rather than open an entity — "search
   * everything" and the recent searches. Opening one is a deliberate submit, so
   * the query is remembered (or moved back to the front of the ring).
   */
  query?: string
}

/** A titled block of {@link SearchItem}s (Albums / Labels / People / Photos). */
interface SearchGroup {
  /** Stable key for React and the group's DOM ids. */
  key: string
  /** i18n key for the visible heading, or `undefined` for the top action row. */
  headingKey?: ParseKeys
  /**
   * An action rendered on the heading's own line — the recent-searches group's
   * "forget these". It is a real control, so unlike the heading text it is not
   * hidden from assistive tech.
   */
  action?: ReactNode
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
 * The palette group for a resolved UID lookup, or null when the query carries no
 * id (or the id resolved to nothing — the dialog says that in words instead).
 * It is a group of its own, above the "search everything" action, because an id
 * is an exact reference: pasting one and pressing Enter should open the thing,
 * not run a text search that cannot match it.
 */
function buildDirectGroup(
  direct: GlobalSearchDirect | undefined,
  t: TFunction,
): SearchGroup | null {
  if (direct === undefined) {
    return null
  }
  const route = directHitRoute(direct)
  if (route === null || direct.target_kind === undefined) {
    return null
  }
  return {
    key: 'direct',
    headingKey: 'globalSearch.direct.heading',
    items: [
      {
        id: 'sc-opt-direct',
        to: route,
        primary: directHitTitle(direct),
        secondary: directHitSecondary(direct, t),
        thumbSrc:
          direct.target_kind === 'photo' && direct.target_uid !== undefined
            ? thumbUrl(direct.target_uid, RESULT_THUMB_SIZE)
            : direct.thumb_url,
        circle: direct.target_kind === 'person',
        icon: DIRECT_TARGET_ICON[direct.target_kind],
      },
    ],
  }
}

/**
 * The palette group for the reader's recent searches, or null when there are
 * none. It is what an empty palette offers instead of an idle hint: the queries
 * come from the server, so the phone offers what was typed on the laptop.
 *
 * Each row navigates to the full search for that query — running it is what the
 * palette's rows do, and the palette itself is not a query-language editor.
 */
function buildHistoryGroup(
  queries: readonly string[],
  clear: () => void,
  t: TFunction,
): SearchGroup | null {
  if (queries.length === 0) {
    return null
  }
  return {
    key: 'history',
    headingKey: 'search.history.label',
    action: (
      // Not aria-hidden, unlike the heading beside it: this one does something.
      <button type="button" className="kukatko-search-group__action" onClick={clear}>
        {t('search.history.clear')}
      </button>
    ),
    items: queries.map((query, index) => ({
      id: `sc-opt-history-${index}`,
      to: `/search?${new URLSearchParams({ q: query }).toString()}`,
      primary: query,
      icon: 'clock-history',
      query,
    })),
  }
}

/**
 * Builds the ordered, grouped palette rows for a query and its (possibly still
 * loading) result. A resolved UID lookup comes first — pasting an id is an exact
 * reference and Enter should open it. Then the "search everything" action, so a
 * user who just types and presses Enter lands on the full search page; the entity
 * groups follow when they arrive.
 *
 * Those groups go from the most specific answer to the least: albums, labels,
 * people, photos. Somebody searching for a place name almost always means the
 * album or the label that collects it, not one of the two hundred photographs
 * that happen to match — and a single photo, being the narrowest possible answer
 * to a word, is the one thing worth scrolling for rather than tripping over. The
 * order is the drawn order and the keyboard order at once: {@link SearchGroup}s
 * are flattened in this sequence, so arrowing down walks exactly what the eye
 * reads. An empty query yields no rows; the caller offers the recent searches
 * (or the idle hint) instead.
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
          query: trimmed,
        },
      ],
    },
  ]

  if (result === null) {
    return groups
  }

  const directGroup = buildDirectGroup(result.direct, t)
  if (directGroup !== null) {
    groups.unshift(directGroup)
  }

  if (result.albums.length > 0) {
    groups.push({
      key: 'albums',
      headingKey: 'globalSearch.groups.albums',
      items: result.albums.map((album) => ({
        id: `sc-opt-album-${album.uid}`,
        to: `/albums/${album.uid}`,
        primary: album.title === '' ? untitled : albumDisplayTitle(album.title, lang),
        count: album.photo_count,
        thumbSrc: album.thumb_url,
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
        thumbSrc: label.thumb_url,
        icon: 'tags',
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
        thumbSrc: person.thumb_url,
        circle: true,
        icon: 'person-circle',
      })),
    })
  }
  if (result.photos.length > 0) {
    groups.push({
      key: 'photos',
      headingKey: 'globalSearch.groups.photos',
      items: result.photos.map((photo) => ({
        id: `sc-opt-photo-${photo.uid}`,
        to: `/photos/${photo.uid}`,
        primary: firstNonEmpty(
          localizeCountryNames(photo.title, lang),
          photo.original_name,
          photo.file_name,
        ),
        secondary: formatPhotoDate(photo.taken_at, lang),
        thumbSrc: thumbUrl(photo.uid, RESULT_THUMB_SIZE),
        icon: 'images',
      })),
    })
  }
  return groups
}

/**
 * The leading medallion of one result row: the entity's own picture when it has
 * one, and the kind's glyph when it does not.
 *
 * A photograph is what tells one album — or label, or person — from another at a
 * glance, so every row that *can* show one does. The glyph is the fallback and
 * has to stay one: an entity with no photo behind it, and an image that never
 * arrives, must both land on it rather than on an empty hole, and the two boxes
 * are the same size so a row never changes height when they swap.
 */
function ResultMedia({ item }: { item: SearchItem }) {
  // A src that fails to load falls back to the glyph. Keyed on the src itself, so
  // a row reused for a different entity as the query changes gets a fresh try.
  const [failedSrc, setFailedSrc] = useState<string | null>(null)
  const circle = item.circle === true ? ' kukatko-search-option__thumb--circle' : ''

  if (item.thumbSrc !== undefined && item.thumbSrc !== '' && item.thumbSrc !== failedSrc) {
    return (
      <FadeInImage
        src={item.thumbSrc}
        // Decoration: the row names the entity right beside it, so a screen
        // reader reading the picture too would only say everything twice.
        alt=""
        aria-hidden="true"
        className={`kukatko-search-option__thumb${circle}`}
        onError={() => {
          setFailedSrc(item.thumbSrc ?? null)
        }}
      />
    )
  }
  return (
    <span className={`kukatko-search-option__icon${circle}`}>
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
 *
 * With the field still empty it offers the reader's own recent searches instead of
 * the idle hint, each row running that search on the search page. They are ordinary
 * rows, so the palette's contract holds unchanged: the first is highlighted, Enter
 * opens it — which makes "open the palette, press Enter" repeat the last search —
 * and the group's heading carries the action that forgets them all.
 *
 * Opening a row that *runs* a search — "search everything", or a recent one — records that
 * query. It is the palette's one deliberate submit: what it hands the search page arrives
 * there as a URL, which that page rightly refuses to remember on its own.
 */
function SearchCommandDialog({ show, onClose }: DialogProps) {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const inputRef = useRef<HTMLInputElement>(null)
  const listboxId = useId()

  const [query, setQuery] = useState('')
  const [activeIndex, setActiveIndex] = useState(0)
  const { status, result } = useGlobalSearch(query)
  // Recent searches stand in for the idle hint, and are only fetched while the
  // palette is actually open on an empty field.
  const history = useSearchHistory(show && query.trim() === '')
  const recordSearch = useRecordSearch()

  const groups = useMemo(() => {
    const built = buildGroups(query, result, i18n.language, t)
    if (built.length > 0) {
      return built
    }
    const historyGroup = buildHistoryGroup(
      history.entries.map((entry) => entry.query),
      history.clear,
      t,
    )
    return historyGroup === null ? [] : [historyGroup]
  }, [query, result, i18n.language, t, history.entries, history.clear])
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
      // Opening a row that runs a search is that search being submitted — the
      // palette has no other moment at which one is, and the search page it
      // lands on only sees a query arriving in the URL.
      if (item.query !== undefined) {
        recordSearch(item.query)
      }
      onClose()
      void navigate(item.to)
    },
    [navigate, onClose, recordSearch],
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
  // an empty field with no history to offer (idle), or a failed request (error).
  let message: string | null = null
  if (trimmed === '' && flat.length === 0) {
    message = t('searchCommand.idle')
  } else if (trimmed !== '' && status === 'error') {
    message = t('searchCommand.error')
  }
  const listboxOpen = message === null && flat.length > 0

  // A well-formed id that names nothing gets said out loud, above the rows. The
  // fallback would otherwise be the "search everything" action over a string
  // that can never match any text — an empty result that reads as a broken
  // search rather than as "there is no such id".
  const unknownUid = result?.direct !== undefined && !result.direct.found ? result.direct : null

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
            title={t('searchCommand.clear')}
            onClick={() => {
              setQuery('')
              inputRef.current?.focus()
            }}
          />
        )}
      </div>

      {unknownUid !== null && (
        <p className="kukatko-search-status text-warning-emphasis mb-0" role="status">
          {t('globalSearch.direct.notFound', {
            kind: t(DIRECT_KIND_LABEL[unknownUid.kind]),
            uid: unknownUid.uid,
          })}
        </p>
      )}

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
                <div className="kukatko-search-group__heading">
                  {/* The label is decoration — the group's rows name themselves —
                      while an action beside it is not, so only the text is hidden. */}
                  <span aria-hidden="true">{t(group.headingKey)}</span>
                  {group.action}
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
 * The header's global search: a compact icon button that opens a command palette.
 * It is deliberately *not* field-shaped — the control never receives a keystroke,
 * it only opens a dialog, and looking like an input spent 16rem of a navbar that
 * was already overflowing. A magnifier the size of a tap target says the same
 * thing in a tenth of the width.
 *
 * Because nothing about it is visible text, its name and its shortcut are stated
 * outright: `aria-label` names the action, `aria-keyshortcuts` publishes the
 * chords to assistive tech, and the `title` tooltip spells both out for anyone
 * hovering — the keycap the old field displayed is otherwise the one thing the
 * shrink would have cost. The palette stays reachable from anywhere with `/` or
 * Cmd/Ctrl-K (neither hijacks typing: the `/` shortcut is suppressed while a text
 * field or a form dialog has focus, mirroring the app's other shortcuts), and both
 * chords are listed in the keyboard-shortcuts overlay that sits in the same bar.
 * The open state is local component state, so the palette never touches the
 * URL-driven view state and Back keeps working.
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
        title={t('searchCommand.triggerTitle')}
        aria-keyshortcuts="/ Control+K Meta+K"
        onClick={() => {
          setOpen(true)
        }}
      >
        <Icon name="search" />
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

import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { createSearchParams, useNavigate } from 'react-router-dom'

import { useGlobalSearch } from '../hooks/useGlobalSearch'
import { thumbUrl } from '../services/photos'
import { type GlobalSearchResult } from '../services/search'

/** Thumbnail size used for the small avatars/tiles in the quick-results dropdown. */
const DROPDOWN_THUMB_SIZE = 'tile_100'

/** The entity groups shown in the dropdown, in display order. */
type GroupKey = 'albums' | 'labels' | 'people' | 'photos'

/**
 * One flattened, navigable dropdown row. Flattening the grouped result into a
 * single ordered list makes arrow-key navigation across groups trivial while the
 * `group` field still lets the renderer insert section headers.
 */
interface DropdownItem {
  id: string
  group: GroupKey
  to: string
  primary: string
  /** Photo count shown as a subtitle (albums/labels); absent for people/photos. */
  count?: number
  /** UID of a photo to thumbnail (cover or the photo itself); absent for labels. */
  thumbUid?: string
  /** Render the thumbnail as a round avatar (people). */
  circle?: boolean
}

/**
 * Flattens a grouped global-search result into an ordered list of navigable
 * rows: albums, then labels, then people, then photos. Each row carries the route
 * its entity links to and enough data to render a compact line.
 */
function flattenItems(result: GlobalSearchResult, untitled: string): DropdownItem[] {
  const items: DropdownItem[] = []
  for (const album of result.albums) {
    items.push({
      id: `album-${album.uid}`,
      group: 'albums',
      to: `/albums/${album.uid}`,
      primary: album.title || untitled,
      count: album.photo_count,
      thumbUid: album.cover,
    })
  }
  for (const label of result.labels) {
    items.push({
      id: `label-${label.uid}`,
      group: 'labels',
      to: `/labels/${label.uid}`,
      primary: label.name,
      count: label.photo_count,
    })
  }
  for (const person of result.people) {
    items.push({
      id: `person-${person.uid}`,
      group: 'people',
      to: `/people/${person.uid}`,
      primary: person.name,
      thumbUid: person.cover,
      circle: true,
    })
  }
  for (const photo of result.photos) {
    items.push({
      id: `photo-${photo.uid}`,
      group: 'photos',
      to: `/photos/${photo.uid}`,
      primary: photo.title || photo.file_name,
      thumbUid: photo.uid,
    })
  }
  return items
}

/**
 * Compact search box in the navbar with a live, grouped quick-results dropdown.
 * As the user types (debounced inside {@link useGlobalSearch}), matching albums,
 * labels, people and photos appear grouped by type; clicking a row navigates
 * straight to that entity, while pressing Enter (or Submit) goes to the full
 * `/search?q=…` page. The dropdown is keyboard-navigable (Up/Down/Enter) and
 * closes on blur or Escape. The query in the URL keeps results shareable and Back
 * working; this component just adds a faster, cross-entity entry point.
 */
export function NavbarSearch() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [text, setText] = useState('')
  const [open, setOpen] = useState(false)
  const [activeIndex, setActiveIndex] = useState(-1)
  const containerRef = useRef<HTMLDivElement>(null)

  const { status, result } = useGlobalSearch(text)
  const items = useMemo(
    () => (result ? flattenItems(result, t('globalSearch.untitled')) : []),
    [result, t],
  )

  const trimmed = text.trim()
  const showDropdown = open && trimmed !== ''

  // Reset the keyboard highlight whenever the result set changes.
  useEffect(() => {
    setActiveIndex(-1)
  }, [items])

  function goToSearchPage() {
    const q = trimmed
    if (q === '') {
      return
    }
    setOpen(false)
    void navigate({ pathname: '/search', search: `?${createSearchParams({ q }).toString()}` })
  }

  function selectItem(item: DropdownItem) {
    setOpen(false)
    setText('')
    void navigate(item.to)
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    switch (e.key) {
      case 'ArrowDown':
        if (items.length > 0) {
          e.preventDefault()
          setOpen(true)
          setActiveIndex((i) => Math.min(i + 1, items.length - 1))
        }
        break
      case 'ArrowUp':
        if (items.length > 0) {
          e.preventDefault()
          setActiveIndex((i) => Math.max(i - 1, -1))
        }
        break
      case 'Enter': {
        const item = activeIndex >= 0 ? items[activeIndex] : undefined
        if (item) {
          // Navigate to the highlighted entity instead of submitting the form.
          e.preventDefault()
          selectItem(item)
        }
        // Otherwise fall through to the form's submit → the full /search page.
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
      className="position-relative d-flex me-md-2 my-2 my-md-0"
      onBlur={(e) => {
        // Close only when focus leaves the whole widget (not on inner focus moves).
        if (!containerRef.current?.contains(e.relatedTarget)) {
          setOpen(false)
        }
      }}
    >
      <Form
        role="search"
        aria-label={t('search.formLabel')}
        className="d-flex flex-grow-1"
        onSubmit={(e) => {
          e.preventDefault()
          goToSearchPage()
        }}
      >
        <Form.Control
          type="search"
          size="sm"
          className="flex-grow-1"
          value={text}
          placeholder={t('search.placeholder')}
          aria-label={t('search.queryLabel')}
          role="combobox"
          aria-expanded={showDropdown}
          aria-controls="global-search-results"
          autoComplete="off"
          onFocus={() => {
            setOpen(true)
          }}
          onKeyDown={handleKeyDown}
          onChange={(e) => {
            setText(e.target.value)
            setOpen(true)
          }}
        />
        <Button type="submit" size="sm" variant="outline-light" className="ms-2">
          {t('search.submit')}
        </Button>
      </Form>

      {showDropdown && (
        <ul
          id="global-search-results"
          role="listbox"
          aria-label={t('globalSearch.resultsLabel')}
          className="dropdown-menu show w-100 mt-1 shadow overflow-auto"
          style={{ top: '100%', maxHeight: '70vh' }}
        >
          {status === 'loading' && items.length === 0 && (
            <li className="dropdown-item-text d-flex align-items-center gap-2 text-secondary">
              <Spinner animation="border" size="sm" role="status" />
              {t('globalSearch.loading')}
            </li>
          )}

          {status === 'ready' && items.length === 0 && (
            <li className="dropdown-item-text text-secondary small">{t('globalSearch.empty')}</li>
          )}

          {status === 'error' && (
            <li className="dropdown-item-text text-danger small">{t('globalSearch.error')}</li>
          )}

          {items.map((item, index) => {
            const showHeader = index === 0 || items[index - 1].group !== item.group
            return (
              <Fragment key={item.id}>
                {showHeader && (
                  <li className="dropdown-header">{t(`globalSearch.groups.${item.group}`)}</li>
                )}
                <li>
                  <button
                    type="button"
                    role="option"
                    aria-selected={index === activeIndex}
                    className={`dropdown-item d-flex align-items-center gap-2 ${
                      index === activeIndex ? 'active' : ''
                    }`}
                    // Keep the input focused so the blur handler does not close the
                    // menu before the click lands.
                    onMouseDown={(e) => {
                      e.preventDefault()
                    }}
                    onClick={() => {
                      selectItem(item)
                    }}
                  >
                    {item.thumbUid ? (
                      <img
                        src={thumbUrl(item.thumbUid, DROPDOWN_THUMB_SIZE)}
                        alt=""
                        width={32}
                        height={32}
                        loading="lazy"
                        className={`flex-shrink-0 object-fit-cover ${
                          item.circle ? 'rounded-circle' : 'rounded'
                        }`}
                        style={{ width: 32, height: 32 }}
                      />
                    ) : (
                      <span
                        aria-hidden="true"
                        className="flex-shrink-0 rounded bg-secondary-subtle"
                        style={{ width: 32, height: 32 }}
                      />
                    )}
                    <span className="text-truncate">{item.primary}</span>
                    {item.count !== undefined && (
                      <span className="ms-auto text-secondary small flex-shrink-0">
                        {t('albums.photoCount', { count: item.count })}
                      </span>
                    )}
                  </button>
                </li>
              </Fragment>
            )
          })}
        </ul>
      )}
    </div>
  )
}

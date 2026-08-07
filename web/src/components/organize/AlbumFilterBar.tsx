import { type ParseKeys } from 'i18next'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import ButtonGroup from 'react-bootstrap/ButtonGroup'
import Form from 'react-bootstrap/Form'
import InputGroup from 'react-bootstrap/InputGroup'
import { useTranslation } from 'react-i18next'

import {
  type AlbumsView,
  type AlbumSort,
  type AlbumTab,
  ALBUM_SORTS,
  ALBUM_TABS,
  ALBUMS_SHOW_EMPTY,
  toAlbumSort,
  toAlbumTab,
} from '../../lib/albumBrowse'
import { type SetUrlState } from '../../lib/urlState'
import { Icon } from '../Icon'

/** The i18n label key of each section, so the strip and the copy cannot drift apart. */
const TAB_LABEL_KEY: Record<AlbumTab, ParseKeys> = {
  album: 'albums.tabs.album',
  folder: 'albums.tabs.folder',
  moment: 'albums.tabs.moment',
  state: 'albums.tabs.state',
}

/** The i18n label key of each ordering. */
const SORT_LABEL_KEY: Record<AlbumSort, ParseKeys> = {
  date: 'albums.sort.date',
  name: 'albums.sort.name',
  count: 'albums.sort.count',
}

/** Props for {@link AlbumFilterBar}. */
export interface AlbumFilterBarProps {
  /** The current URL-encoded view of the album index. */
  view: AlbumsView
  /** Applies a patch to that view (and so to the URL). */
  onChange: SetUrlState<AlbumsView>
  /** How many albums each section holds under the current search and empty filter. */
  counts: Record<AlbumTab, number>
}

/**
 * The album index's own filter bar: the section strip (Moje alba · Podle měsíce
 * · Momenty · Místa, each with its live count), a search over album names, the
 * ordering selector and the switch that brings empty albums back.
 *
 * It is the smaller sibling of the library's `FilterBar` and follows the same
 * conventions: every control writes straight into the URL, so Back steps through
 * the sections, and only live typing replaces the history entry instead of
 * pushing one per keystroke.
 */
export function AlbumFilterBar({ view, onChange, counts }: AlbumFilterBarProps) {
  const { t } = useTranslation()
  const tab = toAlbumTab(view.type)
  const sort = toAlbumSort(view.sort)

  return (
    <Form
      className="mb-3"
      role="search"
      aria-label={t('albums.filters.barLabel')}
      // Nothing here is submitted — every control filters in place — so Enter in
      // the search box must not reload the page out from under the view.
      onSubmit={(event) => {
        event.preventDefault()
      }}
    >
      <ButtonGroup aria-label={t('albums.filters.sections')} className="flex-wrap mb-2">
        {ALBUM_TABS.map((name) => (
          <Button
            key={name}
            type="button"
            variant={name === tab ? 'primary' : 'outline-secondary'}
            aria-pressed={name === tab}
            className="d-flex align-items-center gap-2"
            onClick={() => {
              onChange({ type: name })
            }}
          >
            {t(TAB_LABEL_KEY[name])}
            <Badge
              bg={name === tab ? 'light' : 'secondary'}
              text={name === tab ? 'dark' : undefined}
              pill
            >
              {counts[name]}
            </Badge>
          </Button>
        ))}
      </ButtonGroup>

      <div className="d-flex flex-wrap align-items-center gap-2">
        <InputGroup className="kukatko-filter-search">
          <InputGroup.Text aria-hidden="true">
            <Icon name="search" />
          </InputGroup.Text>
          <Form.Control
            type="search"
            value={view.q}
            aria-label={t('albums.filters.search')}
            placeholder={t('albums.filters.searchPlaceholder')}
            onChange={(event) => {
              // Replace rather than push: a history entry per keystroke would
              // turn Back into a backspace key.
              onChange({ q: event.target.value }, { replace: true })
            }}
          />
        </InputGroup>

        <Form.Select
          className="kukatko-filter-sort w-auto"
          value={sort}
          aria-label={t('albums.filters.sort')}
          onChange={(event) => {
            onChange({ sort: event.target.value })
          }}
        >
          {ALBUM_SORTS.map((name) => (
            <option key={name} value={name}>
              {t(SORT_LABEL_KEY[name])}
            </option>
          ))}
        </Form.Select>

        <Form.Check
          type="switch"
          id="albums-show-empty"
          className="mb-0"
          label={t('albums.filters.showEmpty')}
          checked={view.empty === ALBUMS_SHOW_EMPTY}
          onChange={(event) => {
            onChange({ empty: event.target.checked ? ALBUMS_SHOW_EMPTY : '' })
          }}
        />
      </div>
    </Form>
  )
}

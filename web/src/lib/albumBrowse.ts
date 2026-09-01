import { albumDisplayTitle } from '../i18n/albumNames'
import { type AlbumSummary, type AlbumType } from '../services/organize'

import { foldedIncludes } from './text'

/**
 * Browsing the album index: which section a machine-made album belongs to, and
 * the pure filter/sort the page applies on top of the one list the API returns.
 *
 * The API already classifies every album (`type`), and more than half of the
 * library's albums are machine-made month folders and moments. Kept in one pile
 * they bury the albums somebody actually created, so the page splits them into
 * sections and this module owns the rules — as pure functions, so the ordering
 * and the counts are testable without a DOM.
 */

/** The sections the album index is split into, in the order they are shown. */
export const ALBUM_TABS = ['album', 'folder', 'moment', 'state'] as const

/** One section of the album index. */
export type AlbumTab = (typeof ALBUM_TABS)[number]

/** The section shown when the URL asks for none: the hand-made albums. */
export const ALBUM_TAB_DEFAULT: AlbumTab = 'album'

/** The orderings the sort selector offers. */
export const ALBUM_SORTS = ['date', 'name', 'count'] as const

/** One ordering of the album index. */
export type AlbumSort = (typeof ALBUM_SORTS)[number]

/** The ordering used when the URL asks for none: the server's own ranking. */
export const ALBUM_SORT_DEFAULT: AlbumSort = 'date'

/**
 * The section an album belongs to. `month` rides along with `folder` — both are
 * machine-made calendar groupings, and giving the type its own tab would mean a
 * fifth section that is empty on every instance imported so far. A type the
 * backend grows later (and this frontend does not know yet) falls into the
 * default section rather than disappearing from every one of them.
 */
export function tabForType(type: AlbumType): AlbumTab {
  switch (type) {
    case 'folder':
    case 'month':
      return 'folder'
    case 'moment':
      return 'moment'
    case 'state':
      return 'state'
    default:
      return ALBUM_TAB_DEFAULT
  }
}

/** Narrows a raw URL value to a section, defaulting when it is not one. */
export function toAlbumTab(value: string): AlbumTab {
  return (ALBUM_TABS as readonly string[]).includes(value) ? (value as AlbumTab) : ALBUM_TAB_DEFAULT
}

/** Narrows a raw URL value to an ordering, defaulting when it is not one. */
export function toAlbumSort(value: string): AlbumSort {
  return (ALBUM_SORTS as readonly string[]).includes(value)
    ? (value as AlbumSort)
    : ALBUM_SORT_DEFAULT
}

/**
 * URL-encoded view state of the album index: the section, the name search, the
 * ordering and whether empty albums are shown. All values are strings (the
 * urlState convention), so the whole view round-trips through the query string.
 */
// A type alias (not an interface) so it satisfies the urlState `Record<string,
// string>` constraint — interfaces lack the implicit index signature TS requires.
// eslint-disable-next-line @typescript-eslint/consistent-type-definitions -- see above
export type AlbumsView = {
  type: string
  q: string
  sort: string
  /** `'1'` shows albums holding no photos; empty hides them. */
  empty: string
}

/** The album index as it opens: hand-made albums, server order, no empty ones. */
export const ALBUMS_DEFAULTS: AlbumsView = {
  type: ALBUM_TAB_DEFAULT,
  q: '',
  sort: ALBUM_SORT_DEFAULT,
  empty: '',
}

/** The URL value of `empty` that shows albums with no photos. */
export const ALBUMS_SHOW_EMPTY = '1'

/** What the album index is currently showing, decoded from the URL. */
export interface AlbumBrowseOptions {
  /** The selected section. */
  tab: AlbumTab
  /** Free-text filter over album names (folded: case- and accent-insensitive). */
  query: string
  /** The selected ordering. */
  sort: AlbumSort
  /** Whether albums holding no photos are shown. */
  showEmpty: boolean
  /** Active UI language, deciding the display names albums are matched and sorted by. */
  language: string
}

/** Decodes the URL view into the options {@link browseAlbums} takes. */
export function albumBrowseOptions(view: AlbumsView, language: string): AlbumBrowseOptions {
  return {
    tab: toAlbumTab(view.type),
    query: view.q,
    sort: toAlbumSort(view.sort),
    showEmpty: view.empty === ALBUMS_SHOW_EMPTY,
    language,
  }
}

/** Reports whether the view is the one the page opens with — nothing to reset. */
export function isDefaultAlbumsView(view: AlbumsView): boolean {
  return (Object.keys(ALBUMS_DEFAULTS) as (keyof AlbumsView)[]).every(
    (key) => view[key] === ALBUMS_DEFAULTS[key],
  )
}

/**
 * The sections the library actually has something in, in the order they are
 * shown. A library of nothing but hand-made albums has one, and the page then
 * drops the section strip altogether rather than offering three dead buttons.
 *
 * Presence is read from the whole list, not from the current search or the
 * empty-album switch: a strip that appeared and vanished as the reader typed
 * would move the grid under the pointer, and a section whose only match is
 * hidden still has to say so through its zero count.
 */
export function presentSections(albums: AlbumSummary[]): AlbumTab[] {
  const present = new Set(albums.map((album) => tabForType(album.type)))
  return ALBUM_TABS.filter((tab) => present.has(tab))
}

/** The album index after filtering: what to render, and what each section holds. */
export interface AlbumBrowseResult {
  /** The albums of the selected section, in the selected order. */
  visible: AlbumSummary[]
  /** How many albums each section holds under the current search / empty filter. */
  counts: Record<AlbumTab, number>
  /** How many albums the search and the empty filter dropped, across all sections. */
  filteredOut: number
  /** The sections this library has albums in — what the strip may offer. */
  sections: AlbumTab[]
  /** The section actually shown, which is the requested one only if it exists here. */
  tab: AlbumTab
}

/**
 * Reports whether an album's name matches the search, checking both the stored
 * title and the Czech display name — typing `leden` finds `January 2026`, and so
 * does typing `January`.
 */
function matchesQuery(album: AlbumSummary, query: string, language: string): boolean {
  if (query.trim() === '') {
    return true
  }
  return (
    foldedIncludes(album.title, query) ||
    foldedIncludes(albumDisplayTitle(album.title, language), query)
  )
}

/**
 * Compares two albums by the name the reader actually sees, so a Czech-rendered
 * month sorts where its Czech name belongs. Numeric collation keeps `Léto 2`
 * before `Léto 10`, and base sensitivity ignores case and diacritics.
 */
function compareByName(a: AlbumSummary, b: AlbumSummary, language: string): number {
  return albumDisplayTitle(a.title, language).localeCompare(
    albumDisplayTitle(b.title, language),
    language,
    { numeric: true, sensitivity: 'base' },
  )
}

/**
 * Applies the section, the search, the empty-album filter and the ordering to
 * the whole album list, and counts what each section holds under the same search
 * and empty filter — so the tab badges answer "where are my matches?" rather
 * than restating the library's totals.
 *
 * `date` keeps the order the API returned (newest album first, by its newest
 * photo, with undated ones last). That ranking needs the photos themselves, so
 * the page preserves the server's order instead of approximating it from the
 * album's capture span.
 */
export function browseAlbums(
  albums: AlbumSummary[],
  { tab, query, sort, showEmpty, language }: AlbumBrowseOptions,
): AlbumBrowseResult {
  const pool = albums.filter(
    (album) => (showEmpty || album.photo_count > 0) && matchesQuery(album, query, language),
  )

  const counts: Record<AlbumTab, number> = { album: 0, folder: 0, moment: 0, state: 0 }
  for (const album of pool) {
    counts[tabForType(album.type)] += 1
  }

  // A section the library has nothing in is not offered by the strip, so a URL
  // asking for it (a stale link, or the default on a library made only of month
  // folders) would strand the reader on an empty grid with no way back.
  const sections = presentSections(albums)
  const shown = sections.includes(tab) ? tab : (sections[0] ?? ALBUM_TAB_DEFAULT)

  const visible = pool.filter((album) => tabForType(album.type) === shown)
  if (sort === 'name') {
    visible.sort((a, b) => compareByName(a, b, language))
  } else if (sort === 'count') {
    visible.sort((a, b) => b.photo_count - a.photo_count || compareByName(a, b, language))
  }

  return { visible, counts, filteredOut: albums.length - pool.length, sections, tab: shown }
}

import type { TFunction } from 'i18next'

import { type LibraryFacets } from '../../hooks/useLibraryFacets'
import {
  type LibraryView,
  LIBRARY_DEFAULTS,
  parseFilterList,
  removeFromFilterList,
} from '../../lib/libraryView'
import { type EntityKind } from '../entityStyle'

/** A single active-filter descriptor, rendered as a removable chip. */
export interface FilterChip {
  /** Stable key for React and the filter it represents. */
  key: string
  /** Human-readable "Field: value" summary shown on the chip. */
  label: string
  /** The patch that clears just this filter. */
  clear: Partial<LibraryView>
  /**
   * The catalog entity this chip stands for, when it is one. Album, label and
   * person chips carry a kind so the bar can colour and icon them per the shared
   * entity convention; the remaining filters (year, favorites, rating, flag, …)
   * leave it undefined and keep the neutral chip style.
   */
  kind?: EntityKind
}

/** Options for {@link buildChips}. */
export interface BuildChipsOptions {
  /**
   * The facet option lists, used to name an album/label/person by its title or
   * name rather than its UID. Omitted (or missing an entry) falls back to the raw
   * UID, so a chip is never blank.
   */
  facets?: LibraryFacets
  /**
   * Whether to include the free-text query. The filter bar leaves it out — it has
   * its own visible input, and on the search page it belongs to the page — while
   * the empty state names it, because a reader looking at zero results needs to
   * see every filter that got them there.
   */
  includeQuery?: boolean
}

/**
 * Derives the removable chips for every active filter. The returned length
 * doubles as the "active filters" count on the filter bar's toggle badge.
 */
export function buildChips(
  view: LibraryView,
  t: TFunction,
  options: BuildChipsOptions = {},
): FilterChip[] {
  const { facets, includeQuery = false } = options
  const chips: FilterChip[] = []
  const bool = (v: string) => t(v === 'true' ? 'library.triState.yes' : 'library.triState.no')

  if (includeQuery && view.q !== '') {
    chips.push({
      key: 'q',
      label: `${t('library.filters.search')}: ${view.q}`,
      clear: { q: '' },
    })
  }
  if (view.year !== '') {
    chips.push({
      key: 'year',
      label: `${t('library.filters.year')}: ${view.year}`,
      clear: { year: '' },
    })
  }
  // One chip per selected album and one per selected label (the facets combine
  // with AND). Each chip's remove strips just its own UID from the list, so
  // dismissing the last one clears the facet.
  for (const uid of parseFilterList(view.album)) {
    const album = facets?.albums.find((a) => a.uid === uid)
    chips.push({
      key: `album:${uid}`,
      label: `${t('library.filters.album')}: ${album?.title ?? uid}`,
      clear: { album: removeFromFilterList(view.album, uid) },
      kind: 'album',
    })
  }
  for (const uid of parseFilterList(view.label)) {
    const label = facets?.labels.find((l) => l.uid === uid)
    chips.push({
      key: `label:${uid}`,
      label: `${t('library.filters.label')}: ${label?.name ?? uid}`,
      clear: { label: removeFromFilterList(view.label, uid) },
      kind: 'tag',
    })
  }
  // One chip per selected person, named by the subject's name and carrying the
  // person entity hue/icon, mirroring the album/label chips (all AND-combined).
  for (const uid of parseFilterList(view.person)) {
    const subject = facets?.subjects.find((s) => s.uid === uid)
    chips.push({
      key: `person:${uid}`,
      label: `${t('library.filters.person')}: ${subject?.name ?? uid}`,
      clear: { person: removeFromFilterList(view.person, uid) },
      kind: 'person',
    })
  }
  if (view.favorite === 'true') {
    chips.push({
      key: 'favorite',
      label: t('library.filters.favorite'),
      clear: { favorite: '' },
    })
  }
  if (view.archived !== LIBRARY_DEFAULTS.archived) {
    chips.push({
      key: 'archived',
      label: t(view.archived === 'only' ? 'library.archived.only' : 'library.archived.show'),
      clear: { archived: LIBRARY_DEFAULTS.archived },
    })
  }
  if (view.has_gps !== '') {
    chips.push({
      key: 'has_gps',
      label: `${t('library.filters.hasGps')}: ${bool(view.has_gps)}`,
      clear: { has_gps: '' },
    })
  }
  if (view.camera !== '') {
    chips.push({
      key: 'camera',
      label: `${t('library.filters.camera')}: ${view.camera}`,
      clear: { camera: '' },
    })
  }
  if (view.taken_after !== '') {
    chips.push({
      key: 'taken_after',
      label: `${t('library.filters.takenAfter')}: ${view.taken_after}`,
      clear: { taken_after: '' },
    })
  }
  if (view.taken_before !== '') {
    chips.push({
      key: 'taken_before',
      label: `${t('library.filters.takenBefore')}: ${view.taken_before}`,
      clear: { taken_before: '' },
    })
  }
  if (view.min_rating !== '') {
    chips.push({
      key: 'min_rating',
      label: `${t('library.filters.minRating')}: ${t('library.minRating.atLeast', { n: view.min_rating })}`,
      clear: { min_rating: '' },
    })
  }
  if (view.flag !== '') {
    const flagLabelKey =
      view.flag === 'pick'
        ? 'library.flag.picks'
        : view.flag === 'reject'
          ? 'library.flag.rejects'
          : 'library.flag.eyes'
    chips.push({
      key: 'flag',
      label: `${t('library.filters.flag')}: ${t(flagLabelKey)}`,
      clear: { flag: '' },
    })
  }
  return chips
}

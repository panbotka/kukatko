import type { TFunction } from 'i18next'

import { type LibraryFacets } from '../../hooks/useLibraryFacets'
import {
  type LibraryView,
  LIBRARY_DEFAULTS,
  parseFilterList,
  periodOf,
  periodPatch,
  removeFromFilterList,
  UPLOADER_NONE,
} from '../../lib/libraryView'
import { ANY_PERIOD, formatPeriod, isAnyPeriod } from '../../lib/period'
import { type UploaderBucket } from '../../services/photos'
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
   * entity convention; the remaining filters (period, favorites, rating, flag, …)
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
   * The uploader facet, used to name the chosen uploader rather than show their
   * UID. It is separate from {@link BuildChipsOptions.facets} because the
   * uploader filter survives on pages whose grid is already scoped (an album),
   * where the album/label/person pickers are dropped. Omitted (or missing the
   * entry) falls back to the raw UID, so a chip is never blank.
   */
  uploaders?: readonly UploaderBucket[]
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
 *
 * `locale` is the reader's active language, used to word the capture period the
 * way its own control does — a chip and the control it mirrors must not describe
 * the same filter differently.
 */
export function buildChips(
  view: LibraryView,
  t: TFunction,
  locale: string,
  options: BuildChipsOptions = {},
): FilterChip[] {
  const { facets, uploaders, includeQuery = false } = options
  const chips: FilterChip[] = []
  const bool = (v: string) => t(v === 'true' ? 'library.triState.yes' : 'library.triState.no')

  if (includeQuery && view.q !== '') {
    chips.push({
      key: 'q',
      label: `${t('library.filters.search')}: ${view.q}`,
      clear: { q: '' },
    })
  }
  // One chip for the whole time axis, however it was set — a decade, a year, or
  // an exact date range — because there is one filter behind all three.
  const period = periodOf(view)
  if (!isAnyPeriod(period)) {
    chips.push({
      key: 'period',
      label: `${t('library.filters.period')}: ${formatPeriod(period, t, locale)}`,
      clear: periodPatch(ANY_PERIOD),
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
  // Who put the photos here. The imported group is a filter like any other, so
  // it gets a chip too — named, not shown as the reserved word behind it.
  if (view.uploader !== '') {
    chips.push({
      key: 'uploader',
      label: `${t('library.filters.uploader')}: ${uploaderChipName(view.uploader, uploaders, t)}`,
      clear: { uploader: '' },
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

/**
 * How an uploader filter is worded on its chip: the reserved "imported" group by
 * its own name, an uploader by the name the facet reported, and — when the facet
 * has not loaded (or the account is gone) — the raw UID, so the chip still says
 * which filter is on and can still be removed.
 */
function uploaderChipName(
  uid: string,
  uploaders: readonly UploaderBucket[] | undefined,
  t: TFunction,
): string {
  if (uid === UPLOADER_NONE) {
    return t('library.filters.uploaderImported')
  }
  const uploader = uploaders?.find((candidate) => candidate.uid === uid)
  return uploader === undefined || uploader.name === '' ? uid : uploader.name
}

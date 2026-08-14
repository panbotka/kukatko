import { useTranslation } from 'react-i18next'

import { formatCount } from '../../lib/format'
import { LIBRARY_PATH } from '../../lib/libraryView'
import { formatRelativeTime } from '../../lib/relativeTime'
import type { DuplicateScan, RemainingWork } from '../../services/system'

import { StatTileGrid, type StatTileSpec } from './StatTile'

/** The library narrowed to the photos carrying no coordinates. */
const NO_GPS_HREF = `${LIBRARY_PATH}?q=${encodeURIComponent('geo:no')}`

/**
 * The duplicates tile. It is the one number on the page that is not counted while
 * the request is served: the near-duplicate scan is far too expensive for a polled
 * endpoint, so the backend runs it in the background and reports when it last
 * finished. Until it has, the tile says so instead of showing a zero that would
 * read as "no duplicates".
 */
function duplicatesTile(scan: DuplicateScan, locale: string): StatTileSpec {
  if (!scan.configured) {
    return {
      key: 'duplicates',
      labelKey: 'system.remaining.duplicates',
      value: '—',
      hintKey: 'system.remaining.duplicatesOff',
    }
  }
  if (!scan.available) {
    return {
      key: 'duplicates',
      labelKey: 'system.remaining.duplicates',
      value: '—',
      hintKey: 'system.remaining.duplicatesPending',
    }
  }
  return {
    key: 'duplicates',
    labelKey: 'system.remaining.duplicates',
    value: formatCount(scan.groups, locale),
    to: '/duplicates',
    gap: true,
    hintKey: 'system.remaining.duplicatesScanned',
    hintValues: {
      age: scan.computed_at === undefined ? '' : formatRelativeTime(scan.computed_at, locale),
    },
  }
}

/**
 * The backlogs, in the order they are worked through: the people first (naming
 * faces is the point of the app), then the metadata gaps, then the two kinds of
 * duplicate.
 *
 * Every tile that has a screen to work it through on links there; the three
 * metadata gaps with no matching filter (no capture time, no place, no OCR) stay
 * static rather than pretending to lead somewhere.
 */
function tilesFor(remaining: RemainingWork, locale: string): StatTileSpec[] {
  const count = (value: number) => formatCount(value, locale)
  return [
    {
      key: 'faces-unassigned',
      labelKey: 'system.remaining.facesUnassigned',
      value: count(remaining.faces_unassigned),
      to: '/review',
      gap: true,
    },
    {
      key: 'clusters',
      labelKey: 'system.remaining.clusters',
      value: count(remaining.clusters),
      to: '/people/clusters',
      gap: true,
    },
    {
      key: 'without-taken-at',
      labelKey: 'system.remaining.withoutTakenAt',
      value: count(remaining.photos_without_taken_at),
      gap: true,
    },
    {
      key: 'without-gps',
      labelKey: 'system.remaining.withoutGps',
      value: count(remaining.photos_without_gps),
      to: NO_GPS_HREF,
      gap: true,
    },
    {
      key: 'without-place',
      labelKey: 'system.remaining.withoutPlace',
      value: count(remaining.photos_without_place),
      gap: true,
    },
    {
      key: 'without-ocr',
      labelKey: 'system.remaining.withoutOcr',
      value: count(remaining.photos_without_ocr),
      gap: true,
    },
    {
      key: 'duplicate-markers',
      labelKey: 'system.remaining.duplicateMarkers',
      value: count(remaining.duplicate_markers),
      to: '/duplicate-markers',
      gap: true,
    },
    duplicatesTile(remaining.duplicates, locale),
  ]
}

/**
 * The dashboard's answer to "what is still to do?". Every number here is a
 * backlog, so zero is the good value and a non-zero one is highlighted: this is
 * the section an operator opens the page to shrink.
 */
export function RemainingWorkPanel({ remaining }: { remaining: RemainingWork }) {
  const { t, i18n } = useTranslation()
  return (
    <section className="mb-4" aria-labelledby="system-remaining-title">
      <h2 id="system-remaining-title" className="kk-section-title mb-1">
        {t('system.dashboard.remainingTitle')}
      </h2>
      <p className="text-secondary small">{t('system.dashboard.remainingIntro')}</p>
      <StatTileGrid tiles={tilesFor(remaining, i18n.language)} />
    </section>
  )
}

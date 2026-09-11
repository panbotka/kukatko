import { useTranslation } from 'react-i18next'

import { LIBRARY_PATH } from '../../lib/libraryView'
import { formatRelativeTime } from '../../lib/relativeTime'
import type { DuplicateScan, RemainingWork, VideoStatus } from '../../services/system'

import { StatTileGrid, type StatTileSpec } from './StatTile'

/** The library narrowed to the photos carrying no coordinates. */
const NO_GPS_HREF = `${LIBRARY_PATH}?q=${encodeURIComponent('geo:no')}`

/**
 * The library narrowed to the videos. The backlog is narrower than that — the
 * videos with no streaming version — but the query language has no token for it,
 * so the tile leads to the set that contains them rather than nowhere.
 */
const VIDEOS_HREF = `${LIBRARY_PATH}?q=${encodeURIComponent('type:video')}`

/**
 * The maintenance page, where the "fill in the places" option reverse-geocodes
 * exactly the photos the "without a place" tile counts.
 */
const MAINTENANCE_PATH = '/maintenance'

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
    value: scan.groups,
    to: '/duplicates',
    tone: 'queued',
    hintKey: 'system.remaining.duplicatesScanned',
    hintValues: {
      age: scan.computed_at === undefined ? '' : formatRelativeTime(scan.computed_at, locale),
    },
  }
}

/**
 * The backlogs, in the order they are worked through: the people first (naming
 * faces is the point of the app), then the metadata gaps, then the videos with no
 * streaming version, then the two kinds of duplicate.
 *
 * Every tile that has a screen to work it through on links there; the two
 * metadata gaps with no matching filter (no capture time, no OCR) stay static
 * rather than pretending to lead somewhere. The place gap does have one — the
 * maintenance page's "fill in the places" option schedules exactly the photos it
 * counts.
 */
function tilesFor(remaining: RemainingWork, video: VideoStatus, locale: string): StatTileSpec[] {
  // Every tile in this section is a backlog: work waiting for somebody, which is
  // the `queued` tone the job table paints its waiting column in. Zero is the
  // good value here and the shared rule mutes it, which is exactly right — an
  // emptied backlog should stop asking for attention.
  return [
    {
      key: 'faces-unassigned',
      labelKey: 'system.remaining.facesUnassigned',
      value: remaining.faces_unassigned,
      to: '/review',
      tone: 'queued',
    },
    {
      key: 'clusters',
      labelKey: 'system.remaining.clusters',
      value: remaining.clusters,
      to: '/people/clusters',
      tone: 'queued',
    },
    {
      key: 'without-taken-at',
      labelKey: 'system.remaining.withoutTakenAt',
      value: remaining.photos_without_taken_at,
      tone: 'queued',
    },
    {
      key: 'without-gps',
      labelKey: 'system.remaining.withoutGps',
      value: remaining.photos_without_gps,
      to: NO_GPS_HREF,
      tone: 'queued',
    },
    {
      key: 'without-place',
      labelKey: 'system.remaining.withoutPlace',
      value: remaining.photos_without_place,
      to: MAINTENANCE_PATH,
      tone: 'queued',
    },
    {
      key: 'without-ocr',
      labelKey: 'system.remaining.withoutOcr',
      value: remaining.photos_without_ocr,
      tone: 'queued',
    },
    // With streaming switched off no video is ever encoded, so "none of them has
    // a streaming version" is the instance working as configured and not work
    // anybody can do: the tile is left out entirely rather than shown as a
    // backlog nothing would ever shrink. The video section says why.
    ...(video.streaming_enabled
      ? [
          {
            key: 'videos-without-streaming',
            labelKey: 'system.remaining.videosWithoutStreaming',
            value: remaining.videos_without_streaming,
            to: VIDEOS_HREF,
            tone: 'queued',
          } satisfies StatTileSpec,
        ]
      : []),
    {
      key: 'duplicate-markers',
      labelKey: 'system.remaining.duplicateMarkers',
      value: remaining.duplicate_markers,
      to: '/duplicate-markers',
      tone: 'queued',
    },
    duplicatesTile(remaining.duplicates, locale),
  ]
}

/**
 * The dashboard's answer to "what is still to do?". Every number here is a
 * backlog, so zero is the good value and a non-zero one is coloured as work
 * waiting: this is the section an operator opens the page to shrink.
 */
export function RemainingWorkPanel({
  remaining,
  video,
}: {
  remaining: RemainingWork
  video: VideoStatus
}) {
  const { t, i18n } = useTranslation()
  return (
    <section className="mb-4" aria-labelledby="system-remaining-title">
      <h2 id="system-remaining-title" className="kk-section-title mb-1">
        {t('system.dashboard.remainingTitle')}
      </h2>
      <p className="text-secondary small">{t('system.dashboard.remainingIntro')}</p>
      <StatTileGrid tiles={tilesFor(remaining, video, i18n.language)} />
    </section>
  )
}

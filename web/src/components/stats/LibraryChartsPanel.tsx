import type { TFunction } from 'i18next'
import Col from 'react-bootstrap/Col'
import Row from 'react-bootstrap/Row'
import { useTranslation } from 'react-i18next'

import { formatBytes, formatCount, formatMonth, formatMonthName } from '../../lib/format'
import { LIBRARY_PATH } from '../../lib/libraryView'
import { periodForYears } from '../../lib/period'
import type { CameraPhotos, LibraryCharts, MediaStorage } from '../../services/system'

import { BarList, type BarDatum } from './BarList'
import { ChartCard } from './ChartCard'
import { ChartStatement } from './ChartStatement'
import { ColumnChart, type ColumnDatum } from './ColumnChart'

/** How often the capture-year axis carries a label: one per decade. */
const YEAR_TICK = 10

/**
 * The four charts of the statistics dashboard, drawn from
 * `GET /system/stats/charts`: when the photos were taken, when they arrived,
 * what took them, and what they cost in bytes.
 *
 * Each chart is one measure, so each is one shape and one colour; what a bar
 * means is written beside it or under it, never carried by a hue. Every chart
 * states its key numbers in an `aria-label`, so the page can be read without
 * seeing a single bar, and a chart with nothing to show — or with nothing to
 * compare, see {@link ChartStatement} — says so in a sentence instead of drawing
 * an empty frame or a lone bar.
 *
 * The four smaller panels are paired by the shape they draw rather than by the
 * measure they carry: the two time axes share a row, the two lists of named
 * things share the next. A ten-row list is half a screen taller than a chart, and
 * pairing one with the other used to leave the chart's card padded out with
 * several hundred pixels of nothing.
 */
export function LibraryChartsPanel({ charts }: { charts: LibraryCharts }) {
  const { t } = useTranslation()
  return (
    <div className="d-flex flex-column gap-3" data-testid="library-charts">
      <ChartCard
        title={t('stats.charts.years.title')}
        icon="calendar-range"
        hint={t('stats.charts.years.hint')}
      >
        <PhotosByYear charts={charts} />
      </ChartCard>
      <Row className="g-3" xs={1} lg={2}>
        <Col>
          <ChartCard
            title={t('stats.charts.added.title')}
            icon="box-arrow-in-down"
            hint={t('stats.charts.added.hint')}
          >
            <AddedByMonth charts={charts} />
          </ChartCard>
        </Col>
        <Col>
          <ChartCard
            title={t('stats.charts.growth.title')}
            icon="bar-chart"
            hint={t('stats.charts.growth.hint')}
          >
            <StorageByYear charts={charts} />
          </ChartCard>
        </Col>
        <Col>
          <ChartCard
            title={t('stats.charts.cameras.title')}
            icon="image"
            hint={t('stats.charts.cameras.hint')}
          >
            <TopCameras charts={charts} />
          </ChartCard>
        </Col>
        <Col>
          <ChartCard
            title={t('stats.charts.storage.title')}
            icon="files"
            hint={t('stats.charts.storage.hint')}
          >
            <StorageByMedia charts={charts} />
          </ChartCard>
        </Col>
      </Row>
    </div>
  )
}

/**
 * The capture-year histogram, the page's widest chart because it is the page's
 * longest story — the library reaches back to 1905. A year with photos in it
 * links to that year in the library, so the chart is a way in and not only a
 * picture; an empty year links nowhere, because there would be nothing there.
 */
function PhotosByYear({ charts }: { charts: LibraryCharts }) {
  const { t, i18n } = useTranslation()
  const years = charts.photos_by_year
  if (years.length === 0) {
    return <p className="kk-chart-empty mb-0">{t('stats.charts.years.empty')}</p>
  }
  const total = years.reduce((sum, year) => sum + year.photos, 0)
  const peak = years.reduce((best, year) => (year.photos > best.photos ? year : best), years[0])
  const data: ColumnDatum[] = years.map((year) => ({
    key: String(year.year),
    value: year.photos,
    tick: year.year % YEAR_TICK === 0 ? String(year.year) : undefined,
    title: t('stats.charts.years.bar', {
      year: year.year,
      photos: photoCount(t, i18n.language, year.photos),
    }),
    href: year.photos > 0 ? yearHref(year.year) : undefined,
    linkLabel: t('stats.charts.years.link', { year: year.year }),
  }))

  return (
    <ColumnChart
      data={data}
      testId="chart-years"
      ariaLabel={t('stats.charts.years.summary', {
        from: years[0].year,
        to: years[years.length - 1].year,
        total: formatCount(total, i18n.language),
        peakYear: peak.year,
        peak: formatCount(peak.photos, i18n.language),
      })}
    />
  )
}

/**
 * Arrivals over the last twelve months — the chart that answers "are we still
 * importing?". The window is fixed by the backend, so a quiet month is a visible
 * gap rather than a month that silently left the axis.
 *
 * A window with at most one month in it is not drawn. A library filled by a
 * single import — which is how this one, and most family archives, begin — has
 * exactly that shape, and eleven zeroes beside one full-height bar is a card
 * spent saying "nothing happened" eleven times. The one month and its count are
 * stated instead, and a window with nothing in it says so outright. Two busy
 * months are already a comparison, so the chart returns the moment the data has
 * something to compare: nothing here knows anything about this instance.
 */
function AddedByMonth({ charts }: { charts: LibraryCharts }) {
  const { t, i18n } = useTranslation()
  const months = charts.added_by_month
  const filled = months.filter((month) => month.photos > 0)
  if (filled.length === 0) {
    return (
      <p className="kk-chart-empty mb-0">
        {t('stats.charts.added.empty', { months: months.length })}
      </p>
    )
  }
  if (filled.length === 1) {
    const [year, index] = monthParts(filled[0].month)
    return (
      <ChartStatement
        value={photoCount(t, i18n.language, filled[0].photos)}
        note={t('stats.charts.added.single', {
          months: months.length,
          month: formatMonth(year, index, i18n.language),
        })}
        testId="chart-added-statement"
      />
    )
  }
  // Two filled months at least, so both reductions have something to start from.
  const total = months.reduce((sum, month) => sum + month.photos, 0)
  const peak = filled.reduce((best, month) => (month.photos > best.photos ? month : best))
  const [peakYear, peakIndex] = monthParts(peak.month)
  const data: ColumnDatum[] = months.map((month) => {
    const [year, index] = monthParts(month.month)
    return {
      key: month.month,
      value: month.photos,
      tick: formatMonthName(year, index, i18n.language),
      title: t('stats.charts.added.bar', {
        month: formatMonth(year, index, i18n.language),
        photos: photoCount(t, i18n.language, month.photos),
      }),
    }
  })

  return (
    <ColumnChart
      data={data}
      testId="chart-added"
      ariaLabel={t('stats.charts.added.summary', {
        months: months.length,
        total: formatCount(total, i18n.language),
        peakMonth: formatMonth(peakYear, peakIndex, i18n.language),
        peak: formatCount(peak.photos, i18n.language),
      })}
    />
  )
}

/**
 * The ten cameras behind most of the library. Each row links to that camera's
 * photos through the library's own `camera` filter, so the destination is an
 * ordinary library view the reader can narrow further or save.
 */
function TopCameras({ charts }: { charts: LibraryCharts }) {
  const { t, i18n } = useTranslation()
  const cameras = charts.top_cameras
  if (cameras.length === 0) {
    return <p className="kk-chart-empty mb-0">{t('stats.charts.cameras.empty')}</p>
  }
  const data: BarDatum[] = cameras.map((camera) => ({
    key: camera.camera,
    label: camera.camera,
    value: camera.photos,
    valueLabel: formatCount(camera.photos, i18n.language),
    title: t('stats.charts.cameras.bar', {
      camera: camera.camera,
      photos: photoCount(t, i18n.language, camera.photos),
    }),
    href: cameraHref(camera),
    linkLabel: t('stats.charts.cameras.link', { camera: camera.camera }),
  }))

  return (
    <BarList
      data={data}
      testId="chart-cameras"
      ariaLabel={t('stats.charts.cameras.summary', {
        cameras: cameras.length,
        top: cameras[0].camera,
        photos: formatCount(cameras[0].photos, i18n.language),
      })}
    />
  )
}

/**
 * What the library costs in bytes, split across the media it holds. Every bucket
 * is drawn even when it is empty: "no video" is an answer, a missing row is not.
 */
function StorageByMedia({ charts }: { charts: LibraryCharts }) {
  const { t, i18n } = useTranslation()
  const buckets = charts.storage_by_media
  const total = buckets.reduce((sum, bucket) => sum + bucket.bytes, 0)
  const data: BarDatum[] = buckets.map((bucket) => ({
    key: bucket.media,
    label: mediaLabel(t, bucket),
    value: bucket.bytes,
    valueLabel: formatBytes(bucket.bytes, i18n.language),
    title: t('stats.charts.storage.bar', {
      media: mediaLabel(t, bucket),
      size: formatBytes(bucket.bytes, i18n.language),
      photos: photoCount(t, i18n.language, bucket.photos),
    }),
  }))

  return (
    <>
      <BarList
        data={data}
        testId="chart-storage"
        ariaLabel={t('stats.charts.storage.summary', {
          total: formatBytes(total, i18n.language),
        })}
      />
      <p className="kk-chart-empty mt-3 mb-0" data-testid="storage-total">
        {t('stats.charts.storage.total', { total: formatBytes(total, i18n.language) })}
      </p>
    </>
  )
}

/**
 * How the library grew, by the year each photo was added rather than taken. The
 * bars are the running total, so the shape is the library's size over time; what
 * a single year added rides along in the bar's hover text, because two scales on
 * one axis would only be readable by accident.
 *
 * Growth needs two years to be growth. One year is one bar, and one bar scaled
 * against itself is a solid rectangle the width of the card — which reads as
 * software that failed to draw a chart rather than as a library that is a year
 * old. That year is stated instead. A library that really did grow over five
 * years still gets its chart; the rule is the length of the series, not what is
 * known about this instance.
 */
function StorageByYear({ charts }: { charts: LibraryCharts }) {
  const { t, i18n } = useTranslation()
  const years = charts.storage_by_year
  if (years.length === 0) {
    return <p className="kk-chart-empty mb-0">{t('stats.charts.growth.empty')}</p>
  }
  if (years.length === 1) {
    return (
      <ChartStatement
        value={formatBytes(years[0].cumulative_bytes, i18n.language)}
        note={t('stats.charts.growth.single', {
          year: years[0].year,
          photos: photoCount(t, i18n.language, years[0].photos),
        })}
        testId="chart-growth-statement"
      />
    )
  }
  const last = years[years.length - 1]
  const data: ColumnDatum[] = years.map((year) => ({
    key: String(year.year),
    value: year.cumulative_bytes,
    tick: String(year.year),
    title: t('stats.charts.growth.bar', {
      year: year.year,
      total: formatBytes(year.cumulative_bytes, i18n.language),
      added: formatBytes(year.bytes, i18n.language),
    }),
  }))

  return (
    <ColumnChart
      data={data}
      testId="chart-growth"
      ariaLabel={t('stats.charts.growth.summary', {
        from: years[0].year,
        to: last.year,
        total: formatBytes(last.cumulative_bytes, i18n.language),
      })}
    />
  )
}

/** The translated, grouped "N photos" phrase every bar's hover text ends with. */
function photoCount(t: TFunction, locale: string, photos: number): string {
  return t('stats.charts.photos', { count: photos, formatted: formatCount(photos, locale) })
}

/** The translated name of a media bucket, falling back to its raw key. */
function mediaLabel(t: TFunction, bucket: MediaStorage): string {
  switch (bucket.media) {
    case 'image':
      return t('stats.charts.storage.media.image')
    case 'live':
      return t('stats.charts.storage.media.live')
    case 'video':
      return t('stats.charts.storage.media.video')
    case 'raw':
      return t('stats.charts.storage.media.raw')
    default:
      return bucket.media
  }
}

/** Splits a `YYYY-MM` bucket label into its year and its 1-based month. */
function monthParts(month: string): [number, number] {
  const [year, index] = month.split('-')
  return [Number(year), Number(index)]
}

/**
 * The library scoped to one calendar year, expressed as the period filter the
 * library itself writes — so the destination is an ordinary library view that can
 * be narrowed further, shared or saved, not a screen of its own.
 */
function yearHref(year: number): string {
  // Built by periodForYears, not by hand, so the bounds are exactly the ones the
  // period control recognises as a whole year and labels "1907" rather than a
  // date range it cannot name.
  const period = periodForYears(year, year)
  const params = new URLSearchParams({ taken_after: period.from, taken_before: period.to })
  return `${LIBRARY_PATH}?${params.toString()}`
}

/** The library scoped to one camera, through its own `camera` filter. */
function cameraHref(camera: CameraPhotos): string {
  const model = camera.model === '' ? camera.camera : camera.model
  return `${LIBRARY_PATH}?${new URLSearchParams({ camera: model }).toString()}`
}

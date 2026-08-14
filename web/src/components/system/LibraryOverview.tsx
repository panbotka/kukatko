import Card from 'react-bootstrap/Card'
import Col from 'react-bootstrap/Col'
import Row from 'react-bootstrap/Row'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { formatBytes, formatCount } from '../../lib/format'
import { LIBRARY_PATH } from '../../lib/libraryView'
import type { LibrarySummary } from '../../services/system'

import { StatTileGrid, type StatTileSpec } from './StatTile'

/** The library narrowed by a query-language filter, e.g. `type:video`. */
function libraryQuery(query: string): string {
  return `${LIBRARY_PATH}?q=${encodeURIComponent(query)}`
}

/**
 * The tiles of the library section, in the order the questions are asked: how
 * much is there, how much of it is video, what is on its way out (the trash) and
 * what is deliberately out of the grid (hidden, private), then what it is
 * organised by.
 *
 * Every tile whose number has a matching view links into it — the trash to
 * /trash, the three photo subsets to the library under the query-language filter
 * that selects exactly them, the collections to their catalogues. Faces and
 * embeddings have no listing of their own and stay static.
 */
function tilesFor(library: LibrarySummary, locale: string): StatTileSpec[] {
  const count = (value: number) => formatCount(value, locale)
  return [
    {
      key: 'photos',
      labelKey: 'system.tiles.photos',
      value: count(library.photos),
      to: LIBRARY_PATH,
    },
    {
      key: 'videos',
      labelKey: 'system.tiles.videos',
      value: count(library.videos),
      to: libraryQuery('type:video'),
    },
    {
      key: 'trashed',
      labelKey: 'system.tiles.trashed',
      value: count(library.trashed),
      to: '/trash',
    },
    {
      key: 'hidden',
      labelKey: 'system.tiles.hidden',
      value: count(library.hidden),
      to: libraryQuery('hidden:yes'),
    },
    {
      key: 'private',
      labelKey: 'system.tiles.private',
      value: count(library.private),
      to: libraryQuery('private:yes'),
    },
    { key: 'albums', labelKey: 'system.tiles.albums', value: count(library.albums), to: '/albums' },
    { key: 'labels', labelKey: 'system.tiles.labels', value: count(library.labels), to: '/labels' },
    { key: 'people', labelKey: 'system.tiles.people', value: count(library.people), to: '/people' },
    { key: 'faces', labelKey: 'system.tiles.faces', value: count(library.faces) },
    { key: 'embeddings', labelKey: 'system.tiles.embeddings', value: count(library.embeddings) },
  ]
}

/** The four upload windows, as a card of rows rather than four more tiles. */
function UploadsCard({ library }: { library: LibrarySummary }) {
  const { t, i18n } = useTranslation()
  const rows = [
    { key: 'day', value: library.uploads.day },
    { key: 'week', value: library.uploads.week },
    { key: 'month', value: library.uploads.month },
    { key: 'year', value: library.uploads.year },
  ] as const
  return (
    <Card className="h-100">
      <Card.Body>
        <h3 className="kk-text-eyebrow text-secondary mb-2">{t('system.uploads.title')}</h3>
        <dl className="row mb-0 kk-text-caption">
          {rows.map((row) => (
            <div key={row.key} className="col-12 d-flex justify-content-between">
              <dt className="text-secondary fw-normal">{t(`system.uploads.${row.key}`)}</dt>
              <dd className="mb-1" data-testid={`uploads-${row.key}`}>
                {formatCount(row.value, i18n.language)}
              </dd>
            </div>
          ))}
        </dl>
        <p className="text-secondary kk-text-caption mt-3 mb-0">{t('system.uploads.note')}</p>
      </Card.Body>
    </Card>
  )
}

/**
 * What the library weighs **according to the catalogue** — the sum of the
 * originals' recorded sizes — split into the browsable library, the trash and
 * the derived media.
 *
 * It is a different question from the server's disk (see the disk card further
 * down the page) and the two must not be confused: this instance keeps its
 * originals in an object store, so the disk holds none of them and reports a
 * near-empty originals directory while the library itself weighs tens of
 * gigabytes.
 */
function CatalogueStorageCard({ library }: { library: LibrarySummary }) {
  const { t, i18n } = useTranslation()
  const rows = [
    { key: 'library', bytes: library.library_bytes },
    { key: 'trash', bytes: library.trash_bytes },
    { key: 'derived', bytes: library.derived_bytes },
  ] as const
  return (
    <Card className="h-100">
      <Card.Body>
        <h3 className="kk-text-eyebrow text-secondary mb-2">
          {t('system.catalogueStorage.title')}
        </h3>
        <dl className="row mb-0 kk-text-caption">
          {rows.map((row) => (
            <div key={row.key} className="col-12 d-flex justify-content-between">
              <dt className="text-secondary fw-normal">
                {t(`system.catalogueStorage.${row.key}`)}
              </dt>
              <dd className="mb-1" data-testid={`catalogue-storage-${row.key}`}>
                {formatBytes(row.bytes, i18n.language)}
              </dd>
            </div>
          ))}
        </dl>
        <p className="text-secondary kk-text-caption mt-3 mb-0">
          {t('system.catalogueStorage.note')}
        </p>
      </Card.Body>
    </Card>
  )
}

/**
 * The dashboard's answer to "what is in the library?": the counts as tiles, then
 * what arrived recently and what it all weighs. It renders the numbers the status
 * snapshot already carries — there is no second fetch and no arithmetic here, so
 * this section and the statistics page cannot drift apart.
 */
export function LibraryOverview({ library }: { library: LibrarySummary }) {
  const { t, i18n } = useTranslation()
  return (
    <section className="mb-4" aria-labelledby="system-library-title">
      <h2 id="system-library-title" className="kk-section-title mb-1">
        {t('system.dashboard.libraryTitle')}
      </h2>
      <p className="text-secondary small">
        {t('system.dashboard.libraryIntro')}{' '}
        <Link to="/stats">{t('system.dashboard.libraryStatsLink')}</Link>
      </p>
      <StatTileGrid tiles={tilesFor(library, i18n.language)} />
      <Row className="g-3 mt-0" xs={1} md={2}>
        <Col>
          <UploadsCard library={library} />
        </Col>
        <Col>
          <CatalogueStorageCard library={library} />
        </Col>
      </Row>
    </section>
  )
}

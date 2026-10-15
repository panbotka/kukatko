import Card from 'react-bootstrap/Card'
import Col from 'react-bootstrap/Col'
import Row from 'react-bootstrap/Row'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { formatBytes, formatCount } from '../../lib/format'
import type { LibrarySummary } from '../../services/system'

import { StatTileGrid, type StatTileSpec } from './StatTile'

/**
 * The tiles of the library section, in the order the questions are asked: how
 * much is there, how much of it is video, what is on its way out (the trash) and
 * what is deliberately out of the grid (hidden, private), then what it is
 * organised by.
 *
 * No tile links anywhere: the numbers are the whole instance's — every
 * project's library summed — and no one page lists them. A link into "the"
 * library would open whichever project was visited last under a count that is
 * not its own; each project's own numbers, with their links, are on its page
 * under /system/projects.
 */
function tilesFor(library: LibrarySummary): StatTileSpec[] {
  // Not one of these is a queue state: they are what the library *is*, so they
  // take no colour at all and the eye is left for the sections that count work.
  // A zero still recedes — the shared rule sees to that.
  return [
    {
      key: 'photos',
      labelKey: 'system.tiles.photos',
      value: library.photos,
    },
    {
      key: 'videos',
      labelKey: 'system.tiles.videos',
      value: library.videos,
    },
    {
      key: 'trashed',
      labelKey: 'system.tiles.trashed',
      value: library.trashed,
    },
    {
      key: 'hidden',
      labelKey: 'system.tiles.hidden',
      value: library.hidden,
    },
    {
      key: 'private',
      labelKey: 'system.tiles.private',
      value: library.private,
    },
    { key: 'albums', labelKey: 'system.tiles.albums', value: library.albums },
    { key: 'labels', labelKey: 'system.tiles.labels', value: library.labels },
    { key: 'people', labelKey: 'system.tiles.people', value: library.people },
    { key: 'faces', labelKey: 'system.tiles.faces', value: library.faces },
    { key: 'embeddings', labelKey: 'system.tiles.embeddings', value: library.embeddings },
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
  const { t } = useTranslation()
  return (
    <section className="mb-4" aria-labelledby="system-library-title">
      <h2 id="system-library-title" className="kk-section-title mb-1">
        {t('system.dashboard.libraryTitle')}
      </h2>
      <p className="text-secondary small">
        {t('system.dashboard.libraryIntro')}{' '}
        <Link to="/system/projects">{t('system.dashboard.libraryStatsLink')}</Link>
      </p>
      <StatTileGrid tiles={tilesFor(library)} />
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

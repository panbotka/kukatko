import type { ParseKeys } from 'i18next'
import { Fragment } from 'react'
import Card from 'react-bootstrap/Card'
import Col from 'react-bootstrap/Col'
import Row from 'react-bootstrap/Row'
import { useTranslation } from 'react-i18next'

import { formatCount } from '../lib/format'
import type { LibraryStats } from '../services/system'

import { Icon, type IconName } from './Icon'

/**
 * One line inside a card: a label and its count. `gap` marks a coverage gap —
 * photos still missing an embedding or a face — which is highlighted while it is
 * non-zero, because that is the number the page is opened for during an import.
 */
interface StatRow {
  key: string
  labelKey: ParseKeys
  value: number
  gap?: boolean
}

/** One card: a headline count with its breakdown beneath. */
interface StatGroup {
  id: string
  titleKey: ParseKeys
  icon: IconName
  headlineKey: ParseKeys
  headline: number
  rows: StatRow[]
}

/**
 * Lays the counts out as five cards, each leading with the number that answers
 * its question ("how many photos?", "how many are embedded?") and breaking it
 * down underneath. The order follows the pipeline: what was imported, what has
 * been embedded, what has faces, who was named, how it is organised.
 */
function groupsFor(stats: LibraryStats): StatGroup[] {
  return [
    {
      id: 'photos',
      titleKey: 'stats.groups.photos',
      icon: 'images',
      headlineKey: 'stats.photos',
      headline: stats.photos,
      rows: [
        { key: 'videos', labelKey: 'stats.videos', value: stats.videos },
        { key: 'live', labelKey: 'stats.live', value: stats.photos_live },
        { key: 'archived', labelKey: 'stats.archived', value: stats.photos_archived },
      ],
    },
    {
      id: 'embeddings',
      titleKey: 'stats.groups.embeddings',
      icon: 'magic',
      headlineKey: 'stats.embeddings',
      headline: stats.embeddings,
      rows: [
        {
          key: 'with-embedding',
          labelKey: 'stats.withEmbedding',
          value: stats.photos_with_embedding,
        },
        {
          key: 'without-embedding',
          labelKey: 'stats.withoutEmbedding',
          value: stats.photos_without_embedding,
          gap: true,
        },
      ],
    },
    {
      id: 'faces',
      titleKey: 'stats.groups.faces',
      icon: 'person-bounding-box',
      headlineKey: 'stats.faces',
      headline: stats.faces,
      rows: [
        { key: 'with-faces', labelKey: 'stats.withFaces', value: stats.photos_with_faces },
        {
          key: 'without-faces',
          labelKey: 'stats.withoutFaces',
          value: stats.photos_without_faces,
          gap: true,
        },
      ],
    },
    {
      id: 'people',
      titleKey: 'stats.groups.people',
      icon: 'people',
      headlineKey: 'stats.subjects',
      headline: stats.subjects,
      rows: [
        { key: 'person', labelKey: 'stats.subjectsPerson', value: stats.subjects_person },
        { key: 'pet', labelKey: 'stats.subjectsPet', value: stats.subjects_pet },
        { key: 'other', labelKey: 'stats.subjectsOther', value: stats.subjects_other },
        { key: 'named', labelKey: 'stats.markersAssigned', value: stats.markers_assigned },
        {
          key: 'unnamed',
          labelKey: 'stats.markersUnassigned',
          value: stats.markers_unassigned,
          gap: true,
        },
      ],
    },
    {
      id: 'collections',
      titleKey: 'stats.groups.collections',
      icon: 'collection',
      headlineKey: 'stats.albums',
      headline: stats.albums,
      rows: [{ key: 'labels', labelKey: 'stats.labels', value: stats.labels }],
    },
  ]
}

/**
 * The library counts as a grid of cards — the shared rendering of
 * `GET /system/stats`, used both by the statistics page and by the System page's
 * Library section, so the two can never drift apart or double-fetch a second
 * aggregation. It is purely presentational: the caller owns loading, errors and
 * retries. Every number is grouped for the active language (see
 * {@link formatCount}); the coverage gaps are highlighted while non-zero.
 */
export function LibraryStatsCards({ stats }: { stats: LibraryStats }) {
  const { t, i18n } = useTranslation()
  return (
    <Row className="g-3" xs={1} md={2} xl={3} data-testid="library-stats">
      {groupsFor(stats).map((group) => (
        <Col key={group.id}>
          <Card className="h-100">
            <Card.Body>
              <h3 className="kk-text-eyebrow text-secondary d-flex align-items-center gap-2 mb-1">
                <Icon name={group.icon} />
                {t(group.titleKey)}
              </h3>
              <div className="kk-display" data-testid={`stat-headline-${group.id}`}>
                {formatCount(group.headline, i18n.language)}
              </div>
              <div className="text-secondary kk-text-caption mb-3">{t(group.headlineKey)}</div>
              <dl className="row mb-0 kk-text-caption">
                {group.rows.map((row) => (
                  <Fragment key={row.key}>
                    <dt className="col-8 text-secondary fw-normal">{t(row.labelKey)}</dt>
                    <dd
                      className={`col-4 text-end mb-1${
                        row.gap === true && row.value > 0 ? ' text-warning fw-semibold' : ''
                      }`}
                      data-testid={`stat-${group.id}-${row.key}`}
                    >
                      {formatCount(row.value, i18n.language)}
                    </dd>
                  </Fragment>
                ))}
              </dl>
            </Card.Body>
          </Card>
        </Col>
      ))}
    </Row>
  )
}

import type { ParseKeys } from 'i18next'
import { Fragment } from 'react'
import Card from 'react-bootstrap/Card'
import Col from 'react-bootstrap/Col'
import Row from 'react-bootstrap/Row'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { formatCount } from '../lib/format'
import { LIBRARY_PATH } from '../lib/libraryView'
import type { LibraryStats } from '../services/system'

import { Icon, type IconName } from './Icon'

/**
 * The library narrowed to photos with no face on them, written in the query
 * language so the destination is an ordinary library view the reader can filter
 * further or save — not a dead-end screen of its own.
 */
const NO_FACES_HREF = `${LIBRARY_PATH}?q=${encodeURIComponent('faces:0')}`

/**
 * Where a count leads when it is clicked. `needsWrite` marks a destination only
 * an editor may open (the review game, the trash): a viewer sees the number as
 * plain text rather than a link that would bounce them off a page they are not
 * allowed on.
 */
interface StatLink {
  /** Route the number links to. */
  to: string
  /**
   * Accessible name of the link. The formatted number alone ("16 585") names
   * nothing, so the destination is spelled out for the title and aria-label.
   */
  labelKey: ParseKeys
  /** True when only editors and above may follow the link. */
  needsWrite?: boolean
}

/**
 * One line inside a card: a label and its count. `gap` marks a coverage gap —
 * photos still waiting to be processed, faces nobody has named — which is
 * highlighted while it is non-zero, because that is the number the page is
 * opened for during an import. A highlighted number is a call to action, so
 * whatever action exists for it rides along in `link`.
 */
interface StatRow {
  key: string
  labelKey: ParseKeys
  value: number
  gap?: boolean
  link?: StatLink
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
 * its question ("how many photos?", "how many can I search by content?") and
 * breaking it down underneath. The order follows the pipeline: what was
 * imported, what can be searched by content, what has faces, who was named, how
 * it is organised.
 *
 * The wording is the family's, not the pipeline's: the card that used to be
 * titled "Embeddingy" and count embedding rows now says what an embedding buys
 * the reader — searching photos by what is in them — and leads with the photos
 * that are ready for it. Nothing about the numbers changed; the vocabulary did.
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
        {
          key: 'archived',
          labelKey: 'stats.archived',
          value: stats.photos_archived,
          link: { to: '/trash', labelKey: 'stats.links.trash', needsWrite: true },
        },
      ],
    },
    {
      id: 'content',
      titleKey: 'stats.groups.content',
      icon: 'magic',
      headlineKey: 'stats.contentReady',
      headline: stats.photos_with_embedding,
      rows: [
        {
          key: 'pending',
          labelKey: 'stats.contentPending',
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
          link: { to: NO_FACES_HREF, labelKey: 'stats.links.withoutFaces' },
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
          link: { to: '/review', labelKey: 'stats.links.unnamed', needsWrite: true },
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
 * One count, rendered as a link when the row leads somewhere the current role
 * may actually go. The link inherits the row's colour (`text-reset`) so a
 * highlighted gap stays highlighted, and keeps an underline so it still reads as
 * a link; its accessible name is the destination, because a link named "16 585"
 * tells a screen-reader user nothing.
 */
function StatValue({ row, canWrite }: { row: StatRow; canWrite: boolean }) {
  const { t, i18n } = useTranslation()
  const text = formatCount(row.value, i18n.language)
  const link = row.link
  if (link === undefined || (link.needsWrite === true && !canWrite)) {
    return <>{text}</>
  }
  const name = t(link.labelKey)
  return (
    <Link
      to={link.to}
      className="text-reset text-decoration-underline"
      title={name}
      aria-label={name}
    >
      {text}
    </Link>
  )
}

/**
 * The library counts as a grid of cards — the shared rendering of
 * `GET /system/stats`, used both by the statistics page and by the System page's
 * Library section, so the two can never drift apart or double-fetch a second
 * aggregation. The caller owns loading, errors and retries. Every number is
 * grouped for the active language (see {@link formatCount}); the coverage gaps
 * are highlighted while non-zero, and the counts that have somewhere to lead
 * (the trash, photos with nobody on them, faces still to name) are links —
 * gated on the reader's role, so nobody is offered an action they cannot take.
 */
export function LibraryStatsCards({ stats }: { stats: LibraryStats }) {
  const { t, i18n } = useTranslation()
  const { canWrite } = useAuth()
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
                      <StatValue row={row} canWrite={canWrite} />
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

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
 * The photos hidden from the library, written in the same query language —
 * `hidden:yes` is the documented way back to a hidden photo, so the count that
 * explains part of the library's smaller number also leads to the photos behind
 * it. Open to every role: hiding is an edit, reading the hidden photos is not.
 */
const HIDDEN_HREF = `${LIBRARY_PATH}?q=${encodeURIComponent('hidden:yes')}`

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

/**
 * A sentence closing a card, for the one thing a column of numbers cannot say
 * about itself: how its counts relate to another card's, or to another screen's.
 * `total`, when given, is the one number the sentence spells out — interpolated
 * as `{{total}}` and grouped for the active language.
 */
interface StatNote {
  key: ParseKeys
  total?: number
}

/** One card: a headline count with its breakdown beneath. */
interface StatGroup {
  id: string
  titleKey: ParseKeys
  icon: IconName
  headlineKey: ParseKeys
  headline: number
  rows: StatRow[]
  note?: StatNote
}

/**
 * Lays the counts out as six cards, each leading with the number that answers
 * its question ("how many photos?", "how many can I search by content?") and
 * breaking it down underneath. The order follows the pipeline: what was
 * imported, what can be searched by content, which faces were found, who was
 * named, what is drawn on the photos, how it is organised.
 *
 * The wording is the family's, not the pipeline's: the card that used to be
 * titled "Embeddingy" and count embedding rows now says what an embedding buys
 * the reader — searching photos by what is in them — and leads with the photos
 * that are ready for it. Nothing about the numbers changed; the vocabulary did.
 *
 * The photos card walks the total down to the number the library grid reports,
 * because those two disagreed with nothing on the page to explain it: the
 * catalogue holds more photos than the grid lists, and the difference is the
 * photos the user hid plus the variants folded behind a stack's primary. Each is
 * a row of its own — the hidden ones link to `hidden:yes`, the result links to
 * the library itself — and the closing sentence says why the subtraction
 * happens. No number is computed here: the backend reports all four.
 *
 * Faces and markers are two different things and each card keeps to one of them.
 * A **face** is one detection on one photo; the faces card leads with the faces
 * that have a name, its gap row is the faces that do not, and the two add up to
 * everything the detector found — which is exactly the whole the faces coverage
 * meter divides by, so the card and the meter answer the same question with the
 * same numbers. A **marker** is a box drawn on a photo; that count is smaller,
 * overlaps the detections and never adds up with them, so it lives on a card of
 * its own with a sentence saying so. Before this split all three counts were
 * labelled "obličej", read as a partition and did not add up.
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
        {
          key: 'archived',
          labelKey: 'stats.archived',
          value: stats.photos_archived,
          link: { to: '/trash', labelKey: 'stats.links.trash', needsWrite: true },
        },
        { key: 'live', labelKey: 'stats.live', value: stats.photos_live },
        {
          key: 'hidden',
          labelKey: 'stats.hidden',
          value: stats.photos_hidden,
          link: { to: HIDDEN_HREF, labelKey: 'stats.links.hidden' },
        },
        { key: 'stacked', labelKey: 'stats.stacked', value: stats.photos_stacked },
        {
          key: 'listed',
          labelKey: 'stats.listed',
          value: stats.photos_listed,
          link: { to: LIBRARY_PATH, labelKey: 'stats.links.library' },
        },
      ],
      note: { key: 'stats.photosNote', total: stats.photos_listed },
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
      headlineKey: 'stats.facesNamed',
      headline: stats.faces_assigned,
      rows: [
        {
          key: 'unnamed',
          labelKey: 'stats.facesUnnamed',
          value: stats.faces_unassigned,
          gap: true,
          link: { to: '/review', labelKey: 'stats.links.unnamed', needsWrite: true },
        },
        { key: 'with-faces', labelKey: 'stats.withFaces', value: stats.photos_with_faces },
        {
          key: 'without-faces',
          labelKey: 'stats.withoutFaces',
          value: stats.photos_without_faces,
          gap: true,
          link: { to: NO_FACES_HREF, labelKey: 'stats.links.withoutFaces' },
        },
      ],
      note: { key: 'stats.facesNote', total: stats.faces },
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
      ],
    },
    {
      id: 'markers',
      titleKey: 'stats.groups.markers',
      icon: 'bounding-box',
      headlineKey: 'stats.markers',
      headline: stats.markers,
      rows: [
        { key: 'named', labelKey: 'stats.markersAssigned', value: stats.markers_assigned },
        { key: 'unnamed', labelKey: 'stats.markersUnassigned', value: stats.markers_unassigned },
      ],
      note: { key: 'stats.markersNote' },
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
 * The sentence closing a card. It is the card's only prose, so it says only what
 * the numbers cannot: what they add up to, or which neighbouring count they must
 * not be added to.
 */
function StatNoteText({ note }: { note: StatNote }) {
  const { t, i18n } = useTranslation()
  if (note.total === undefined) {
    return <p className="text-secondary kk-text-caption mt-3 mb-0">{t(note.key)}</p>
  }
  return (
    <p className="text-secondary kk-text-caption mt-3 mb-0">
      {t(note.key, { total: formatCount(note.total, i18n.language) })}
    </p>
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
 * A card whose counts could be mistaken for a neighbour's closes with a sentence
 * saying how the two relate (see {@link groupsFor}).
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
              {group.note !== undefined && <StatNoteText note={group.note} />}
            </Card.Body>
          </Card>
        </Col>
      ))}
    </Row>
  )
}

import { useState } from 'react'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useLightbox } from '../../hooks/useLightbox'
import { formatBytes, formatDate } from '../../lib/format'
import { pairId } from '../../lib/duplicateCompare'
import { gridTemplateColumns, REVIEW_GRID_SCOPE } from '../../lib/gridDensity'
import { photoLabel } from '../../lib/photoTitle'
import { type DuplicateGroup, type DuplicateMember } from '../../services/duplicates'
import { thumbUrl } from '../../services/photos'
import { FadeInImage } from '../FadeInImage'
import { Icon } from '../Icon'
import { EnlargeButton } from '../review/EnlargeButton'
import { ReviewLightbox } from '../review/ReviewLightbox'

import '../review/review.css'

/** Thumbnail size used for the side-by-side comparison tiles. */
const COMPARE_THUMB_SIZE = 'tile_224'

/**
 * The size the overlay shows a member at: the whole frame. The tiles are
 * centre-cropped squares, which is exactly what "are these two the same shot?"
 * cannot be decided from — a crop hides the very edges the two versions differ at.
 */
const MEMBER_LIGHTBOX_SIZE = 'fit_1280'

/**
 * The pair the compare view should open on for this group: the suggested keeper
 * against the first other member, i.e. the first pair the compare queue would
 * offer for this group anyway. It is a starting position, not a filter — the
 * compare view builds its own queue over every group.
 */
function comparePairId(group: DuplicateGroup): string {
  const other = group.members.find((m) => m.uid !== group.keeper_uid)
  return pairId(group.keeper_uid, other?.uid ?? group.keeper_uid)
}

interface DuplicateGroupCardProps {
  /** The group of likely-duplicate photos to review. */
  group: DuplicateGroup
  /**
   * Whether an action is in flight (disables this card's buttons). The parent
   * holds this lock page-wide, so while one group is being resolved no other
   * group can start a competing merge behind its back.
   */
  busy: boolean
  /** Keep keeperUid and merge the rest of the group into it. */
  onResolve: (group: DuplicateGroup, keeperUid: string) => void
  /** Dismiss the group as "not a duplicate" (removes it from the view). */
  onDismiss: (groupId: string) => void
  /** How many member tiles sit side by side — the review tools' shared count. */
  density: number
}

/**
 * One reviewable duplicate group: the members shown side by side, a radio to
 * choose which photo to keep (pre-selected to the server's suggested keeper), and
 * actions to keep-the-best-and-merge-the-rest or dismiss the group. The keeper
 * choice is local state; the parent previews and performs the merge.
 *
 * A tile is a 224 px square — enough to see that a group belongs together, never
 * enough to pick within one — so a click on it enlarges the **whole frame** over
 * the page, stepping through this group's members with ←/→ and offering the same
 * "keep this one" the tile's radio does. The full compare screen stays one click
 * away in the footer for the side-by-side reading of the metadata.
 */
export function DuplicateGroupCard({
  group,
  busy,
  onResolve,
  onDismiss,
  density,
}: DuplicateGroupCardProps) {
  const { t, i18n } = useTranslation()
  const [keeperUid, setKeeperUid] = useState(group.keeper_uid)
  const lightbox = useLightbox(group.members)
  const enlarged = lightbox.item

  return (
    <Card className="mb-4">
      <Card.Header className="d-flex justify-content-between align-items-center flex-wrap gap-2">
        <span className="d-flex align-items-center gap-2">
          <Badge bg="info">{t(`duplicates.reason.${group.reason}`)}</Badge>
          {/* Somebody has already answered "yes, this is the same photo twice" in
              the sorting game. The backend sorts these groups first, so the badge
              is what explains why this one is at the top — otherwise a confirmed
              group looks like an ordinary machine guess that jumped the queue. */}
          {group.confirmed && (
            <Badge
              bg="success"
              title={t('duplicates.confirmedTitle')}
              data-testid="duplicate-confirmed"
            >
              {t('duplicates.confirmed')}
            </Badge>
          )}
          <span className="text-secondary small">
            {t('duplicates.memberCount', { count: group.members.length })}
          </span>
        </span>
        <Button
          variant="outline-secondary"
          size="sm"
          disabled={busy}
          onClick={() => {
            onDismiss(group.id)
          }}
        >
          {t('duplicates.dismiss')}
        </Button>
      </Card.Header>
      <Card.Body>
        <div
          className="d-grid kk-review-grid"
          data-density={density}
          /* The member row is what is worth making bigger here — the page is a
             list of groups, and judging happens *inside* one. The responsive
             2/3/4 columns it had are now the user's own count, and
             `kk-review-grid` is what keeps that count honest: a tile carries a
             filename, its dimensions and a radio label, so without it the `1fr`
             tracks would grow to that text and run off the side of a phone. */
          style={{
            gridTemplateColumns: gridTemplateColumns(density),
            gap: `${String(REVIEW_GRID_SCOPE.gapPx)}px`,
          }}
        >
          {group.members.map((member, index) => (
            <DuplicateMemberTile
              key={member.uid}
              member={member}
              selected={member.uid === keeperUid}
              groupId={group.id}
              onSelect={setKeeperUid}
              onEnlarge={() => {
                lightbox.open(index)
              }}
            />
          ))}
        </div>
      </Card.Body>
      <Card.Footer className="d-flex justify-content-end gap-2">
        {/* The tiles above are 224px squares — enough to spot a group, not enough
            to choose within one. The compare view is where the decision is made,
            so it is offered right next to the shortcut that skips it. */}
        <Link
          to={`/duplicates/compare?pair=${encodeURIComponent(comparePairId(group))}`}
          className="btn btn-outline-secondary btn-sm"
        >
          <Icon name="arrows-angle-expand" className="me-1" />
          {t('duplicates.compare.open')}
        </Link>
        <Button
          variant="primary"
          size="sm"
          disabled={busy}
          onClick={() => {
            onResolve(group, keeperUid)
          }}
        >
          {t('duplicates.merge.button')}
        </Button>
      </Card.Footer>

      {enlarged !== null && (
        <ReviewLightbox
          stage={{
            photoUid: enlarged.uid,
            fileWidth: enlarged.file_width,
            fileHeight: enlarged.file_height,
            // The duplicates payload carries no orientation tag; the stage takes
            // its shape from the loaded preview anyway and draws no box here.
            orientation: 0,
            size: MEMBER_LIGHTBOX_SIZE,
            href: `/photos/${enlarged.uid}`,
            alt: photoLabel(enlarged, i18n.language),
          }}
          title={`${photoLabel(enlarged, i18n.language)} · ${enlarged.file_width}×${enlarged.file_height} · ${formatBytes(enlarged.file_size)}`}
          onClose={lightbox.close}
          onPrev={lightbox.prev}
          onNext={lightbox.next}
          hasPrev={lightbox.hasPrev}
          hasNext={lightbox.hasNext}
        >
          <Button
            variant={enlarged.uid === keeperUid ? 'success' : 'outline-light'}
            className="d-flex align-items-center gap-1"
            onClick={() => {
              setKeeperUid(enlarged.uid)
            }}
          >
            <Icon name="check-lg" />
            {t('duplicates.keepThis')}
          </Button>
        </ReviewLightbox>
      )}
    </Card>
  )
}

interface DuplicateMemberTileProps {
  member: DuplicateMember
  selected: boolean
  groupId: string
  onSelect: (uid: string) => void
  /** Opens this member in the group's lightbox. */
  onEnlarge: () => void
}

/** A single comparison tile: thumbnail, metadata and the keep-this radio. */
function DuplicateMemberTile({
  member,
  selected,
  groupId,
  onSelect,
  onEnlarge,
}: DuplicateMemberTileProps) {
  const { t, i18n } = useTranslation()
  const label = photoLabel(member, i18n.language)
  return (
    <div className={`border rounded p-2 h-100 ${selected ? 'border-primary border-2' : ''}`}>
      {/* The thumbnail enlarges rather than navigating: leaving for the photo's
          own page is the overlay's corner anchor, the same control every review
          tool has, and losing the group you were judging to a navigation was
          never what a click on a duplicate tile meant to do. */}
      <EnlargeButton onEnlarge={onEnlarge} className="mb-2">
        <FadeInImage
          src={thumbUrl(member.uid, COMPARE_THUMB_SIZE)}
          alt={label}
          className="w-100 rounded"
          style={{ aspectRatio: '1 / 1', objectFit: 'cover' }}
        />
      </EnlargeButton>
      <div className="small text-truncate" title={label}>
        {label}
      </div>
      <div className="small text-secondary">
        {member.file_width}×{member.file_height} · {formatBytes(member.file_size)}
      </div>
      {member.taken_at !== undefined && member.taken_at !== '' && (
        <div className="small text-secondary">{formatDate(member.taken_at, i18n.language)}</div>
      )}
      <div className="d-flex gap-1 flex-wrap my-1">
        {member.phash_distance !== undefined && (
          <Badge bg="light" text="dark" title={t('duplicates.phashDistanceTitle')}>
            ≈{member.phash_distance}
          </Badge>
        )}
        {member.embedding_distance !== undefined && (
          <Badge bg="light" text="dark" title={t('duplicates.embeddingDistanceTitle')}>
            {member.embedding_distance.toFixed(3)}
          </Badge>
        )}
      </div>
      <Form.Check
        type="radio"
        name={`keeper-${groupId}`}
        id={`keeper-${groupId}-${member.uid}`}
        checked={selected}
        onChange={() => {
          onSelect(member.uid)
        }}
        label={t('duplicates.keepThis')}
      />
    </div>
  )
}

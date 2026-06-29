import { useState } from 'react'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Col from 'react-bootstrap/Col'
import Form from 'react-bootstrap/Form'
import Row from 'react-bootstrap/Row'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { formatBytes } from '../../lib/format'
import { type DuplicateGroup, type DuplicateMember } from '../../services/duplicates'
import { thumbUrl } from '../../services/photos'

/** Thumbnail size used for the side-by-side comparison tiles. */
const COMPARE_THUMB_SIZE = 'tile_224'

interface DuplicateGroupCardProps {
  /** The group of likely-duplicate photos to review. */
  group: DuplicateGroup
  /** Whether an action on this group is in flight (disables the buttons). */
  busy: boolean
  /** Keep keeperUid and archive the rest of the group. */
  onResolve: (group: DuplicateGroup, keeperUid: string) => void
  /** Dismiss the group as "not a duplicate" (removes it from the view). */
  onDismiss: (groupId: string) => void
}

/**
 * One reviewable duplicate group: the members shown side by side, a radio to
 * choose which photo to keep (pre-selected to the server's suggested keeper), and
 * actions to keep-and-archive-the-rest or dismiss the group. The keeper choice is
 * local state; the parent performs the archive through the bulk API.
 */
export function DuplicateGroupCard({ group, busy, onResolve, onDismiss }: DuplicateGroupCardProps) {
  const { t } = useTranslation()
  const [keeperUid, setKeeperUid] = useState(group.keeper_uid)
  const archiveCount = group.members.length - 1

  return (
    <Card className="mb-4">
      <Card.Header className="d-flex justify-content-between align-items-center flex-wrap gap-2">
        <span className="d-flex align-items-center gap-2">
          <Badge bg="info">{t(`duplicates.reason.${group.reason}`)}</Badge>
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
        <Row xs={2} sm={3} md={4} className="g-3">
          {group.members.map((member) => (
            <Col key={member.uid}>
              <DuplicateMemberTile
                member={member}
                selected={member.uid === keeperUid}
                groupId={group.id}
                onSelect={setKeeperUid}
              />
            </Col>
          ))}
        </Row>
      </Card.Body>
      <Card.Footer className="d-flex justify-content-end">
        <Button
          variant="primary"
          size="sm"
          disabled={busy}
          onClick={() => {
            onResolve(group, keeperUid)
          }}
        >
          {t('duplicates.keepAndArchive', { count: archiveCount })}
        </Button>
      </Card.Footer>
    </Card>
  )
}

interface DuplicateMemberTileProps {
  member: DuplicateMember
  selected: boolean
  groupId: string
  onSelect: (uid: string) => void
}

/** A single comparison tile: thumbnail, metadata and the keep-this radio. */
function DuplicateMemberTile({ member, selected, groupId, onSelect }: DuplicateMemberTileProps) {
  const { t } = useTranslation()
  const label = member.title !== '' ? member.title : member.file_name
  return (
    <div className={`border rounded p-2 h-100 ${selected ? 'border-primary border-2' : ''}`}>
      <Link to={`/photos/${member.uid}`} className="d-block mb-2">
        <img
          src={thumbUrl(member.uid, COMPARE_THUMB_SIZE)}
          alt={label}
          loading="lazy"
          className="w-100 rounded"
          style={{ aspectRatio: '1 / 1', objectFit: 'cover' }}
        />
      </Link>
      <div className="small text-truncate" title={label}>
        {label}
      </div>
      <div className="small text-secondary">
        {member.file_width}×{member.file_height} · {formatBytes(member.file_size)}
      </div>
      {member.taken_at !== undefined && member.taken_at !== '' && (
        <div className="small text-secondary">{new Date(member.taken_at).toLocaleDateString()}</div>
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

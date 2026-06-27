import { useState } from 'react'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { purgeCountdown } from '../../lib/trashCountdown'
import { GRID_THUMB_SIZE, type Photo, thumbUrl } from '../../services/photos'

/** Props for {@link TrashCard}. */
export interface TrashCardProps {
  photo: Photo
  /** Retention window in days, used to render the auto-purge countdown. */
  retentionDays: number
  /** Whether this card is selected for a bulk action. */
  selected: boolean
  /** Disables the per-card actions while a mutation for this photo is in flight. */
  busy: boolean
  /** Toggles this card's selection. */
  onToggleSelect: (uid: string) => void
  /** Restores (unarchives) this photo. */
  onRestore: (uid: string) => void
  /** Permanently deletes this photo (the caller confirms first). */
  onDelete: (uid: string) => void
}

/**
 * A single archived photo in the trash grid: a square thumbnail (linking to the
 * detail page), a selection checkbox for bulk actions, an auto-purge countdown
 * badge, and per-item Restore / Delete forever buttons. The thumbnail is
 * lazy-loaded in a fixed square box so the grid never shifts as images stream in.
 */
export function TrashCard({
  photo,
  retentionDays,
  selected,
  busy,
  onToggleSelect,
  onRestore,
  onDelete,
}: TrashCardProps) {
  const { t } = useTranslation()
  const [loaded, setLoaded] = useState(false)
  const [failed, setFailed] = useState(false)

  const label = photo.title !== '' ? photo.title : photo.file_name
  const countdown = purgeCountdown(photo.archived_at, retentionDays)

  return (
    <Card className="h-100">
      <div
        className="position-relative bg-secondary-subtle overflow-hidden"
        style={{ aspectRatio: '1 / 1' }}
      >
        <Link
          to={`/photos/${photo.uid}`}
          className="d-block w-100 h-100"
          aria-label={label}
          title={label}
        >
          {!failed && (
            <img
              src={thumbUrl(photo.uid, GRID_THUMB_SIZE)}
              alt={label}
              loading="lazy"
              decoding="async"
              onLoad={() => {
                setLoaded(true)
              }}
              onError={() => {
                setFailed(true)
              }}
              className="w-100 h-100"
              style={{
                objectFit: 'cover',
                opacity: loaded ? 1 : 0,
                transition: 'opacity 0.2s ease-in',
              }}
            />
          )}
          {failed && (
            <span className="d-flex w-100 h-100 align-items-center justify-content-center text-secondary small p-2 text-center">
              {t('library.tile.unavailable')}
            </span>
          )}
        </Link>
        <Form.Check
          type="checkbox"
          checked={selected}
          onChange={() => {
            onToggleSelect(photo.uid)
          }}
          aria-label={t('trash.selectItem', { name: label })}
          className="position-absolute top-0 start-0 m-1"
        />
        {countdown !== null && (
          <Badge
            bg={countdown.due ? 'danger' : countdown.daysLeft <= 3 ? 'warning' : 'secondary'}
            className="position-absolute bottom-0 end-0 m-1"
          >
            {countdown.due
              ? t('trash.countdown.due')
              : t('trash.countdown.days', { count: countdown.daysLeft })}
          </Badge>
        )}
      </div>
      <Card.Body className="p-2 d-flex gap-2">
        <Button
          variant="outline-secondary"
          size="sm"
          className="flex-fill"
          disabled={busy}
          onClick={() => {
            onRestore(photo.uid)
          }}
        >
          {t('trash.restore')}
        </Button>
        <Button
          variant="outline-danger"
          size="sm"
          className="flex-fill"
          disabled={busy}
          onClick={() => {
            onDelete(photo.uid)
          }}
        >
          {t('trash.deleteForever')}
        </Button>
      </Card.Body>
    </Card>
  )
}

import { useState } from 'react'
import Dropdown from 'react-bootstrap/Dropdown'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { type LabelCount } from '../../services/organize'
import { thumbUrl } from '../../services/photos'
import { FadeInImage } from '../FadeInImage'
import { Icon } from '../Icon'

/**
 * Thumbnail size for a chip's preview. The medallion is a couple of dozen pixels
 * across and a cloud draws a hundred of them at once, so it takes the smallest
 * square tile — the same one the command palette's rows use.
 */
const CHIP_THUMB_SIZE = 'tile_100'

/**
 * What an editor can do to a label from the cloud, plus which label is currently
 * mid-save. Passed as one object rather than four props because they always
 * travel together, and its absence is what tells a chip it is being read by a
 * viewer.
 */
export interface LabelChipActions {
  /** Opens the rename modal for a label. */
  onRename: (label: LabelCount) => void
  /** Asks for a label to be deleted (the page confirms first). */
  onDelete: (label: LabelCount) => void
  /** Switches a label in or out of the review game. */
  onToggleReview: (label: LabelCount, enabled: boolean) => void
  /** UID of the label whose review setting is being saved, if any. */
  savingUID: string | null
}

/** Props for {@link LabelChip}. */
export interface LabelChipProps {
  /** The label the chip stands for. */
  label: LabelCount
  /** The editor actions, or `undefined` for a read-only chip. */
  actions?: LabelChipActions
}

/**
 * One label in the cloud: a pill carrying the name, its photo count and — for an
 * editor — a "…" menu with the actions the old full-width row spread across it.
 *
 * The row's three inline controls (rename, delete, the review switch) could not
 * follow the labels into a cloud: repeated on every one of a hundred chips they
 * would cost more width than the names, which is the space the cloud exists to
 * save. They live in a per-chip menu instead, where the review setting reads as
 * the action it is ("ask about it" / "skip it") rather than as a switch whose
 * meaning has to be inferred from its position.
 *
 * A label the game skips still says so without opening the menu: it carries a
 * muted `slash-circle` beside its name, so the state stays visible at a glance
 * the way the switch used to make it.
 *
 * A label that has photos leads with one of them — the same preview an album
 * tile draws, at chip size — because a wall of a hundred identical pills is a
 * wall of text, and "Dovolená 2016" says far less than the picture behind it. A
 * label with no photo (and one whose thumbnail fails to arrive) simply keeps the
 * plain pill: there is no gap to fill, a chip is not a fixed-height row.
 */
export function LabelChip({ label, actions }: LabelChipProps) {
  const { t } = useTranslation()
  const [thumbFailed, setThumbFailed] = useState(false)
  const cover = label.cover_uid
  const showThumb = cover !== undefined && cover !== '' && !thumbFailed
  const skipped = actions !== undefined && !label.review_enabled
  // The count is a bare number on screen and the glyph says nothing out loud, so
  // the link spells both out for a screen reader instead.
  const accessibleName = [
    label.name,
    t('labels.photoCount', { count: label.photo_count }),
    ...(skipped ? [t('labels.review.off')] : []),
  ].join(', ')

  return (
    <div className="kk-label-chip">
      {/* The name may be long user data, so it truncates rather than stretching
          the chip past the cloud; the count keeps its own width beside it. */}
      <Link to={`/labels/${label.uid}`} className="kk-label-chip__link" aria-label={accessibleName}>
        {showThumb && (
          // Decoration: the link is named after the label, so a screen reader
          // reading the picture too would only say the same thing twice.
          <FadeInImage
            src={thumbUrl(cover, CHIP_THUMB_SIZE)}
            alt=""
            aria-hidden="true"
            className="kk-label-chip__thumb"
            onError={() => {
              setThumbFailed(true)
            }}
          />
        )}
        <span className="kk-label-chip__name">{label.name}</span>
        {skipped && (
          <span className="kk-label-chip__skipped" title={t('labels.review.off')}>
            <Icon name="slash-circle" />
          </span>
        )}
        <span className="kk-label-chip__count">{label.photo_count}</span>
      </Link>

      {actions && (
        <Dropdown align="end">
          <Dropdown.Toggle
            variant="link"
            id={`label-actions-${label.uid}`}
            className="kk-label-chip__menu"
            aria-label={t('labels.actions', { name: label.name })}
            title={t('labels.actions', { name: label.name })}
          >
            <Icon name="three-dots" />
          </Dropdown.Toggle>
          <Dropdown.Menu>
            <Dropdown.Item
              className="d-flex align-items-center gap-2"
              onClick={() => {
                actions.onRename(label)
              }}
            >
              <Icon name="pencil" />
              {t('labels.rename')}
            </Dropdown.Item>
            <Dropdown.Item
              className="d-flex align-items-center gap-2"
              disabled={actions.savingUID === label.uid}
              onClick={() => {
                actions.onToggleReview(label, !label.review_enabled)
              }}
            >
              <Icon name="ui-checks" />
              {label.review_enabled ? t('labels.review.disable') : t('labels.review.enable')}
            </Dropdown.Item>
            {/* Behind a divider, so a mis-tap cannot land on it from the neutral
                actions above — the same rule the header overflow menu follows. */}
            <Dropdown.Divider />
            <Dropdown.Item
              className="d-flex align-items-center gap-2 text-danger"
              onClick={() => {
                actions.onDelete(label)
              }}
            >
              <Icon name="trash" />
              {t('labels.delete')}
            </Dropdown.Item>
          </Dropdown.Menu>
        </Dropdown>
      )}
    </div>
  )
}

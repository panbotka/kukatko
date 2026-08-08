import Badge from 'react-bootstrap/Badge'
import ListGroup from 'react-bootstrap/ListGroup'
import { useTranslation } from 'react-i18next'

import { FadeInImage } from '../FadeInImage'
import { Icon } from '../Icon'

import { thumbUrl } from '../../services/photos'

/**
 * Thumbnail size for a place preview. The rendered box is 56 px square (72 on a
 * wide screen), so the small square crop is already twice the pixels a retina
 * screen draws — and this list can be hundreds of municipalities long.
 */
const PREVIEW_THUMB_SIZE = 'tile_224'

/** Props for {@link PlaceRow}. */
export interface PlaceRowProps {
  /** The place's own name, as the row's label. */
  name: string
  /** How many photos the library holds from this place. */
  count: number
  /**
   * The photo that stands for the place. Absent leaves the well showing a map-pin
   * placeholder rather than a broken image — a place can exist in the hierarchy
   * with nothing left to draw.
   */
  coverUid?: string
  /** Opens the place: the level below, or its photo grid. */
  onSelect: () => void
}

/**
 * One row of the place browse list: a preview photo, the place's name and its
 * photo count, the whole row clickable.
 *
 * The preview is the point. Places is a way through a photo library, and the list
 * used to be text — names and numbers, in a gallery, about photographs. The
 * thumbnail is the place's newest photo, chosen server-side (`cover_uid`), and it
 * is decorative here: the row's own text already names the place, so an alt text
 * would only repeat it to a screen reader.
 */
export function PlaceRow({ name, count, coverUid, onSelect }: PlaceRowProps) {
  const { t } = useTranslation()

  return (
    <ListGroup.Item
      action
      onClick={onSelect}
      className="d-flex align-items-center gap-3 kk-place-row"
    >
      <span className="kk-place-row__media flex-shrink-0">
        {coverUid === undefined || coverUid === '' ? (
          <Icon name="geo-alt" className="text-secondary" />
        ) : (
          <FadeInImage
            src={thumbUrl(coverUid, PREVIEW_THUMB_SIZE)}
            alt=""
            skeleton
            className="w-100 h-100"
            style={{ objectFit: 'cover' }}
          />
        )}
      </span>
      <span className="flex-grow-1 text-truncate">{name}</span>
      <Badge bg="secondary" pill>
        {t('places.photoCount', { count })}
      </Badge>
    </ListGroup.Item>
  )
}

import { useTranslation } from 'react-i18next'

import { formatDuration } from '../../lib/format'
import { Icon } from '../Icon'

/** What {@link MediaKindMark} needs to know about a candidate. */
export interface MediaKindMarkProps {
  /** The candidate's backend media type (`image`, `video`, `live`). */
  mediaType?: string
  /** Clip length in milliseconds, when the backend knows it. */
  durationMs?: number
  /** Extra classes for placing the mark (spacing, absolute positioning). */
  className?: string
}

/**
 * Says that a duplicate candidate is a video, and how long it is.
 *
 * Everything the duplicates screens draw is a picture: a clip is represented by
 * one poster frame, in a square tile, next to a photograph in the same square
 * tile. The decision being asked for is which of them to archive — and archiving
 * a clip throws away every frame but the one on screen. So the fact has to be on
 * the screen before the answer is given, with the duration, which is the number
 * that says how much is at stake.
 *
 * A still renders nothing: "video" is the exception worth marking, and badging
 * every photograph as a photograph would bury it.
 */
export function MediaKindMark({ mediaType, durationMs, className }: MediaKindMarkProps) {
  const { t } = useTranslation()
  if (mediaType !== 'video') {
    return null
  }
  const classes = ['badge', 'text-bg-warning', 'd-inline-flex', 'align-items-center', 'gap-1']
  if (className !== undefined) {
    classes.push(className)
  }
  return (
    <span
      className={classes.join(' ')}
      title={t('duplicates.videoTitle')}
      data-testid="media-video"
    >
      <Icon name="play-fill" />
      <span>{t('duplicates.video')}</span>
      {durationMs !== undefined && durationMs > 0 && <span>{formatDuration(durationMs)}</span>}
    </span>
  )
}

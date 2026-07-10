import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useSlideshowSettings } from '../../hooks/useSlideshowSettings'
import { formatDuration, slideshowDurationMs } from '../../lib/duration'
import { type LibraryView } from '../../lib/libraryView'
import { slideshowHref, type SlideshowScope } from '../../lib/slideshowView'

/** Props for {@link SlideshowStart}. */
export interface SlideshowStartProps {
  /** Which photos the slideshow plays: an album, a label, a search, or the library. */
  scope: SlideshowScope
  /** The current filters/sort, carried into the slideshow so it plays this view. */
  view: LibraryView
  /**
   * How many photos the slideshow will play — the total the page's list already
   * reports, not a fresh count query. Omit it while the total is still unknown:
   * the estimate is then left out rather than shown as a placeholder.
   */
  count?: number
}

/**
 * The "start slideshow" button plus, beside it, how much the reader is signing
 * up for: the photo count and the estimated running time ("40 fotek, asi 3 min
 * 20 s"). A 400-photo album at five seconds a slide is over half an hour, and
 * nothing else on the page says so.
 *
 * The estimate is the count times the persisted auto-advance interval, so it
 * follows the speed the reader last chose in the player. It renders on one line
 * next to the button and is omitted entirely when the count is not (yet) known.
 */
export function SlideshowStart({ scope, view, count }: SlideshowStartProps) {
  const { t } = useTranslation()
  const { settings } = useSlideshowSettings()

  const estimate =
    count !== undefined && count > 0
      ? t('slideshow.estimate', {
          count,
          duration: formatDuration(slideshowDurationMs(count, settings.intervalMs), t),
        })
      : null

  return (
    <span className="d-inline-flex align-items-center gap-2">
      <Link to={slideshowHref(scope, view)} className="btn btn-outline-secondary btn-sm">
        {t('slideshow.start')}
      </Link>
      {estimate !== null && <small className="text-secondary text-nowrap">{estimate}</small>}
    </span>
  )
}

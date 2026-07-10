import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { type LibraryView } from '../../lib/libraryView'
import { slideshowHref, type SlideshowScope } from '../../lib/slideshowView'

/** Props for {@link SlideshowStart}. */
export interface SlideshowStartProps {
  /** Which photos the slideshow plays: an album, a label, a search, or the library. */
  scope: SlideshowScope
  /** The current filters/sort, carried into the slideshow so it plays this view. */
  view: LibraryView
  /**
   * How many photos the slideshow will play. Still accepted so the grids can pass
   * the total they already know, but no longer shown here: the running-time
   * estimate moved into the player, beside the speed control, where it tracks the
   * show as it advances instead of sitting frozen on the start screen.
   */
  count?: number
}

/**
 * The "start slideshow" button. It carries the current scope and the view's
 * filters/sort into the player so the show plays exactly the photos on screen,
 * in the same order, and Back returns here.
 *
 * It shows no running-time estimate: that readout belongs to the player, next to
 * the speed control, so it reflects the chosen speed and counts down as the show
 * runs rather than previewing a single frozen number before it begins.
 */
export function SlideshowStart({ scope, view }: SlideshowStartProps) {
  const { t } = useTranslation()

  return (
    <Link to={slideshowHref(scope, view)} className="btn btn-outline-secondary btn-sm">
      {t('slideshow.start')}
    </Link>
  )
}

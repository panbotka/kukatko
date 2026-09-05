import { useCallback, useState } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'

import { formatDuration, slideshowDurationMs } from '../../lib/duration'
import { type LibraryView } from '../../lib/libraryView'
import {
  readSettings,
  SLIDESHOW_DEFAULTS,
  type SlideshowSettings,
  writeSettings,
} from '../../lib/slideshowSettings'
import { slideshowHref, type SlideshowScope } from '../../lib/slideshowView'

import Modal from '../Modal'
import { SlideshowSettingsForm } from './SlideshowSettingsForm'

/** Props for {@link SlideshowStart}. */
export interface SlideshowStartProps {
  /** Which photos the slideshow plays: an album, a label, a search, or the library. */
  scope: SlideshowScope
  /** The current filters/sort, carried into the slideshow so it plays this view. */
  view: LibraryView
  /**
   * How many photos the slideshow will play — the server's count for this view.
   * The dialog states how long the show will take at the chosen speed, which is
   * this count times the interval. Omitted (or unknown) means no estimate is
   * shown: a made-up figure would be worse than none.
   */
  count?: number
}

/**
 * The "start slideshow" button and the dialog it opens.
 *
 * Starting a show used to be a leap: one click and it played, at whatever speed
 * was left over from the last time, with no way to say "shuffle these" or "and
 * tell me what I'm looking at". So the click now asks first — the same settings
 * the running player offers, pre-filled with the ones last used, plus how long
 * the show will take at that speed — and only starts on confirmation.
 *
 * The dialog is edited as a **draft**: nothing is persisted until the reader
 * confirms, so dismissing it starts nothing and changes nothing, exactly as
 * cancelling should.
 *
 * The estimate is stated here, before the show, rather than only inside the
 * player: it is what tells a reader whether this is a two-minute look or a
 * forty-minute evening, and that is a decision they make *before* the lights go
 * down. The player keeps its own countdown of what is left, which answers a
 * different question.
 *
 * The button stays a link to the player's URL, so the show is still reachable by
 * middle-click or a bookmark (with the settings last saved); the ordinary click
 * is intercepted to ask first. Both entry points carry the current scope and the
 * view's filters/sort, so the show plays exactly the photos on screen, in the
 * same order, and Back returns here. All four grids render this one component,
 * so none of them can drift into offering a different set of options.
 */
export function SlideshowStart({ scope, view, count }: SlideshowStartProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  // A draft: the persisted settings are read when the dialog opens (they may have
  // changed in the player or another tab since this grid mounted) and written
  // back only if the reader starts the show.
  const [draft, setDraft] = useState<SlideshowSettings>(SLIDESHOW_DEFAULTS)
  const href = slideshowHref(scope, view)

  const openDialog = useCallback((event: React.MouseEvent): void => {
    // Leave the modified clicks alone: ctrl/cmd/shift/middle-click mean "open it
    // over there", and hijacking them would break the link's own affordances.
    if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.shiftKey) {
      return
    }
    event.preventDefault()
    setDraft(readSettings())
    setOpen(true)
  }, [])

  const change = useCallback((patch: Partial<SlideshowSettings>): void => {
    setDraft((prev) => ({ ...prev, ...patch }))
  }, [])

  const start = useCallback((): void => {
    writeSettings(draft)
    setOpen(false)
    void navigate(href)
  }, [draft, href, navigate])

  const close = useCallback((): void => {
    setOpen(false)
  }, [])

  // One pass through the photos at the chosen speed. With repeat on this is not
  // how long the show runs — it runs until somebody stops it — so it is stated as
  // what one pass takes, rather than as a total that would simply be untrue.
  const duration =
    count === undefined ? '' : formatDuration(slideshowDurationMs(count, draft.intervalMs), t)

  return (
    <>
      <Link to={href} className="btn btn-outline-secondary btn-sm" onClick={openDialog}>
        {t('slideshow.start')}
      </Link>
      <Modal show={open} onHide={close} centered>
        <Modal.Header closeButton closeLabel={t('slideshow.close')}>
          <Modal.Title as="h2" className="h5 mb-0">
            {t('slideshow.dialog.title')}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          <SlideshowSettingsForm settings={draft} onChange={change} idPrefix="slideshow-start" />
          {duration !== '' && (
            <p className="small text-secondary mt-3 mb-0">
              {draft.repeat
                ? t('slideshow.dialog.durationRepeat', { duration })
                : t('slideshow.dialog.duration', { duration })}
            </p>
          )}
        </Modal.Body>
        <Modal.Footer>
          <Button variant="outline-secondary" size="sm" onClick={close}>
            {t('confirmModal.cancel')}
          </Button>
          <Button variant="primary" size="sm" onClick={start}>
            {t('slideshow.dialog.start')}
          </Button>
        </Modal.Footer>
      </Modal>
    </>
  )
}

import { type ReactNode, useEffect } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { Icon } from '../Icon'

import './slideshow.css'

/** Props for {@link SlideshowNotice}. */
export interface SlideshowNoticeProps {
  /** What the panel says: a spinner, an {@link import('../EmptyState').EmptyState}, an error. */
  children: ReactNode
  /** Leaves the slideshow — the same exit the player's close button uses. */
  onClose: () => void
}

/**
 * The black stage with something other than a photograph on it: the show
 * loading, the show empty, the show failed.
 *
 * It exists because those three states are not "a page inside the app" — the
 * route is mounted outside the layout shell, so there is no navbar, no Back
 * link and no browser chrome around them. A reader who lands on a slideshow that
 * is slow to load used to be left with a spinner on a black screen and no way
 * off it: nothing to click, and Esc unhandled because the player that listens
 * for it had not been mounted. So the way out is part of the panel — the same
 * close button in the same corner as the player's, plus Esc — and it never
 * hides, because with nothing playing there is no picture for it to be in the
 * way of.
 */
export function SlideshowNotice({ children, onClose }: SlideshowNoticeProps) {
  const { t } = useTranslation()

  // Esc leaves, exactly as it does once the show is running. Waiting for photos
  // must not be the one state of the player the key does not work in.
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent): void => {
      if (event.key === 'Escape') {
        onClose()
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => {
      window.removeEventListener('keydown', onKeyDown)
    }
  }, [onClose])

  return (
    <div className="slideshow">
      <Button
        variant="dark"
        size="sm"
        className="slideshow__close"
        aria-label={t('slideshow.close')}
        title={t('slideshow.close')}
        onClick={onClose}
      >
        <Icon name="x-lg" />
      </Button>
      <div className="slideshow__notice">{children}</div>
    </div>
  )
}

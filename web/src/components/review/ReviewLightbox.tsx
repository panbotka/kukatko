import { type ReactNode } from 'react'
import Button from 'react-bootstrap/Button'
import Modal from 'react-bootstrap/Modal'
import { useTranslation } from 'react-i18next'

import { useKeyboardShortcuts } from '../../hooks/useKeyboardShortcuts'
import { Icon } from '../Icon'

import { ReviewStage, type ReviewStageProps } from './ReviewStage'

/** Props for {@link ReviewLightbox}. */
export interface ReviewLightboxProps {
  /** The photo to show, including the `href` its corner anchor leads to. */
  stage: ReviewStageProps
  /** A caption for the header — typically what the card said about this item. */
  title?: ReactNode
  /** Closes the overlay. */
  onClose: () => void
  /** Steps to the previous/next item of the list the overlay was opened from. */
  onPrev?: () => void
  onNext?: () => void
  /** Whether stepping would go anywhere; the controls disable when it would not. */
  hasPrev?: boolean
  hasNext?: boolean
  /** The caller's own decision buttons, shown in the footer under the photo. */
  children?: ReactNode
}

/**
 * The review tools' answer to „I am not sure": the photo big enough to actually
 * judge, over the page it was opened from, with the same decision buttons its
 * card carries so a look does not cost a trip back.
 *
 * It draws a {@link ReviewStage} — literally the review game's stage — so the
 * padded face rectangle, the measured frame and the corner anchor out to the
 * photo's page behave identically in a tool and in the game.
 *
 * Keys are bound through the app's shared {@link useKeyboardShortcuts} rather
 * than through the modal's own handling, so they live in one place with the rest
 * of the app's shortcuts and can be asserted at the document level: `Esc` closes,
 * `←`/`→` step, and `o` opens the photo's page in a new tab — the same key the
 * corner anchor advertises and the game already uses.
 *
 * The overlay holds no review state. It renders the item it is handed and calls
 * back; which item that is belongs to `useLightbox`, and what a decision means
 * belongs to the page.
 */
export function ReviewLightbox({
  stage,
  title,
  onClose,
  onPrev,
  onNext,
  hasPrev = false,
  hasNext = false,
  children,
}: ReviewLightboxProps) {
  const { t } = useTranslation()
  const { href } = stage

  useKeyboardShortcuts({
    Escape: onClose,
    ArrowLeft: () => {
      if (hasPrev) {
        onPrev?.()
      }
    },
    ArrowRight: () => {
      if (hasNext) {
        onNext?.()
      }
    },
    o: () => {
      if (href !== undefined) {
        window.open(href, '_blank', 'noopener,noreferrer')
      }
    },
  })

  return (
    <Modal
      show
      fullscreen
      onHide={onClose}
      // Escape is ours (above), so the modal must not also answer it — one
      // close per keystroke, and every shortcut on this screen in one map.
      keyboard={false}
      className="review-lightbox"
      aria-label={t('review.lightbox.title')}
    >
      <Modal.Header className="align-items-center gap-2">
        <span className="text-secondary small">{title}</span>
        <Button
          variant="outline-light"
          size="sm"
          className="ms-auto"
          onClick={onClose}
          aria-label={t('review.lightbox.close')}
          title={t('review.lightbox.close')}
        >
          <Icon name="x-lg" />
        </Button>
      </Modal.Header>

      <Modal.Body className="review-lightbox__body">
        <div className="review-lightbox__stage">
          <ReviewStage {...stage} />
        </div>

        <div className="d-flex flex-wrap align-items-center justify-content-center gap-2">
          <Button
            // `outline-light`, not `outline-secondary`: measured against the
            // overlay's own dark surface, secondary renders at rgb(78, 93, 108)
            // — present in the DOM and invisible to the eye.
            variant="outline-light"
            size="sm"
            onClick={onPrev}
            disabled={!hasPrev}
            aria-label={t('review.lightbox.prev')}
            title={t('review.lightbox.prev')}
          >
            <Icon name="chevron-left" />
          </Button>
          {children}
          <Button
            // `outline-light`, not `outline-secondary`: measured against the
            // overlay's own dark surface, secondary renders at rgb(78, 93, 108)
            // — present in the DOM and invisible to the eye.
            variant="outline-light"
            size="sm"
            onClick={onNext}
            disabled={!hasNext}
            aria-label={t('review.lightbox.next')}
            title={t('review.lightbox.next')}
          >
            <Icon name="chevron-right" />
          </Button>
        </div>
      </Modal.Body>
    </Modal>
  )
}

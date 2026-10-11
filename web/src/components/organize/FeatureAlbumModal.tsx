import { type SyntheticEvent, useEffect, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'

import {
  endOfLocalDay,
  FALLBACK_FEATURED_DEFAULT_DAYS,
  FALLBACK_FEATURED_MAX_DAYS,
  type FeatureDateBounds,
  featureDateBounds,
  isFeatured,
} from '../../lib/featured'
import { featureAlbum, fetchFeaturedAlbums } from '../../services/featured'
import { type Album } from '../../services/organize'
import Modal from '../Modal'

/** Props for {@link FeatureAlbumModal}. */
export interface FeatureAlbumModalProps {
  /** The album to feature, or whose running feature to extend. */
  album: Album
  /** Whether the modal is visible. */
  show: boolean
  /** Dismisses the modal without saving. */
  onHide: () => void
  /** Called with the refreshed album after a successful save. */
  onSaved: (album: Album) => void
}

/**
 * The "Doporučit na úvodní stránce" dialog: one date — the last day the album
 * stays on the homepage — pre-filled from the server's default length
 * (`albums.featured.default_days`, two months out of the box) and capped at its
 * one-year limit. A feature always ends; there is no "until I switch it off".
 *
 * Opened on an album that is already featured it is the "Prodloužit" dialog:
 * the same date, pre-filled with the later of the default and the current end,
 * and saved through the same request — featuring a featured album re-dates it.
 *
 * The bounds are read from `GET /albums/featured` when the dialog opens; until
 * they arrive (or if they cannot be read) the built-in fallbacks stand in, and
 * the server refuses an out-of-range date either way.
 */
export function FeatureAlbumModal({ album, show, onHide, onSaved }: FeatureAlbumModalProps) {
  const { t } = useTranslation()
  const extending = isFeatured(album)
  const [bounds, setBounds] = useState<FeatureDateBounds>(() =>
    featureDateBounds(FALLBACK_FEATURED_DEFAULT_DAYS, FALLBACK_FEATURED_MAX_DAYS, new Date()),
  )
  const [day, setDay] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(false)

  // Reset to the default whenever the dialog opens, then refine it once the
  // server's own bounds are in — a reused dialog never shows the last pick.
  useEffect(() => {
    if (!show) {
      return
    }
    const apply = (defaultDays: number, maxDays: number) => {
      const next = featureDateBounds(defaultDays, maxDays, new Date(), album.featured_until)
      setBounds(next)
      setDay(next.initial)
    }
    apply(FALLBACK_FEATURED_DEFAULT_DAYS, FALLBACK_FEATURED_MAX_DAYS)
    setError(false)
    const controller = new AbortController()
    fetchFeaturedAlbums(controller.signal)
      .then((body) => {
        apply(body.default_days, body.max_days)
      })
      .catch(() => {
        // The fallbacks already stand; the server validates the date anyway.
      })
    return () => {
      controller.abort()
    }
  }, [show, album.featured_until])

  async function save(event: SyntheticEvent) {
    event.preventDefault()
    const until = endOfLocalDay(day)
    if (until === undefined || day < bounds.min || day > bounds.max) {
      setError(true)
      return
    }
    setBusy(true)
    setError(false)
    try {
      onSaved(await featureAlbum(album.uid, until))
    } catch {
      setError(true)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal show={show} onHide={onHide} centered>
      <Form
        onSubmit={(event) => {
          void save(event)
        }}
      >
        <Modal.Header closeButton>
          <Modal.Title>{extending ? t('featured.extendTitle') : t('featured.feature')}</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {error && <p className="text-danger small">{t('featured.error')}</p>}
          <Form.Group controlId="feature-album-until">
            <Form.Label>{t('featured.until')}</Form.Label>
            <Form.Control
              type="date"
              value={day}
              min={bounds.min}
              max={bounds.max}
              required
              disabled={busy}
              onChange={(event) => {
                setDay(event.target.value)
              }}
            />
            <Form.Text>{t('featured.untilHint')}</Form.Text>
          </Form.Group>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="outline-secondary" onClick={onHide} disabled={busy}>
            {t('featured.cancel')}
          </Button>
          <Button type="submit" variant="primary" disabled={busy}>
            {extending ? t('featured.save') : t('featured.submit')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  )
}

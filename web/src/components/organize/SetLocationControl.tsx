import { useEffect, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { type UseBulkEditResult } from '../../hooks/useBulkEdit'
import { parseCoordinates } from '../../lib/coordinates'
import { ApiError } from '../../services/auth'
import {
  type BulkLocationSummary,
  bulkUpdatePhotos,
  fetchBulkLocationSummary,
} from '../../services/bulk'
import { Icon } from '../Icon'
import Modal from '../Modal'
import { LocationPicker } from '../map/LocationPicker'
import { ReasonedButton } from '../ReasonedButton'
import { useToast } from '../toast/ToastContext'

/** What to do about the selected photos that already have a location. */
type ExistingMode = 'overwrite' | 'skip'

/** The count of already-located photos, while it is being read and after. */
type SummaryState =
  | { status: 'loading' }
  | { status: 'ready'; summary: BulkLocationSummary }
  | { status: 'error' }

/** Props for {@link SetLocationControl}. */
export interface SetLocationControlProps {
  /** The bulk-edit state from `useBulkEdit`, owned by the page. */
  bulk: UseBulkEditResult
  /** Bootstrap button variant of the trigger. Defaults to `outline-secondary`. */
  variant?: string
}

/**
 * "Set location" for a whole selection: one pin, dropped once, applied to every
 * photo picked. It is the batch answer to the question the photo detail asks one
 * photo at a time, and it asks it with the very same control — `map/LocationPicker`,
 * name-the-place / type-the-numbers / click-the-map — so nothing about placing a
 * photo has to be learned twice. A box of scans is almost always one village,
 * and this is the operation that says so in one gesture instead of sixty.
 *
 * Before anything is written the dialog states how many of the selected photos
 * already have a location and lets the reader decide what happens to them:
 * overwrite them too, or leave them alone and only fill the empty ones. The
 * default is to leave them: an existing coordinate is usually evidence — a
 * camera's own GPS, or somebody's earlier decision — and a pin dropped from
 * memory over a whole selection is not worth destroying it for. The photos left
 * alone come back reported as skipped, and the toast says how many.
 *
 * The write goes through the ordinary bulk path (`POST /photos/bulk`), so the
 * batch is one transaction with one audit entry, the coordinates are stamped as
 * the user's own decision — never an estimate — and the derived work every
 * moved photo owes (the reverse geocode, the sidecar rewrite) is enqueued by the
 * backend exactly as it is for a single-photo edit.
 *
 * It is absent for a viewer, who may not write, and off — saying why — until a
 * place is actually picked.
 */
export function SetLocationControl({
  bulk,
  variant = 'outline-secondary',
}: SetLocationControlProps) {
  const { t } = useTranslation()
  const { show } = useToast()
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [coordText, setCoordText] = useState('')
  const [mode, setMode] = useState<ExistingMode>('skip')
  const [summary, setSummary] = useState<SummaryState>({ status: 'loading' })

  const { photoUids } = bulk
  // Read the count while the reader is picking the place, so the choice below the
  // map is already answered by the time they reach it. Re-read whenever the
  // dialog is opened: the selection may well have changed in between.
  useEffect(() => {
    if (!open) {
      return
    }
    const controller = new AbortController()
    setSummary({ status: 'loading' })
    fetchBulkLocationSummary(photoUids, controller.signal)
      .then((result) => {
        setSummary({ status: 'ready', summary: result })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setSummary({ status: 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [open, photoUids])

  if (!bulk.canBulkEdit) {
    return null
  }

  const parsed = parseCoordinates(coordText)
  const withLocation = summary.status === 'ready' ? summary.summary.with_location : 0
  // The choice is offered whenever it can matter: some photos are known to have a
  // location, or the count could not be read and nobody can say they do not.
  const choiceMatters = withLocation > 0 || summary.status === 'error'

  const closeDialog = () => {
    setOpen(false)
    setCoordText('')
  }

  const apply = () => {
    if (!parsed.ok) {
      return
    }
    setBusy(true)
    void bulkUpdatePhotos(photoUids, {
      set_location: {
        lat: parsed.value.lat,
        lng: parsed.value.lng,
        only_missing: choiceMatters && mode === 'skip',
      },
    })
      .then((result) => {
        const { updated, skipped } = result.counts
        show({
          variant: 'success',
          message:
            skipped > 0
              ? t('batch.location.appliedSkipped', { count: updated, skipped })
              : t('batch.location.applied', { count: updated }),
        })
        setCoordText('')
        setOpen(false)
        bulk.finish()
      })
      .catch((err: unknown) => {
        // The selection survives a failure, so the batch can be retried without
        // picking sixty tiles again.
        const message =
          err instanceof ApiError && err.message.trim() !== '' ? err.message : t('batch.error')
        show({ variant: 'danger', message })
      })
      .finally(() => {
        setBusy(false)
      })
  }

  const applyReason = busy
    ? t('batch.location.busy')
    : parsed.ok
      ? undefined
      : t('batch.location.pickFirst')

  return (
    <>
      <Button
        variant={variant}
        size="sm"
        onClick={() => {
          setOpen(true)
        }}
        title={t('batch.location.action')}
      >
        <Icon name="geo-alt" className="me-1" />
        <span>{t('batch.location.action')}</span>
      </Button>

      {/* `fullscreen="sm-down"` for the same reason the picker itself can go
          full-screen: a map being aimed at wants the whole phone. */}
      <Modal show={open} onHide={closeDialog} centered fullscreen="sm-down" size="lg">
        <Modal.Header closeButton>
          <Modal.Title as="h2" className="h5 mb-0">
            {t('batch.location.title')}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          <LocationPicker
            value={coordText}
            onChange={setCoordText}
            disabled={busy}
            idPrefix="batch-location"
          />
          <ExistingPhotos
            summary={summary}
            total={photoUids.length}
            mode={mode}
            onMode={setMode}
            disabled={busy}
          />
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={closeDialog} disabled={busy}>
            {t('batch.cancel')}
          </Button>
          <ReasonedButton variant="primary" disabledReason={applyReason} onClick={apply}>
            {busy && <Spinner animation="border" size="sm" className="me-1" aria-hidden="true" />}
            {t('batch.location.apply')}
          </ReasonedButton>
        </Modal.Footer>
      </Modal>
    </>
  )
}

/**
 * The part of the dialog about the photos that already have a location: how many
 * of them there are, and what this batch should do with them.
 *
 * The sentence comes first and the choice second, because the choice is only
 * meaningful once the number is known — "overwrite them too" says nothing until
 * the reader is told that "them" is eleven of their sixty photos. When the count
 * cannot be read the choice is still offered, defaulting to leaving existing
 * locations alone: an unknown number of overwrites is exactly the outcome not to
 * pick blindly.
 */
function ExistingPhotos({
  summary,
  total,
  mode,
  onMode,
  disabled,
}: {
  summary: SummaryState
  total: number
  mode: ExistingMode
  onMode: (mode: ExistingMode) => void
  disabled: boolean
}) {
  const { t } = useTranslation()
  if (summary.status === 'loading') {
    return (
      <p className="kk-text-caption text-secondary mb-0" aria-live="polite">
        {t('batch.location.counting')}
      </p>
    )
  }
  const failed = summary.status === 'error'
  const withLocation = failed ? 0 : summary.summary.with_location
  const counted = failed ? total : summary.summary.total
  return (
    <div aria-live="polite">
      <p className={failed ? 'text-danger mb-2' : 'mb-2'}>
        {failed
          ? t('batch.location.countError')
          : withLocation > 0
            ? t('batch.location.someHave', { count: withLocation, total: counted })
            : t('batch.location.noneHave', { count: counted })}
      </p>
      {(failed || withLocation > 0) && (
        <Form.Group controlId="batch-location-mode">
          <Form.Label className="kk-text-caption mb-1">{t('batch.location.modeLabel')}</Form.Label>
          <Form.Check
            type="radio"
            id="batch-location-skip"
            name="batch-location-mode"
            label={t('batch.location.skip')}
            checked={mode === 'skip'}
            disabled={disabled}
            onChange={() => {
              onMode('skip')
            }}
          />
          <Form.Check
            type="radio"
            id="batch-location-overwrite"
            name="batch-location-mode"
            label={t('batch.location.overwrite')}
            checked={mode === 'overwrite'}
            disabled={disabled}
            onChange={() => {
              onMode('overwrite')
            }}
          />
        </Form.Group>
      )}
    </div>
  )
}

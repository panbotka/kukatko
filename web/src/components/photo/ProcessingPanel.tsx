import { useEffect, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { formatDateTimeMinutes } from '../../lib/format'
import { ApiError } from '../../services/auth'
import {
  type PhotoProcessing,
  type ProcessingState,
  type ProcessingStep,
  runProcessingStep,
} from '../../services/photos'
import { Icon, type IconName } from '../Icon'

/** Props for {@link ProcessingPanel}. */
export interface ProcessingPanelProps {
  /** UID of the photo the report belongs to. */
  uid: string
  /** The report as the detail response gave it, one entry per step in a fixed order. */
  steps: PhotoProcessing[]
  /**
   * Whether the current user may schedule a step (maintainers only). Scheduling
   * background work is an operations action, so nobody else sees the buttons.
   */
  canRun: boolean
}

/** The glyph each state wears. */
const STATE_ICONS: Record<ProcessingState, IconName> = {
  done: 'check-lg',
  running: 'arrow-clockwise',
  queued: 'clock-history',
  failed: 'exclamation-triangle',
  skipped: 'slash-circle',
  pending: 'dash-lg',
}

/**
 * The colour each state wears. `done` is deliberately the quiet one: a library
 * where everything has been computed should read as calm, not as a wall of green
 * ticks demanding attention, so only the states that mean something is happening
 * — or has gone wrong — take a colour.
 */
const STATE_TONES: Record<ProcessingState, string> = {
  done: 'text-success',
  running: 'text-primary',
  queued: 'text-info',
  failed: 'text-danger',
  skipped: 'text-body-secondary',
  pending: 'text-body-secondary',
}

/**
 * What the library has already computed about this photo: the metadata read out
 * of its file, its thumbnails, its embedding, its faces, the text printed in it,
 * the place its coordinate names and its metadata sidecar. One row per step, in
 * the order the pipeline reaches them.
 *
 * It answers the question the detail page could not before — "why does this
 * photo not come up in search / show no faces?" — with a fact rather than a
 * guess: the step is waiting for the AI box, or it failed and here is why, or it
 * cannot apply to this photo at all. A step that ran and found nothing (no face,
 * no text) reads as the success it is, never as a gap.
 *
 * A maintainer additionally gets a "run now" button on every step that is
 * neither done nor skipped, which schedules that one step for that one photo.
 * The queue's dedup index absorbs a double click, so the button is safe to press
 * twice.
 */
export function ProcessingPanel({ uid, steps, canRun }: ProcessingPanelProps) {
  const { t, i18n } = useTranslation()
  const [rows, setRows] = useState<PhotoProcessing[]>(steps)
  const [running, setRunning] = useState<ProcessingStep | null>(null)
  const [failure, setFailure] = useState<string | null>(null)

  // The report belongs to the photo: paging to the next one replaces it wholesale
  // rather than leaving the previous photo's states on screen.
  useEffect(() => {
    setRows(steps)
    setRunning(null)
    setFailure(null)
  }, [steps])

  const run = async (step: ProcessingStep) => {
    setRunning(step)
    setFailure(null)
    try {
      const updated = await runProcessingStep(uid, step)
      setRows((current) => current.map((row) => (row.step === step ? updated : row)))
    } catch (err) {
      setFailure(
        err instanceof ApiError && err.status === 409
          ? t('photo.processing.runNotApplicable')
          : t('photo.processing.runError'),
      )
    } finally {
      setRunning(null)
    }
  }

  /**
   * The muted line under a step's name: when the work landed and what it found,
   * or in one word where it stands.
   */
  const summary = (row: PhotoProcessing): string => {
    if (row.state !== 'done') {
      return t(`photo.processing.states.${row.state}`)
    }
    const when =
      row.at === undefined
        ? t('photo.processing.states.done')
        : formatDateTimeMinutes(row.at, i18n.language)
    if (row.step === 'face_detect' && row.face_count !== undefined) {
      return `${when} · ${t('photo.processing.faceCount', { count: row.face_count })}`
    }
    if (row.step === 'ocr' && row.text_found !== undefined) {
      const text = row.text_found ? t('photo.processing.textFound') : t('photo.processing.textNone')
      return `${when} · ${text}`
    }
    return when
  }

  return (
    <div>
      <ul className="list-unstyled mb-0">
        {rows.map((row) => {
          const runnable = canRun && row.state !== 'done' && row.state !== 'skipped'
          const pending = running === row.step
          return (
            <li key={row.step} className="d-flex align-items-start gap-2 py-1">
              <Icon name={STATE_ICONS[row.state]} className={`${STATE_TONES[row.state]} mt-1`} />
              <span className="flex-grow-1">
                <span className="d-block">{t(`photo.processing.steps.${row.step}`)}</span>
                <span className="d-block small text-secondary">{summary(row)}</span>
                {row.state === 'failed' && row.error !== undefined && row.error !== '' && (
                  <span className="d-block small text-danger text-break">{row.error}</span>
                )}
              </span>
              {runnable && (
                <Button
                  variant="outline-secondary"
                  size="sm"
                  // The row's text can be long (a failed step prints the job's
                  // error), and a flex item that may shrink turns the button into
                  // two stacked lines. It keeps its own width; the text wraps.
                  className="flex-shrink-0 text-nowrap"
                  disabled={pending}
                  title={t('photo.processing.runNow')}
                  onClick={() => {
                    void run(row.step)
                  }}
                >
                  {pending ? (
                    <Spinner animation="border" size="sm" className="me-1" aria-hidden="true" />
                  ) : (
                    <Icon name="play-fill" className="me-1" />
                  )}
                  {t('photo.processing.runNow')}
                </Button>
              )}
            </li>
          )
        })}
      </ul>
      {failure !== null && (
        <div className="text-danger small mt-1" role="alert">
          {failure}
        </div>
      )}
    </div>
  )
}

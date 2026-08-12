import { type ReactNode } from 'react'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'

import {
  SLIDESHOW_EFFECTS,
  SLIDESHOW_INTERVALS_MS,
  type SlideshowEffect,
  type SlideshowSettings,
} from '../../lib/slideshowSettings'

/** Props for {@link SlideshowSettingsForm}. */
export interface SlideshowSettingsFormProps {
  /** The settings being edited. */
  settings: SlideshowSettings
  /** Applies one changed field. The caller decides whether it persists. */
  onChange: (patch: Partial<SlideshowSettings>) => void
  /**
   * Prefix for the control ids, so the dialog's copy and the player's can both
   * exist in one document without two controls sharing an id (which would point
   * every label at the first of them).
   */
  idPrefix: string
  /**
   * An optional note rendered on the speed control's own row — the player puts
   * the remaining running time there. The dialog states its estimate below the
   * form instead, where a whole sentence fits.
   */
  speedNote?: ReactNode
}

/** The caption toggles, each pairing a settings field with its label key. */
const CAPTION_TOGGLES = [
  { field: 'showTitle', labelKey: 'slideshow.captions.title' },
  { field: 'showDescription', labelKey: 'slideshow.captions.description' },
  { field: 'showDate', labelKey: 'slideshow.captions.date' },
] as const

/**
 * The slideshow's settings, as one form: the transition, the per-photo speed,
 * repeat and shuffle, and the three caption toggles.
 *
 * It is deliberately the *only* place these controls exist. The start dialog and
 * the running player both render this component, so a setting cannot be offered
 * in one and missing from the other — the drift the two surfaces would otherwise
 * accumulate. Neither surface owns the values: both hand down `settings` and take
 * back a patch, which is what lets the dialog edit a draft it may discard while
 * the player writes through to the running show.
 */
export function SlideshowSettingsForm({
  settings,
  onChange,
  idPrefix,
  speedNote,
}: SlideshowSettingsFormProps) {
  const { t } = useTranslation()

  return (
    <div className="d-grid gap-3">
      <Form.Group controlId={`${idPrefix}-effect`}>
        <Form.Label className="small mb-1">{t('slideshow.effect.label')}</Form.Label>
        <Form.Select
          size="sm"
          value={settings.effect}
          onChange={(e) => {
            onChange({ effect: e.target.value as SlideshowEffect })
          }}
        >
          {SLIDESHOW_EFFECTS.map((effect) => (
            <option key={effect} value={effect}>
              {t(`slideshow.effect.${effect}`)}
            </option>
          ))}
        </Form.Select>
      </Form.Group>

      <Form.Group controlId={`${idPrefix}-speed`}>
        <div className="d-flex justify-content-between align-items-baseline gap-2 mb-1">
          <Form.Label className="small mb-0">{t('slideshow.speed.label')}</Form.Label>
          {speedNote !== undefined && (
            <span className="small text-secondary text-nowrap">{speedNote}</span>
          )}
        </div>
        <Form.Select
          size="sm"
          value={String(settings.intervalMs)}
          onChange={(e) => {
            onChange({ intervalMs: Number(e.target.value) })
          }}
        >
          {SLIDESHOW_INTERVALS_MS.map((ms) => (
            <option key={ms} value={ms}>
              {t('slideshow.speed.seconds', { seconds: Math.round(ms / 1000) })}
            </option>
          ))}
        </Form.Select>
      </Form.Group>

      <div>
        <Form.Check
          type="switch"
          id={`${idPrefix}-repeat`}
          className="small"
          label={t('slideshow.repeat.label')}
          checked={settings.repeat}
          onChange={(e) => {
            onChange({ repeat: e.target.checked })
          }}
        />
        <Form.Check
          type="switch"
          id={`${idPrefix}-shuffle`}
          className="small"
          label={t('slideshow.shuffle.label')}
          checked={settings.shuffle}
          onChange={(e) => {
            onChange({ shuffle: e.target.checked })
          }}
        />
      </div>

      <fieldset>
        <legend className="small mb-1 fs-6">{t('slideshow.captions.label')}</legend>
        {CAPTION_TOGGLES.map(({ field, labelKey }) => (
          <Form.Check
            key={field}
            type="switch"
            id={`${idPrefix}-${field}`}
            className="small"
            label={t(labelKey)}
            checked={settings[field]}
            onChange={(e) => {
              onChange({ [field]: e.target.checked })
            }}
          />
        ))}
      </fieldset>
    </div>
  )
}

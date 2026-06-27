import { type SyntheticEvent, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'

import { type FaceView, type Suggestion } from '../../services/people'

/** Props for {@link FaceAssignPanel}. */
export interface FaceAssignPanelProps {
  /** The face being named. */
  face: FaceView
  /** True while an assignment request is in flight (disables the controls). */
  busy: boolean
  /** Accepts a suggested identity with one tap. */
  onAcceptSuggestion: (suggestion: Suggestion) => void
  /** Assigns a free-text name (found or created server-side). */
  onAssignName: (name: string) => void
  /** Clears the current assignment (only shown when the face is named). */
  onUnassign: () => void
  /** Dismisses the panel. */
  onClose: () => void
}

/** Formats a 0..1 confidence as a whole-percent string for display. */
function confidencePct(confidence: number): string {
  return `${Math.round(confidence * 100)}%`
}

/**
 * The assignment controls for a single selected face: the current identity (with
 * an unassign action), one-tap suggestion buttons ranked by confidence, and a
 * free-text name field that finds or creates a subject. Touch-friendly — every
 * action is a full-size button.
 */
export function FaceAssignPanel({
  face,
  busy,
  onAcceptSuggestion,
  onAssignName,
  onUnassign,
  onClose,
}: FaceAssignPanelProps) {
  const { t } = useTranslation()
  const [name, setName] = useState('')

  function handleSubmit(event: SyntheticEvent) {
    event.preventDefault()
    const trimmed = name.trim()
    if (trimmed !== '') {
      onAssignName(trimmed)
    }
  }

  const assigned = face.subject_name !== undefined && face.subject_name !== ''

  return (
    <div className="border rounded p-3 mt-2" aria-label={t('faces.panel.title')}>
      <div className="d-flex justify-content-between align-items-start mb-2">
        <strong>
          {assigned
            ? t('faces.panel.assignedTo', { name: face.subject_name })
            : t('faces.panel.title')}
        </strong>
        <Button
          variant="outline-secondary"
          size="sm"
          onClick={onClose}
          aria-label={t('faces.panel.close')}
        >
          ✕
        </Button>
      </div>

      {assigned && (
        <Button
          variant="outline-danger"
          size="sm"
          className="mb-2"
          disabled={busy}
          onClick={onUnassign}
        >
          {t('faces.panel.unassign')}
        </Button>
      )}

      {face.suggestions.length > 0 && (
        <div className="mb-2">
          <p className="small text-secondary mb-1">{t('faces.panel.suggestions')}</p>
          <div className="d-flex flex-wrap gap-2">
            {face.suggestions.map((suggestion) => (
              <Button
                key={suggestion.subject_uid}
                variant="outline-primary"
                size="sm"
                disabled={busy}
                onClick={() => {
                  onAcceptSuggestion(suggestion)
                }}
              >
                {suggestion.subject_name} · {confidencePct(suggestion.confidence)}
              </Button>
            ))}
          </div>
        </div>
      )}

      <Form onSubmit={handleSubmit}>
        <Form.Label htmlFor="face-name-input" className="small text-secondary mb-1">
          {t('faces.panel.nameLabel')}
        </Form.Label>
        <div className="d-flex gap-2">
          <Form.Control
            id="face-name-input"
            type="text"
            value={name}
            placeholder={t('faces.panel.namePlaceholder')}
            disabled={busy}
            onChange={(event) => {
              setName(event.target.value)
            }}
          />
          <Button type="submit" variant="primary" disabled={busy || name.trim() === ''}>
            {t('faces.panel.assign')}
          </Button>
        </div>
      </Form>
    </div>
  )
}

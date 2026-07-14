import { useState } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { isNamed } from '../../lib/faceState'
import { type FaceView, type SubjectCount, type Suggestion } from '../../services/people'
import { AddAutocomplete } from '../photo/AddAutocomplete'

/** The identity an assignment names a face with. */
export type SubjectChoice = Pick<Suggestion, 'subject_uid' | 'subject_name'>

/** Props for {@link FaceAssignPanel}. */
export interface FaceAssignPanelProps {
  /** The face being named. */
  face: FaceView
  /** Every subject in the library, for the typeahead. */
  subjects: SubjectCount[]
  /** True while the subject list is still loading (the typeahead waits for it). */
  subjectsLoading?: boolean
  /** True while an assignment request is in flight (disables the controls). */
  busy: boolean
  /** Names the face with an existing subject — a ranked suggestion or a typeahead pick. */
  onAcceptSuggestion: (subject: SubjectChoice) => void
  /** Assigns a free-text name (the subject is found or created server-side). */
  onAssignName: (name: string) => void
  /** Clears the current assignment (only shown when the face is named). */
  onUnassign: () => void
  /** Dismisses the panel (deselects the face). */
  onClose: () => void
}

/** How many ranked suggestions to offer: past the third, confidence is guesswork. */
const MAX_SUGGESTIONS = 3

/** Formats a 0..1 confidence as a whole-percent string for display. */
function confidencePct(confidence: number): string {
  return `${Math.round(confidence * 100)}%`
}

/**
 * The assignment controls for a single selected face: one-tap suggestion buttons
 * ranked by similarity to the faces already named in the library, and a typeahead
 * that names it with an existing person or creates a new one.
 *
 * An assigned face shows who it names, and can be reassigned — the backend ranks
 * alternatives for it too, with the person it already names excluded — or cleared.
 * Reassignment is a mode rather than the default view, so a correct name is never
 * one stray click from being replaced.
 */
export function FaceAssignPanel({
  face,
  subjects,
  subjectsLoading = false,
  busy,
  onAcceptSuggestion,
  onAssignName,
  onUnassign,
  onClose,
}: FaceAssignPanelProps) {
  const { t } = useTranslation()
  const [reassigning, setReassigning] = useState(false)

  const assigned = isNamed(face)
  const naming = !assigned || reassigning
  const suggestions = face.suggestions.slice(0, MAX_SUGGESTIONS)

  return (
    <div
      className="border rounded p-3 mt-2"
      aria-label={t('faces.panel.title')}
      onKeyDown={(event) => {
        if (event.key !== 'Escape') {
          return
        }
        // Escape backs out one step at a time: first out of reassignment (keeping
        // the name that is already there), then out of the face itself.
        event.stopPropagation()
        if (reassigning) {
          setReassigning(false)
        } else {
          onClose()
        }
      }}
    >
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
        <div className="d-flex gap-2 mb-2">
          <Button
            variant="outline-primary"
            size="sm"
            disabled={busy}
            onClick={() => {
              setReassigning(!reassigning)
            }}
          >
            {reassigning ? t('faces.panel.cancelReassign') : t('faces.panel.reassign')}
          </Button>
          <Button variant="outline-danger" size="sm" disabled={busy} onClick={onUnassign}>
            {t('faces.panel.unassign')}
          </Button>
        </div>
      )}

      {naming && (
        <>
          {suggestions.length > 0 && (
            <div className="mb-2">
              <p className="small text-secondary mb-1">{t('faces.panel.suggestions')}</p>
              <div className="d-flex flex-wrap gap-2">
                {suggestions.map((suggestion) => (
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

          <AddAutocomplete
            id={`face-name-${face.face_index}`}
            label={t('faces.panel.nameLabel')}
            autoFocus
            disabled={busy || subjectsLoading}
            options={subjects.map((subject) => ({
              uid: subject.uid,
              label: subject.name,
              hint: String(subject.marker_count),
            }))}
            onAdd={(uid) => {
              const subject = subjects.find((candidate) => candidate.uid === uid)
              onAcceptSuggestion({ subject_uid: uid, subject_name: subject?.name ?? '' })
            }}
            onCreate={(name) => {
              // The assignment is optimistic and its failure surfaces as the panel's
              // error alert, so the field may clear right away.
              onAssignName(name)
              return Promise.resolve(true)
            }}
          />
        </>
      )}
    </div>
  )
}

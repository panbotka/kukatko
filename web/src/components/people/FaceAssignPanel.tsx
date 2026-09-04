import { useState } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { type Frame, padBbox, squareCrop } from '../../lib/faceGeometry'
import { hasEmbedding, isNamed } from '../../lib/faceState'
import { type FaceView, type SubjectCount, type Suggestion } from '../../services/people'
import { Icon } from '../Icon'
import { AddAutocomplete } from '../photo/AddAutocomplete'
import { FaceCrop } from './FaceCrop'

/** The identity an assignment names a face with. */
export type SubjectChoice = Pick<Suggestion, 'subject_uid' | 'subject_name'>

/**
 * The edge length of the panel's face crop, in CSS pixels. Deliberately far
 * larger than the 44px thumbnail on the row above it: that one is a
 * cross-reference ("this row, that box"), this one is the evidence — on a group
 * photograph the marker itself can be a dozen pixels across, and nobody should be
 * asked to put a name to a face they cannot see.
 */
const PANEL_FACE_SIZE = 96

/**
 * How much context the panel's crop keeps around the face box — the review
 * card's 30 %. A crop cut tight to the detector's rectangle is a mask, not a
 * person; the padding gives back the hair and the chin that make somebody
 * recognisable.
 */
const PANEL_FACE_PADDING = 0.3

/** Props for {@link FaceAssignPanel}. */
export interface FaceAssignPanelProps {
  /** The face being named. */
  face: FaceView
  /** The photo the face is on — the crop is cut from its thumbnail. */
  photoUid: string
  /**
   * The photo's display frame (after EXIF orientation), or null while it is still
   * unknown. Without it the crop is left out rather than guessed: a crop squared
   * against the wrong frame shows a stretched stranger.
   */
  frame: Frame | null
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
 *
 * A face with no embedding says so: it is the honest explanation of an empty
 * suggestion list (there is nothing to rank neighbours against), and it tells the
 * reader that this panel, by hand, is the *only* way that face will ever be named.
 *
 * **The panel leads with an enlarged crop of the face it is naming.** The
 * marker on the photograph is only as big as the face, and on a crowd that is a
 * handful of pixels — the highlight says *which* face, this says *who*. It costs
 * no extra download: it is a region of the same thumbnail the page already has
 * (`FaceCrop`), and it is cut square in pixel space (`squareCrop`) so nobody is
 * shown a stretched version of themselves.
 */
export function FaceAssignPanel({
  face,
  photoUid,
  frame,
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
  const embedded = hasEmbedding(face)

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
      <div className="d-flex align-items-start gap-3 mb-2">
        {frame !== null && (
          <FaceCrop
            photoUid={photoUid}
            crop={squareCrop(padBbox(face.bbox, PANEL_FACE_PADDING), frame)}
            frame={frame}
            label={t('faces.panel.crop')}
            size={PANEL_FACE_SIZE}
            className="rounded flex-shrink-0"
          />
        )}
        <div className="d-flex justify-content-between align-items-start flex-grow-1 kk-min-w-0">
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
            title={t('faces.panel.close')}
          >
            ✕
          </Button>
        </div>
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

      {!embedded && (
        <p className="small text-secondary d-flex gap-2 mb-2">
          <Icon name="slash-circle" className="mt-1" />
          <span>{t('faces.noEmbedding.note')}</span>
        </p>
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
              // Naming a face is face work, so the hint counts faces, not photos.
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

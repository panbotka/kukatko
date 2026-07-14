import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { useSubjects } from '../../hooks/useSubjects'
import { type UseFacesResult } from '../../hooks/useFaces'
import { type FaceState, faceState } from '../../lib/faceState'
import { Icon } from '../Icon'
import { FaceAssignPanel } from './FaceAssignPanel'

/** Props for {@link FacesPanel}. */
export interface FacesPanelProps {
  /** The faces state machine, shared with the overlay drawn on the photo. */
  faces: UseFacesResult
  /** Whether the viewer may assign people (editors and admins). */
  canWrite: boolean
  /** The `face_index` hovered on the photo, or null. Highlights its row. */
  hovered: number | null
  /** Reports the hovered row, so the overlay can highlight its box. */
  onHover: (faceIndex: number | null) => void
  /** Closes the panel (same as toggling faces off). */
  onClose: () => void
}

/** Chip style per naming state — the same colours the boxes on the photo use. */
const STATE_CHIP: Record<FaceState, string> = {
  assigned: 'text-bg-success',
  unassigned: 'text-bg-warning',
  unmatched: 'text-bg-danger',
}

/** The i18n key naming what a not-yet-assigned face is still missing. */
const STATE_KEY = {
  unassigned: 'faces.state.unassigned',
  unmatched: 'faces.state.detected',
} as const

/**
 * The faces sidebar of the photo detail: one row per detected face, and the
 * assignment controls for the selected one. It appears beside the photo (below it
 * on a phone) whenever the face boxes are shown, and is the only place people are
 * named — the boxes on the photo and these rows drive the same selection, so
 * clicking either one gets you there.
 *
 * Rows are numbered by position, matching the number drawn on each box: `face_index`
 * cannot be used, as markers with no detected face carry negative ones.
 */
export function FacesPanel({ faces, canWrite, hovered, onHover, onClose }: FacesPanelProps) {
  const { t } = useTranslation()
  const { subjects, loading: subjectsLoading } = useSubjects()

  const selected = faces.selected

  return (
    <Card>
      <Card.Header className="d-flex justify-content-between align-items-center">
        <span>{t('faces.count', { count: faces.faces.length })}</span>
        <Button
          variant="link"
          size="sm"
          className="p-0 text-reset text-decoration-none"
          aria-label={t('faces.panel.closePanel')}
          onClick={onClose}
        >
          <Icon name="x-lg" />
        </Button>
      </Card.Header>
      <Card.Body style={{ maxHeight: '80vh', overflowY: 'auto' }}>
        {faces.actionError && <Alert variant="danger">{t('faces.assignError')}</Alert>}
        {faces.status === 'loading' && (
          <Spinner animation="border" size="sm" role="status">
            <span className="visually-hidden">{t('faces.loading')}</span>
          </Spinner>
        )}
        {faces.status === 'error' && <Alert variant="danger">{t('faces.error')}</Alert>}

        <div className="list-group list-group-flush">
          {faces.faces.map((face, position) => {
            const state = faceState(face)
            const number = position + 1
            const isSelected = selected?.face_index === face.face_index
            const chip = state === 'assigned' ? (face.subject_name ?? '') : t(STATE_KEY[state])
            const row = (
              <>
                <span className="fw-medium">{t('faces.row.label', { number })}</span>
                <span className={`badge ms-2 text-truncate ${STATE_CHIP[state]}`}>{chip}</span>
              </>
            )

            return (
              <div key={face.face_index}>
                {canWrite ? (
                  <button
                    type="button"
                    className={`list-group-item list-group-item-action d-flex align-items-center ${
                      isSelected ? 'active' : ''
                    } ${hovered === face.face_index && !isSelected ? 'bg-body-secondary' : ''}`}
                    aria-pressed={isSelected}
                    aria-label={t('faces.row.select', { number })}
                    data-face-state={state}
                    onClick={() => {
                      faces.select(isSelected ? null : face.face_index)
                    }}
                    onMouseEnter={() => {
                      onHover(face.face_index)
                    }}
                    onMouseLeave={() => {
                      onHover(null)
                    }}
                  >
                    {row}
                  </button>
                ) : (
                  <div
                    className="list-group-item d-flex align-items-center"
                    data-face-state={state}
                  >
                    {row}
                  </div>
                )}

                {canWrite && isSelected && (
                  <FaceAssignPanel
                    // Remounting on selection change resets the reassign mode and
                    // the typed name, so no state leaks from the previous face.
                    key={face.face_index}
                    face={face}
                    subjects={subjects}
                    subjectsLoading={subjectsLoading}
                    busy={faces.busy}
                    onAcceptSuggestion={(subject) => {
                      faces.acceptSuggestion(face, subject)
                    }}
                    onAssignName={(name) => {
                      faces.assignName(face, name)
                    }}
                    onUnassign={() => {
                      faces.unassign(face)
                    }}
                    onClose={() => {
                      faces.select(null)
                    }}
                  />
                )}
              </div>
            )
          })}
        </div>
      </Card.Body>
    </Card>
  )
}

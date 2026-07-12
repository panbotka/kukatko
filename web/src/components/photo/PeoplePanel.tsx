import Alert from 'react-bootstrap/Alert'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { type UseFacesResult } from '../../hooks/useFaces'
import { type FaceView } from '../../services/people'
import { ENTITY_STYLE } from '../entityStyle'
import { Icon } from '../Icon'
import { FaceAssignPanel } from '../people/FaceAssignPanel'

/** Props for {@link PeoplePanel}. */
export interface PeoplePanelProps {
  /** The face state machine shared with the on-image overlay ({@link UseFacesResult}). */
  faces: UseFacesResult
  /** Whether the current user may name/clear people (editor/admin). */
  canWrite: boolean
  /**
   * True while a neighbour photo is loading: the faces belong to the target uid,
   * not the photo still on screen, so the chips are held back to a spinner rather
   * than showing another photo's people.
   */
  loading?: boolean
}

/** Whether a detected face has been assigned to a named person. */
function isNamed(face: FaceView): boolean {
  return face.subject_name !== undefined && face.subject_name !== ''
}

/**
 * The People sub-block of the Organize card: the photo's detected faces as person
 * chips (rose, like every other person chip in the app), reusing the same
 * {@link useFaces} state machine that drives the on-image overlay so a face picked
 * on the photo and a chip picked here stay in sync. For an editor each chip is a
 * button that opens the {@link FaceAssignPanel} to assign, rename or remove the
 * person; a viewer sees the named people read-only. Named faces are rose chips,
 * unassigned detections are neutral chips an editor can still name.
 */
export function PeoplePanel({ faces, canWrite, loading = false }: PeoplePanelProps) {
  const { t } = useTranslation()
  const busyLoading = loading || faces.status === 'loading'
  const selected = faces.selected
  // Viewers only care about the people who have a name; an editor also sees the
  // unnamed detections so they can name them.
  const visible = canWrite ? faces.faces : faces.faces.filter(isNamed)

  return (
    <div>
      <div className="small text-secondary mb-1">{t('photo.organize.people')}</div>

      {faces.actionError && (
        <Alert variant="danger" className="py-2 small">
          {t('faces.assignError')}
        </Alert>
      )}

      {busyLoading ? (
        <Spinner animation="border" size="sm" role="status">
          <span className="visually-hidden">{t('faces.loading')}</span>
        </Spinner>
      ) : (
        <div className="d-flex flex-wrap gap-2 mb-2">
          {visible.length === 0 && (
            <span className="text-secondary small">{t('photo.organize.noPeople')}</span>
          )}
          {visible.map((face) => {
            const named = isNamed(face)
            const text = named
              ? (face.subject_name ?? '')
              : t('faces.unnamed', { index: face.face_index + 1 })
            const chipClass = `badge rounded-pill d-inline-flex align-items-center gap-1 ${
              named ? ENTITY_STYLE.person.className : 'text-bg-secondary'
            }`
            if (!canWrite) {
              return (
                <span key={face.face_index} className={chipClass}>
                  <Icon name={ENTITY_STYLE.person.icon} />
                  {text}
                </span>
              )
            }
            return (
              <button
                key={face.face_index}
                type="button"
                className={`${chipClass} border-0`}
                aria-pressed={selected?.face_index === face.face_index}
                aria-label={
                  named
                    ? t('photo.organize.editPerson', { name: text })
                    : t('photo.organize.namePerson', { index: face.face_index + 1 })
                }
                onClick={() => {
                  faces.select(face.face_index)
                }}
              >
                <Icon name={ENTITY_STYLE.person.icon} />
                {text}
              </button>
            )
          })}
        </div>
      )}

      {canWrite && selected !== null && (
        <FaceAssignPanel
          face={selected}
          busy={faces.busy}
          onAcceptSuggestion={(suggestion) => {
            faces.acceptSuggestion(selected, suggestion)
          }}
          onAssignName={(name) => {
            faces.assignName(selected, name)
          }}
          onUnassign={() => {
            faces.unassign(selected)
          }}
          onClose={() => {
            faces.select(null)
          }}
        />
      )}
    </div>
  )
}

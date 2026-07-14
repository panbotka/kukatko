import Alert from 'react-bootstrap/Alert'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { type UseFacesResult } from '../../hooks/useFaces'
import { isNamed } from '../../lib/faceState'
import { ENTITY_STYLE } from '../entityStyle'
import { Icon } from '../Icon'

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
  /**
   * Called with a face's `face_index` when an editor clicks its chip: the page
   * shows the faces panel and selects that face there. Assignment lives in exactly
   * one place, and these chips are the way to reach it without knowing about `m`.
   */
  onEditFace: (faceIndex: number) => void
}

/**
 * The People sub-block of the Organize card: the photo's detected faces as person
 * chips (rose, like every other person chip in the app), reusing the same
 * {@link useFaces} state machine that drives the on-image overlay. It answers "who
 * is in this photo" without turning the face boxes on — they are off by default —
 * and an editor's click on a chip opens the faces panel at that face. Named faces
 * are rose chips, unassigned detections neutral chips an editor can still name; a
 * viewer sees only the named people, read-only.
 *
 * Chips are numbered by position, matching the numbers on the boxes and in the
 * faces panel: `face_index` is negative for markers with no detected face.
 */
export function PeoplePanel({ faces, canWrite, loading = false, onEditFace }: PeoplePanelProps) {
  const { t } = useTranslation()
  const busyLoading = loading || faces.status === 'loading'
  const selected = faces.selected
  // Viewers only care about the people who have a name; an editor also sees the
  // unnamed detections so they can name them.
  const visible = faces.faces
    .map((face, position) => ({ face, number: position + 1 }))
    .filter(({ face }) => canWrite || isNamed(face))

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
          {visible.map(({ face, number }) => {
            const named = isNamed(face)
            const text = named ? (face.subject_name ?? '') : t('faces.unnamed', { index: number })
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
                    : t('photo.organize.namePerson', { index: number })
                }
                onClick={() => {
                  onEditFace(face.face_index)
                }}
              >
                <Icon name={ENTITY_STYLE.person.icon} />
                {text}
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}

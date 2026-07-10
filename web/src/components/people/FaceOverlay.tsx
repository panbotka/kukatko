import { useTranslation } from 'react-i18next'

import { faceBoxStyle } from '../../lib/faceGeometry'
import { type FaceView } from '../../services/people'

/** Props for {@link FaceOverlay}. */
export interface FaceOverlayProps {
  /** The detected faces to draw, from {@link useFaces}. */
  faces: FaceView[]
  /** The `face_index` whose naming panel is open, or null when none is. */
  selected: number | null
  /** Selects a face (opens its naming panel). Never called when read-only. */
  onSelect: (faceIndex: number) => void
  /**
   * When true the boxes are drawn with their names but cannot be selected, for
   * viewers who may not assign people. Defaults false.
   */
  readOnly?: boolean
}

/**
 * A transparent layer of clickable face boxes, positioned from normalised bboxes
 * so it stays correct at any rendered size. It draws no image of its own: mount
 * it as the last child of the element that wraps the photo (a
 * `position-relative` box tight around the `<img>`) and the boxes land on the
 * faces. The layer itself is click-through, so only the boxes intercept
 * pointer events — and when read-only not even those, leaving the image below
 * fully clickable.
 */
export function FaceOverlay({ faces, selected, onSelect, readOnly = false }: FaceOverlayProps) {
  const { t } = useTranslation()

  return (
    <div
      className="position-absolute top-0 start-0 w-100 h-100"
      style={{ pointerEvents: 'none' }}
      data-testid="face-overlay"
    >
      {faces.map((face) => {
        const named = face.subject_name !== undefined && face.subject_name !== ''
        const label = named
          ? (face.subject_name ?? '')
          : t('faces.unnamed', { index: face.face_index + 1 })
        return (
          <button
            key={face.face_index}
            type="button"
            aria-label={label}
            title={label}
            aria-pressed={selected === face.face_index}
            disabled={readOnly}
            onClick={() => {
              onSelect(face.face_index)
            }}
            className="position-absolute p-0 border-2"
            style={{
              ...faceBoxStyle(face.bbox),
              borderStyle: 'solid',
              borderColor: named ? 'var(--bs-success)' : 'var(--bs-warning)',
              background: 'transparent',
              cursor: readOnly ? 'default' : 'pointer',
              pointerEvents: readOnly ? 'none' : 'auto',
            }}
          />
        )
      })}
    </div>
  )
}

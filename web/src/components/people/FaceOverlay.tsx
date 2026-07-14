import { type CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'

import { faceBoxStyle } from '../../lib/faceGeometry'
import { type FaceState, faceState } from '../../lib/faceState'
import { type FaceView } from '../../services/people'

/** Props for {@link FaceOverlay}. */
export interface FaceOverlayProps {
  /** The detected faces to draw, from {@link useFaces}. */
  faces: FaceView[]
  /** The `face_index` whose naming panel is open, or null when none is. */
  selected: number | null
  /** The `face_index` hovered in the faces panel, or null. Highlights its box. */
  hovered?: number | null
  /** Selects a face (opens its naming panel). Never called when read-only. */
  onSelect: (faceIndex: number) => void
  /** Reports the hovered box, so the panel can highlight its row. Never called when read-only. */
  onHover?: (faceIndex: number | null) => void
  /**
   * When true the boxes are drawn with their names but cannot be selected, for
   * viewers who may not assign people. Defaults false.
   */
  readOnly?: boolean
}

/** Border colour per naming state — red is furthest from done, green is done. */
const STATE_COLOR: Record<FaceState, string> = {
  assigned: 'var(--bs-success)',
  unassigned: 'var(--bs-warning)',
  unmatched: 'var(--bs-danger)',
}

/**
 * Chrome drawn outside the box (the number badge and the name label) must never
 * intercept a click: the box under it is the click target, and a swallowed click
 * would also read as a non-swipe surface and break touch navigation.
 */
const CHROME: CSSProperties = { pointerEvents: 'none', whiteSpace: 'nowrap' }

/**
 * A transparent layer of clickable face boxes, positioned from normalised bboxes
 * so it stays correct at any rendered size. It draws no image of its own: mount
 * it as the last child of the element that wraps the photo (a `position-relative`
 * box tight around the `<img>`) and the boxes land on the faces. The layer itself
 * is click-through, so only the boxes intercept pointer events — and when
 * read-only not even those, leaving the image below fully clickable.
 *
 * Each box carries its number, so a panel row ("Face #2") can be traced back to a
 * face without hunting; an assigned box also carries the person's name.
 */
export function FaceOverlay({
  faces,
  selected,
  hovered = null,
  onSelect,
  onHover,
  readOnly = false,
}: FaceOverlayProps) {
  const { t } = useTranslation()

  return (
    <div
      className="position-absolute top-0 start-0 w-100 h-100"
      style={{ pointerEvents: 'none' }}
      data-testid="face-overlay"
    >
      {faces.map((face, position) => {
        const state = faceState(face)
        const number = position + 1
        const isSelected = selected === face.face_index
        const isHovered = hovered === face.face_index
        const label =
          state === 'assigned' ? (face.subject_name ?? '') : t('faces.unnamed', { index: number })
        // A box hugging the bottom edge would have its name label clipped away by
        // the photo container's overflow — flip the label above the box instead.
        const bottomEdge = face.bbox[1] + face.bbox[3] > 0.9

        return (
          <button
            key={face.face_index}
            type="button"
            aria-label={label}
            title={label}
            aria-pressed={isSelected}
            disabled={readOnly}
            data-face-state={state}
            data-selected={isSelected ? 'true' : undefined}
            onClick={() => {
              onSelect(face.face_index)
            }}
            onMouseEnter={() => onHover?.(face.face_index)}
            onMouseLeave={() => onHover?.(null)}
            className="position-absolute p-0"
            style={{
              ...faceBoxStyle(face.bbox),
              borderStyle: 'solid',
              borderWidth: isSelected || isHovered ? 3 : 2,
              borderColor: isSelected ? 'var(--bs-primary)' : STATE_COLOR[state],
              boxShadow: isSelected ? '0 0 0 3px rgba(var(--bs-primary-rgb), 0.35)' : undefined,
              background: 'transparent',
              cursor: readOnly ? 'default' : 'pointer',
              pointerEvents: readOnly ? 'none' : 'auto',
            }}
          >
            <span
              className={`position-absolute top-0 start-0 badge ${
                isSelected ? 'text-bg-primary' : 'text-bg-dark'
              }`}
              style={{ ...CHROME, transform: 'translate(-2px, -100%)' }}
            >
              {number}
            </span>
            {state === 'assigned' && (
              <span
                className={`position-absolute start-0 badge text-bg-dark ${
                  bottomEdge ? 'bottom-100' : 'top-100'
                }`}
                style={CHROME}
              >
                {face.subject_name}
              </span>
            )}
          </button>
        )
      })}
    </div>
  )
}

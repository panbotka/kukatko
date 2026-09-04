import { type CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'

import { faceMarkerStyle, rotateBbox, rotatedFrameStyle } from '../../lib/faceGeometry'
import { type FaceState, faceState } from '../../lib/faceState'
import { type FaceView } from '../../services/people'

import './faceOverlay.css'

/** Props for {@link FaceOverlay}. */
export interface FaceOverlayProps {
  /** The detected faces to draw, from {@link useFaces}. */
  faces: FaceView[]
  /** The `face_index` whose naming panel is open, or null when none is. */
  selected: number | null
  /**
   * The `face_index` the reader is pointing at — hovered or focused, on either
   * side of the pair, since the page feeds its own `onHover` straight back here.
   * Its box is thickened and is the only one that shows a name.
   */
  hovered?: number | null
  /** Selects a face (opens its naming panel). Never called when read-only. */
  onSelect: (faceIndex: number) => void
  /**
   * Reports the box the pointer/focus is on, so the panel can highlight its row.
   * Never called when read-only.
   */
  onHover?: (faceIndex: number | null) => void
  /**
   * When true the boxes are drawn but cannot be selected or even hovered, for
   * viewers who may not assign people — their names then come from the panel,
   * whose rows still report the pairing. Defaults false.
   */
  readOnly?: boolean
  /**
   * The rotation the photo under the boxes is being shown at, in clockwise
   * degrees (0/90/180/270) — the saved or in-progress edit's rotation. The boxes
   * are mapped through it, so they follow the photo instead of staying with the
   * pixels the detector saw. Defaults to 0 (upright).
   */
  rotation?: number
  /**
   * The wrapper's `width / height`, needed only for a quarter turn: it is what
   * says how big the turned photo's box is in percentages of the wrapper it
   * overflows. Pass the measured frame's ratio (`useImageFrame`); omitting it on a
   * quarter turn leaves the layer filling the wrapper, which places the boxes
   * loosely rather than not at all.
   */
  frameRatio?: number
  /**
   * Whether the wrapper the boxes are positioned against is the **measured**
   * frame of the loaded image (`useImageFrame`) rather than a provisional
   * estimate. While false the layer renders empty: a box placed against a frame
   * that may still change lands off its face and then visibly jumps. Defaults
   * true, for a caller that owns a frame it has already settled.
   */
  measured?: boolean
}

/**
 * Border colour per naming state — two colours for two states: yellow is left to
 * do, green is done. Deliberately no third one: whether a marker already covers
 * the face changes nothing the reader can act on (see `lib/faceState`), and red
 * would mean "something is wrong" about the ordinary majority of a library.
 */
const STATE_COLOR: Record<FaceState, string> = {
  named: 'var(--bs-success)',
  unnamed: 'var(--bs-warning)',
}

/**
 * Chrome drawn outside the box (the number badge and the name label) must never
 * intercept a click: the box under it is the click target, and a swallowed click
 * would also read as a non-swipe surface and break touch navigation.
 */
const CHROME: CSSProperties = { pointerEvents: 'none', whiteSpace: 'nowrap' }

/**
 * Paint order inside the layer. The dimming veil (`.kk-face-dim`, z-index 1) sits
 * between the two: an ordinary marker paints under it and goes quiet with the
 * photograph, while the one being pointed at — and above all the selected one —
 * paints over it at full strength. That is the whole point of the veil, and
 * leaving the other markers bright on top of it would undo it.
 */
const Z_HOVERED = 2

/** @see {@link Z_HOVERED} */
const Z_SELECTED = 3

/**
 * How close to an edge of the frame a box has to be for its chrome to be flipped
 * inside it. A name label hangs below the box and the number badge above it, and
 * either one, drawn outside the photograph, is a label the reader never sees.
 */
const EDGE_MARGIN = 0.06

/**
 * The halo around the marker the reader is pointing at — from either side of the
 * pair, since a hovered or focused panel row arrives here as `hovered`.
 *
 * A thicker border on its own is not an answer to "which of these eighteen is
 * row 12?": at the minimum size that is a one-pixel difference nobody can find
 * on a crowded photograph. The selected marker keeps the primary tint it has
 * always had, and the merely-pointed-at one takes a white ring, which reads over
 * a dark coat and a bright sky alike.
 */
function halo(isSelected: boolean, isHovered: boolean): string | undefined {
  if (isSelected) {
    return '0 0 0 3px rgba(var(--bs-primary-rgb), 0.35)'
  }
  return isHovered ? '0 0 0 2px rgba(255, 255, 255, 0.7)' : undefined
}

/**
 * A transparent layer of clickable face boxes, positioned from normalised bboxes
 * so it stays correct at any rendered size. It draws no image of its own: mount
 * it as the last child of the element that wraps the photo (a `position-relative`
 * box tight around the `<img>`) and the boxes land on the faces. The layer itself
 * is click-through, so only the boxes intercept pointer events — and when
 * read-only not even those, leaving the image below fully clickable.
 *
 * Each box carries its number — assigned in reading order by `useFaces`, so #1 is
 * the leftmost face of the top row — which is what ties a box to its row in the
 * faces panel.
 *
 * **Only the active box carries a name.** Drawing every name at once is what the
 * boxes used to do, and on a group photo the labels lay across each other and
 * across neighbouring boxes: with fifteen people it is an unreadable pile, and the
 * library holds thousands of those. The name now appears on the box the reader is
 * actually pointing at (hovered, focused, or selected — `hovered` carries all
 * three, since focus reports through it), which is also the box whose row is lit
 * in the panel. The remaining names are one hover away there.
 *
 * **A box is never smaller than the floor under it.** Traced exactly, a box is
 * only as big as its face, and on a group photograph that is nothing: measured
 * on a concert photo with eighteen detections, the smallest one rendered about
 * 8 x 9 CSS pixels — a mark nobody can tie to the row asking them to name
 * „Unnamed face 12". `faceOverlay.css` puts a floor under the drawn size (raised
 * to the touch floor on a coarse pointer) and, because the geometry is
 * centre-anchored (`faceMarkerStyle`), a marker that hits the floor grows
 * *around* its face rather than sliding off it. The invisible 44px hit box in
 * `app.css` stays: it still covers the case of a marker at the floor on a fine
 * pointer being tapped on a hybrid screen.
 *
 * **The selected face is lit and the rest of the photograph is dimmed** — the
 * same treatment the review game uses to single a face out of a crowd, down to
 * the numbers (`review/review.css`). It is drawn as its own layer
 * (`.kk-face-dim`) with a hole on the selected box, and the z-index ladder puts
 * the unselected markers *under* the veil: they stay visible, they stop
 * competing. Pairing with the panel is reported from focus as well as hover, so
 * a tap (which focuses the box) and the keyboard both light the matching row,
 * where hover alone would never fire on touch.
 *
 * **A rotated photo keeps its boxes.** Detection ran on the upright original, so
 * a photo turned in the editor has every bbox mapped through that rotation
 * (`rotateBbox`) and the layer itself given the turned photo's box
 * (`rotatedFrameStyle`) — the rectangles land on the faces, and because only the
 * coordinates turn, each box's number and name stay upright and readable. A crop
 * is the one adjustment the boxes cannot follow (the frame it leaves is not the
 * frame they were measured against), and the viewer keeps the face UI off then.
 *
 * **The layer draws nothing until its wrapper is the measured image**
 * (`measured`). Percentages are only as good as the box they are percentages of,
 * and a wrapper sized from the catalogue row can be the wrong shape — better a
 * box a moment late than a box on the wrong part of the photograph.
 */
export function FaceOverlay({
  faces,
  selected,
  hovered = null,
  onSelect,
  onHover,
  readOnly = false,
  measured = true,
  rotation = 0,
  frameRatio,
}: FaceOverlayProps) {
  const { t } = useTranslation()
  const drawn = measured ? faces : []
  // The face the veil cuts its hole on. Read from `drawn`, so a selection made
  // before the frame settled dims nothing rather than dimming everything.
  const litFace = drawn.find((face) => face.face_index === selected)

  return (
    <div
      // The layer IS the rendered photo's box, which for a quarter turn is not the
      // wrapper's box — hence inline geometry rather than `w-100 h-100`, whose
      // `!important` would win over it. The layer is rotated only in shape, never
      // by a `rotate()`: the boxes carry the rotation in their coordinates, so
      // their numbers and names stay the right way up.
      // `kk-face-layer` is what a host stylesheet addresses to place the boxes in
      // its own stacking order — the viewer has to, since its photograph is
      // numbered (see `viewer.css`). Keep the class even if this layer's own
      // styling stays inline.
      className="kk-face-layer position-absolute"
      style={{ ...rotatedFrameStyle(rotation, frameRatio), pointerEvents: 'none' }}
      data-testid="face-overlay"
    >
      {litFace !== undefined && (
        // Decoration, and announced as none: the selected face already has a
        // pressed button and an open naming panel saying what it is.
        <div className="kk-face-dim" data-testid="face-dim" aria-hidden="true">
          <span
            className="kk-face-dim__hole"
            style={faceMarkerStyle(rotateBbox(litFace.bbox, rotation))}
          />
        </div>
      )}
      {drawn.map((face, position) => {
        const state = faceState(face)
        const number = position + 1
        const isSelected = selected === face.face_index
        const isHovered = hovered === face.face_index
        const isActive = isSelected || isHovered
        const label =
          state === 'named' ? (face.subject_name ?? '') : t('faces.unnamed', { index: number })
        // Where the box lands on the photo as it is currently shown.
        const bbox = rotateBbox(face.bbox, rotation)
        // A box hugging the bottom edge would have its name label clipped away by
        // the photo container's overflow — flip the label above the box instead.
        const bottomEdge = bbox[1] + bbox[3] > 0.9
        // And one hugging the top edge would lose its number badge the same way,
        // so that one is drawn inside the box instead of above it.
        const topEdge = bbox[1] < EDGE_MARGIN

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
            onFocus={() => onHover?.(face.face_index)}
            onBlur={() => onHover?.(null)}
            className="kk-face-box position-absolute p-0"
            style={{
              // Centre and size as custom properties, because the floor under a
              // marker's apparent size is `max()`/`clamp()` arithmetic against
              // the rendered photograph and belongs in `faceOverlay.css`.
              ...faceMarkerStyle(bbox),
              borderStyle: 'solid',
              borderWidth: isActive ? 3 : 2,
              borderColor: isSelected ? 'var(--bs-primary)' : STATE_COLOR[state],
              boxShadow: halo(isSelected, isHovered),
              background: 'transparent',
              cursor: readOnly ? 'default' : 'pointer',
              pointerEvents: readOnly ? 'none' : 'auto',
              // The active box is the one carrying a name, and a name reaches
              // outside the box it belongs to — lift it over its neighbours, or a
              // box drawn later paints its border straight through the label.
              // The same ladder is what lifts it clear of the dimming veil.
              zIndex: isSelected ? Z_SELECTED : isHovered ? Z_HOVERED : undefined,
            }}
          >
            <span
              className={`position-absolute top-0 start-0 badge ${
                isSelected ? 'text-bg-primary' : 'text-bg-dark'
              }`}
              style={{ ...CHROME, transform: topEdge ? undefined : 'translate(-2px, -100%)' }}
            >
              {number}
            </span>
            {state === 'named' && isActive && (
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

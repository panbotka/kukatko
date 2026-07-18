/**
 * The bootstrap-icons glyphs the UI uses, as their bare icon names (the part
 * after the `bi-` prefix). Keeping them in one union means a typo is a compile
 * error rather than an invisible blank square, and it documents the app's icon
 * vocabulary in a single place.
 */
export type IconName =
  | 'activity'
  | 'arrow-clockwise'
  | 'arrow-counterclockwise'
  | 'arrow-left'
  | 'arrow-right'
  | 'archive'
  | 'arrows-angle-contract'
  | 'arrows-angle-expand'
  | 'bookmarks'
  | 'box-arrow-in-down'
  | 'box-arrow-right'
  | 'check-lg'
  | 'chevron-down'
  | 'chevron-left'
  | 'chevron-right'
  | 'clipboard'
  | 'clock-history'
  | 'cloud-arrow-up'
  | 'collection'
  | 'compass'
  | 'dash-lg'
  | 'exclamation-triangle'
  | 'eye'
  | 'eye-fill'
  | 'files'
  | 'geo-alt'
  | 'github'
  | 'grid-3x3-gap-fill'
  | 'hand-thumbs-down'
  | 'hand-thumbs-down-fill'
  | 'hand-thumbs-up'
  | 'hand-thumbs-up-fill'
  | 'heart'
  | 'images'
  | 'info-circle'
  | 'lock-fill'
  | 'magic'
  | 'map'
  | 'pencil'
  | 'people'
  | 'person-bounding-box'
  | 'person-check'
  | 'person-circle'
  | 'person-gear'
  | 'plus-lg'
  | 'question-circle'
  | 'search'
  | 'shield-lock'
  | 'sliders'
  | 'tags'
  | 'tools'
  | 'trash'
  | 'ui-checks'
  | 'unlock'
  | 'wrench-adjustable'
  | 'x-lg'

/**
 * A decorative bootstrap-icons glyph. Icons only ever accompany a visible text
 * label, so they carry no accessible name: they are `aria-hidden`, and screen
 * readers announce the label alone.
 */
export function Icon({ name, className }: { name: IconName; className?: string }) {
  return (
    <i
      className={`bi bi-${name}${className === undefined ? '' : ` ${className}`}`}
      aria-hidden="true"
    />
  )
}

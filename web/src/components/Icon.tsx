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
  | 'arrows-fullscreen'
  | 'arrows-angle-contract'
  | 'arrows-angle-expand'
  | 'award-fill'
  | 'bar-chart'
  | 'bookmarks'
  | 'bounding-box'
  | 'box-arrow-in-down'
  | 'box-arrow-right'
  | 'box-arrow-up-right'
  | 'calendar-range'
  | 'chat-left-text'
  | 'check-lg'
  | 'chevron-down'
  | 'chevron-left'
  | 'chevron-right'
  | 'chevron-up'
  | 'clipboard'
  | 'clock-history'
  | 'cloud-arrow-up'
  | 'collection'
  | 'compass'
  | 'dash-lg'
  | 'exclamation-triangle'
  | 'eye'
  | 'eye-fill'
  | 'eye-slash'
  | 'files'
  | 'fire'
  | 'funnel'
  | 'geo-alt'
  | 'github'
  | 'grid-3x3-gap-fill'
  | 'hand-thumbs-down'
  | 'hand-thumbs-down-fill'
  | 'hand-thumbs-up'
  | 'hand-thumbs-up-fill'
  | 'heart'
  | 'image'
  | 'image-fill'
  | 'images'
  | 'info-circle'
  | 'key'
  | 'lightning-charge-fill'
  | 'lock-fill'
  | 'magic'
  | 'map'
  | 'pause-fill'
  | 'pencil'
  | 'people'
  | 'person-bounding-box'
  | 'person-check'
  | 'person-circle'
  | 'person-gear'
  | 'person-hearts'
  | 'person-lines-fill'
  | 'play-fill'
  | 'plus-lg'
  | 'question-circle'
  | 'search'
  | 'send'
  | 'skip-backward-fill'
  | 'skip-forward-fill'
  | 'shield-lock'
  | 'slash-circle'
  | 'sliders'
  | 'stars'
  | 'tags'
  | 'three-dots'
  | 'tools'
  | 'trash'
  | 'trophy'
  | 'trophy-fill'
  | 'ui-checks'
  | 'unlock'
  | 'volume-mute-fill'
  | 'volume-up-fill'
  | 'wifi-off'
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

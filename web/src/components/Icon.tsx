/**
 * The bootstrap-icons glyphs the UI uses, as their bare icon names (the part
 * after the `bi-` prefix). Keeping them in one union means a typo is a compile
 * error rather than an invisible blank square, and it documents the app's icon
 * vocabulary in a single place.
 */
export type IconName =
  | 'activity'
  | 'bookmarks'
  | 'box-arrow-in-down'
  | 'box-arrow-right'
  | 'chevron-down'
  | 'chevron-right'
  | 'cloud-arrow-up'
  | 'collection'
  | 'compass'
  | 'files'
  | 'geo-alt'
  | 'heart'
  | 'images'
  | 'map'
  | 'people'
  | 'person-circle'
  | 'person-gear'
  | 'search'
  | 'sliders'
  | 'tags'
  | 'tools'
  | 'trash'
  | 'wrench-adjustable'

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

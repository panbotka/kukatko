import { type IconName } from './Icon'

/**
 * The three catalog entity kinds that carry a distinct colour language. Albums,
 * tags (labels) and people are different kinds of thing, so each gets its own
 * hue and leading icon wherever it appears as a chip or badge (the library
 * active-filter chips, the photo organize panel, the global-search sections).
 * `tag` is the UI-facing name for a label.
 */
export type EntityKind = 'album' | 'tag' | 'person'

/** Presentation for one entity kind: the badge colour class and its icon. */
export interface EntityStyle {
  /**
   * The `.kk-entity-*` modifier class (defined once in `styles/tokens.css`)
   * that paints a Bootstrap `.badge` in this kind's hue with legible text.
   */
  className: string
  /** The leading bootstrap-icon glyph that reinforces the hue for colour-blind readers. */
  icon: IconName
}

/**
 * The single source of truth mapping each entity kind to its colour class and
 * icon. Every place that renders an album/tag/person chip reads from here, so
 * the convention is defined once and stays consistent across the app.
 */
export const ENTITY_STYLE: Record<EntityKind, EntityStyle> = {
  album: { className: 'kk-entity-album', icon: 'collection' },
  tag: { className: 'kk-entity-tag', icon: 'tags' },
  person: { className: 'kk-entity-person', icon: 'person-circle' },
}

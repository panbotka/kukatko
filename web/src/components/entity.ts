import { type IconName } from './Icon'

/**
 * The three catalog entities that carry a distinct colour across the app.
 * `label` is the codebase's name for what the UI also calls a *tag*; the
 * colour convention keeps the two words as one thing.
 */
export type EntityKind = 'album' | 'label' | 'person'

/**
 * The fill-colour class and leading icon for each entity kind. Defined once so
 * every album/tag/person token — the library filter chips, the photo organize
 * panel, the global-search sections — speaks one colour language. The colour
 * classes themselves (and the `--kk-entity-*` hues behind them) live in
 * `styles/tokens.css`; here we only bind a kind to its class and glyph.
 */
const ENTITY: Record<EntityKind, { className: string; icon: IconName }> = {
  album: { className: 'kk-entity-album', icon: 'collection' },
  label: { className: 'kk-entity-label', icon: 'tags' },
  person: { className: 'kk-entity-person', icon: 'person-circle' },
}

/** The badge fill-colour class for an entity kind (album / tag / person). */
export function entityBadgeClassName(kind: EntityKind): string {
  return ENTITY[kind].className
}

/** The leading icon marking an entity kind (album / tag / person). */
export function entityIcon(kind: EntityKind): IconName {
  return ENTITY[kind].icon
}

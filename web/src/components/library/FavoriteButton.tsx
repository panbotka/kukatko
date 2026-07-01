import { type MouseEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { useFavorite } from '../../hooks/useFavorite'

/** Props for {@link FavoriteButton}. */
export interface FavoriteButtonProps {
  /** The photo to favorite. */
  uid: string
  /** The server-known favorite state (`photo.is_favorite`). */
  favorite: boolean
  /** Extra classes for positioning (e.g. an absolute overlay on a grid tile). */
  className?: string
}

/** A heart glyph that is filled when favorited and outlined otherwise. */
function HeartIcon({ filled }: { filled: boolean }) {
  return (
    <svg
      width="18"
      height="18"
      viewBox="0 0 16 16"
      fill={filled ? 'currentColor' : 'none'}
      stroke="currentColor"
      strokeWidth="1.4"
      aria-hidden="true"
      focusable="false"
      className="d-block"
    >
      <path d="M8 13.6S2.3 9.9 2.3 6.1A2.6 2.6 0 0 1 8 4.1a2.6 2.6 0 0 1 5.7 2C13.7 9.9 8 13.6 8 13.6z" />
    </svg>
  )
}

/** Props for {@link FavoriteToggle}. */
export interface FavoriteToggleProps {
  /** The current (optimistic) favorite state. */
  favorite: boolean
  /** Whether a toggle is in flight (disables the control). */
  pending: boolean
  /** Invoked on click to flip the favorite state. */
  onToggle: (event: MouseEvent<HTMLButtonElement>) => void
  /** Extra classes for positioning (e.g. an absolute overlay on a grid tile). */
  className?: string
}

/**
 * The presentational heart button: a filled/outlined heart with the right
 * accessible label for the given state. Controlled — it renders `favorite` and
 * calls `onToggle`, owning no state — so it can back either the self-contained
 * {@link FavoriteButton} (grid tiles) or a lifted favorite (the detail page, where
 * the `f` shortcut and the header heart share one {@link useFavorite}).
 */
export function FavoriteToggle({ favorite, pending, onToggle, className }: FavoriteToggleProps) {
  const { t } = useTranslation()
  return (
    <button
      type="button"
      aria-pressed={favorite}
      aria-label={favorite ? t('favorite.remove') : t('favorite.add')}
      title={favorite ? t('favorite.remove') : t('favorite.add')}
      disabled={pending}
      onClick={onToggle}
      className={`btn btn-sm p-1 lh-1 border-0 rounded-circle d-inline-flex align-items-center justify-content-center kukatko-tap-target ${
        favorite ? 'text-danger' : 'text-white'
      } ${className ?? ''}`}
      style={{ backgroundColor: 'rgba(0, 0, 0, 0.45)' }}
    >
      <HeartIcon filled={favorite} />
    </button>
  )
}

/**
 * A heart toggle that favorites or unfavorites a photo for the current user with
 * an optimistic update (rolling back on failure, via {@link useFavorite}). It is
 * a personal action available to every signed-in user — including viewers — so it
 * carries no role gate. Rendered both as an overlay on grid tiles and on the photo
 * detail page; `className` positions it. When overlaid on a link/tile it stops the
 * click from bubbling so toggling never navigates.
 */
export function FavoriteButton({ uid, favorite, className }: FavoriteButtonProps) {
  const { favorite: isFavorite, pending, toggle } = useFavorite(uid, favorite)

  function handleClick(event: MouseEvent<HTMLButtonElement>) {
    event.preventDefault()
    event.stopPropagation()
    toggle()
  }

  return (
    <FavoriteToggle
      favorite={isFavorite}
      pending={pending}
      onToggle={handleClick}
      className={className}
    />
  )
}

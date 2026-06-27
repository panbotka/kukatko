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

/**
 * A heart toggle that favorites or unfavorites a photo for the current user with
 * an optimistic update (rolling back on failure, via {@link useFavorite}). It is
 * a personal action available to every signed-in user — including viewers — so it
 * carries no role gate. Rendered both as an overlay on grid tiles and on the photo
 * detail page; `className` positions it. When overlaid on a link/tile it stops the
 * click from bubbling so toggling never navigates.
 */
export function FavoriteButton({ uid, favorite, className }: FavoriteButtonProps) {
  const { t } = useTranslation()
  const { favorite: isFavorite, pending, toggle } = useFavorite(uid, favorite)

  function handleClick(event: MouseEvent<HTMLButtonElement>) {
    event.preventDefault()
    event.stopPropagation()
    toggle()
  }

  return (
    <button
      type="button"
      aria-pressed={isFavorite}
      aria-label={isFavorite ? t('favorite.remove') : t('favorite.add')}
      title={isFavorite ? t('favorite.remove') : t('favorite.add')}
      disabled={pending}
      onClick={handleClick}
      className={`btn btn-sm p-1 lh-1 border-0 rounded-circle d-inline-flex ${
        isFavorite ? 'text-danger' : 'text-white'
      } ${className ?? ''}`}
      style={{ backgroundColor: 'rgba(0, 0, 0, 0.45)' }}
    >
      <HeartIcon filled={isFavorite} />
    </button>
  )
}

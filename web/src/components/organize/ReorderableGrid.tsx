import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { GRID_THUMB_SIZE, type Photo, thumbUrl } from '../../services/photos'

/** Props for {@link ReorderableGrid}. */
export interface ReorderableGridProps {
  /** The photos in their current display order. */
  photos: Photo[]
  /**
   * Called with the full UID list in the new order after a move (drag-drop or the
   * up/down controls). The parent persists the order and re-renders `photos`.
   */
  onReorder: (orderedUids: string[]) => void
}

/** Returns `uids` with the item at `from` moved to `to`. */
function move(uids: string[], from: number, to: number): string[] {
  if (from === to || from < 0 || to < 0 || from >= uids.length || to >= uids.length) {
    return uids
  }
  const next = [...uids]
  const [item] = next.splice(from, 1)
  next.splice(to, 0, item)
  return next
}

/**
 * A plain (non-virtualized) photo grid whose tiles can be reordered by dragging
 * or with per-tile up/down controls (the accessible, keyboard-friendly path).
 * Operates on the currently loaded photos; every move reports the full new order
 * via `onReorder` so the parent can persist it. Album photo sets in reorder mode
 * are bounded, so virtualization is unnecessary here.
 */
export function ReorderableGrid({ photos, onReorder }: ReorderableGridProps) {
  const { t } = useTranslation()
  const [dragIndex, setDragIndex] = useState<number | null>(null)
  const uids = photos.map((p) => p.uid)

  const reorder = (from: number, to: number) => {
    const next = move(uids, from, to)
    if (next !== uids) {
      onReorder(next)
    }
  }

  return (
    <div
      style={{
        display: 'grid',
        gridTemplateColumns: 'repeat(auto-fill, minmax(140px, 1fr))',
        gap: '6px',
      }}
    >
      {photos.map((photo, index) => {
        const label = photo.title !== '' ? photo.title : photo.file_name
        return (
          <div
            key={photo.uid}
            draggable
            onDragStart={() => {
              setDragIndex(index)
            }}
            onDragOver={(event) => {
              event.preventDefault()
            }}
            onDrop={(event) => {
              event.preventDefault()
              if (dragIndex !== null) {
                reorder(dragIndex, index)
              }
              setDragIndex(null)
            }}
            onDragEnd={() => {
              setDragIndex(null)
            }}
            className="position-relative bg-secondary-subtle overflow-hidden rounded"
            style={{ aspectRatio: '1 / 1', cursor: 'grab' }}
          >
            <img
              src={thumbUrl(photo.uid, GRID_THUMB_SIZE)}
              alt={label}
              loading="lazy"
              decoding="async"
              draggable={false}
              className="w-100 h-100"
              style={{ objectFit: 'cover' }}
            />
            <div className="position-absolute bottom-0 start-0 end-0 d-flex justify-content-between p-1">
              <button
                type="button"
                className="btn btn-sm btn-dark opacity-75 py-0 px-1"
                disabled={index === 0}
                aria-label={t('albums.reorder.moveBack', { name: label })}
                onClick={() => {
                  reorder(index, index - 1)
                }}
              >
                ‹
              </button>
              <button
                type="button"
                className="btn btn-sm btn-dark opacity-75 py-0 px-1"
                disabled={index === photos.length - 1}
                aria-label={t('albums.reorder.moveForward', { name: label })}
                onClick={() => {
                  reorder(index, index + 1)
                }}
              >
                ›
              </button>
            </div>
          </div>
        )
      })}
    </div>
  )
}

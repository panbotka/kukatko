import { Virtuoso } from 'react-virtuoso'

import { type UploadQueueItem } from '../../hooks/useUploadQueue'

import { UploadItem } from './UploadItem'

/** Props for {@link UploadList}. */
export interface UploadListProps {
  /** The files to render (already filtered by the page when needed). */
  items: UploadQueueItem[]
  /** Removes a file from the queue. */
  onRemove: (id: string) => void
  /** Re-queues a single failed file. */
  onRetry: (id: string) => void
}

/**
 * Virtualized per-file list for the upload page. Only the visible rows are
 * mounted (react-virtuoso), so a batch of 100+ files stays responsive on a
 * phone. It scrolls with the window like the rest of the app, which keeps the
 * overall-progress header pinned above it. Row gaps use bottom padding (not
 * margin) so virtuoso measures each row's height correctly.
 */
export function UploadList({ items, onRemove, onRetry }: UploadListProps) {
  return (
    <Virtuoso
      useWindowScroll
      data={items}
      computeItemKey={(_index, item) => item.id}
      itemContent={(_index, item) => (
        <div className="pb-2">
          <UploadItem item={item} onRemove={onRemove} onRetry={onRetry} />
        </div>
      )}
    />
  )
}

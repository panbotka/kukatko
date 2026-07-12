import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import {
  isAbortError,
  uploadFile,
  type UploadFileResult,
  type UploadWarning,
} from '../services/upload'
import { ApiError } from '../services/auth'

/**
 * Maximum number of files uploaded concurrently. Kept small so a large
 * selection does not open hundreds of parallel requests (saturating the network
 * and the server) while still overlapping enough to stay fast.
 */
export const MAX_CONCURRENT_UPLOADS = 3

/** Lifecycle of a single queued file. */
export type QueueItemStatus = 'queued' | 'uploading' | 'created' | 'duplicate' | 'error'

/** One file in the upload queue with its live progress and outcome. */
export interface UploadQueueItem {
  /** Stable client-side id (queue position is not stable across removals). */
  id: string
  file: File
  status: QueueItemStatus
  /** Upload progress as a fraction in `[0, 1]`. */
  progress: number
  /** Backend photo UID once created (also set for duplicates). */
  photoUid?: string
  /** Human-readable failure detail when `status` is `error`. */
  error?: string
  /** Non-fatal backend warnings (for example a near-duplicate match). */
  warnings?: UploadWarning[]
}

/** Aggregate counts across the queue, feeding the overall-progress header. */
export interface UploadSummary {
  total: number
  queued: number
  uploading: number
  created: number
  duplicate: number
  error: number
}

/** Public surface of {@link useUploadQueue}. */
export interface UseUploadQueueResult {
  items: UploadQueueItem[]
  summary: UploadSummary
  /**
   * Overall batch completion as a fraction in `[0, 1]`. A settled file (created,
   * duplicate, or error) counts as fully done; an in-flight file contributes its
   * live upload fraction, so the aggregate bar advances smoothly rather than only
   * in whole-file steps. Zero for an empty queue.
   */
  progress: number
  /** True while any file is uploading. */
  isUploading: boolean
  /** True once every file has settled (none queued or uploading) and the queue is non-empty. */
  isComplete: boolean
  /** UIDs of newly created photos, for linking into the library. */
  createdUids: string[]
  /**
   * UIDs of every photo the batch resolved to — created *and* duplicate — for
   * post-upload assignment to albums/labels. A duplicate already in the library
   * still carries a `photo_uid`, so it is included: a re-upload should land in
   * the chosen albums just like a fresh one.
   */
  resolvedUids: string[]
  /** Adds files to the queue, skipping ones already present (name + size + mtime). */
  addFiles: (files: FileList | File[]) => void
  /** Removes a file from the queue, aborting it first if it is uploading. */
  removeItem: (id: string) => void
  /** Begins uploading all queued files, respecting the concurrency cap. */
  start: () => void
  /** Re-queues a single failed file. */
  retry: (id: string) => void
  /** Re-queues every failed file. */
  retryFailed: () => void
  /** Aborts in-flight uploads and empties the queue. */
  clear: () => void
}

let idCounter = 0

/** Generates a process-unique id for a queue item. */
function nextId(): string {
  idCounter += 1
  return `u${String(idCounter)}`
}

/** Identity key used to skip files already in the queue. */
function fileKey(file: File): string {
  return `${file.name}:${String(file.size)}:${String(file.lastModified)}`
}

/** Maps a successful per-file result to the matching terminal queue status. */
function statusFor(outcome: UploadFileResult['outcome']): QueueItemStatus {
  if (outcome === 'created') {
    return 'created'
  }
  if (outcome === 'duplicate') {
    return 'duplicate'
  }
  return 'error'
}

/**
 * Manages a queue of files uploaded with per-file progress and a bounded number
 * of concurrent requests. The queue drains automatically once {@link start} (or
 * a retry) marks files runnable: an effect tops up in-flight uploads to the cap
 * whenever the queue changes, so newly added or retried files flow through
 * without extra orchestration.
 *
 * Failures never abort the batch — each file's outcome is captured on its item
 * — mirroring the backend's per-file semantics.
 */
export function useUploadQueue(): UseUploadQueueResult {
  const [items, setItems] = useState<UploadQueueItem[]>([])

  // A committed snapshot read synchronously by the pump (which runs from an
  // effect, after the matching render, so the ref is always up to date there).
  const itemsRef = useRef(items)
  itemsRef.current = items

  // Live request controllers, keyed by item id, for cancellation.
  const controllers = useRef(new Map<string, AbortController>())
  // The queue only drains after the user starts it (or retries); adding files
  // alone leaves them queued for review.
  const startedRef = useRef(false)

  /** Applies a partial update to a single item by id. */
  const patch = useCallback((id: string, changes: Partial<UploadQueueItem>): void => {
    setItems((prev) => prev.map((item) => (item.id === id ? { ...item, ...changes } : item)))
  }, [])

  // Stable indirection so `pump` and `startUpload` can call each other without a
  // dependency cycle (each reads the other's latest version through a ref).
  const startUploadRef = useRef<(item: UploadQueueItem) => void>(() => undefined)

  const pump = useCallback((): void => {
    if (!startedRef.current) {
      return
    }
    const current = itemsRef.current
    const active = current.filter((item) => item.status === 'uploading').length
    let free = MAX_CONCURRENT_UPLOADS - active
    if (free <= 0) {
      return
    }
    for (const item of current) {
      if (free <= 0) {
        break
      }
      if (item.status !== 'queued') {
        continue
      }
      free -= 1
      startUploadRef.current(item)
    }
  }, [])

  const startUpload = useCallback(
    (item: UploadQueueItem): void => {
      const controller = new AbortController()
      controllers.current.set(item.id, controller)
      patch(item.id, { status: 'uploading', progress: 0, error: undefined })

      uploadFile(item.file, {
        signal: controller.signal,
        onProgress: (fraction) => {
          patch(item.id, { progress: fraction })
        },
      })
        .then((result) => {
          patch(item.id, {
            status: statusFor(result.outcome),
            progress: 1,
            photoUid: result.photo_uid,
            warnings: result.warnings,
            error: result.outcome === 'error' ? (result.error ?? '') : undefined,
          })
        })
        .catch((error: unknown) => {
          if (isAbortError(error)) {
            // The item was removed/cleared mid-flight; nothing to record.
            return
          }
          const message = error instanceof ApiError ? error.message : String(error)
          patch(item.id, { status: 'error', error: message })
        })
        .finally(() => {
          controllers.current.delete(item.id)
        })
    },
    [patch],
  )

  startUploadRef.current = startUpload

  // Top up in-flight uploads whenever the queue changes (a file finished, was
  // added, or retried). Idempotent: it only starts files still `queued`.
  useEffect(() => {
    pump()
  }, [items, pump])

  const addFiles = useCallback((files: FileList | File[]): void => {
    const incoming = Array.from(files)
    if (incoming.length === 0) {
      return
    }
    setItems((prev) => {
      const seen = new Set(prev.map((item) => fileKey(item.file)))
      const additions: UploadQueueItem[] = []
      for (const file of incoming) {
        const key = fileKey(file)
        if (seen.has(key)) {
          continue
        }
        seen.add(key)
        additions.push({ id: nextId(), file, status: 'queued', progress: 0 })
      }
      return additions.length > 0 ? [...prev, ...additions] : prev
    })
  }, [])

  const removeItem = useCallback((id: string): void => {
    controllers.current.get(id)?.abort()
    controllers.current.delete(id)
    setItems((prev) => prev.filter((item) => item.id !== id))
  }, [])

  const start = useCallback((): void => {
    startedRef.current = true
    pump()
  }, [pump])

  const retry = useCallback((id: string): void => {
    startedRef.current = true
    setItems((prev) =>
      prev.map((item) =>
        item.id === id && item.status === 'error'
          ? { ...item, status: 'queued', progress: 0, error: undefined }
          : item,
      ),
    )
  }, [])

  const retryFailed = useCallback((): void => {
    startedRef.current = true
    setItems((prev) =>
      prev.map((item) =>
        item.status === 'error'
          ? { ...item, status: 'queued', progress: 0, error: undefined }
          : item,
      ),
    )
  }, [])

  const clear = useCallback((): void => {
    controllers.current.forEach((controller) => {
      controller.abort()
    })
    controllers.current.clear()
    startedRef.current = false
    setItems([])
  }, [])

  // Abort any in-flight uploads if the component unmounts.
  useEffect(() => {
    const live = controllers.current
    return () => {
      live.forEach((controller) => {
        controller.abort()
      })
      live.clear()
    }
  }, [])

  const summary = useMemo<UploadSummary>(() => {
    const counts: UploadSummary = {
      total: items.length,
      queued: 0,
      uploading: 0,
      created: 0,
      duplicate: 0,
      error: 0,
    }
    for (const item of items) {
      counts[item.status] += 1
    }
    return counts
  }, [items])

  const createdUids = useMemo(
    () =>
      items.flatMap((item) =>
        item.status === 'created' && item.photoUid !== undefined && item.photoUid !== ''
          ? [item.photoUid]
          : [],
      ),
    [items],
  )

  const resolvedUids = useMemo(
    () =>
      items.flatMap((item) =>
        (item.status === 'created' || item.status === 'duplicate') &&
        item.photoUid !== undefined &&
        item.photoUid !== ''
          ? [item.photoUid]
          : [],
      ),
    [items],
  )

  // Overall completion fraction, weighting in-flight files by their live upload
  // progress so the aggregate bar moves continuously rather than in file-sized
  // jumps. Read from `status` (not `progress`) so a failed file — whose progress
  // is never forced to 1 — still counts as done.
  const progress = useMemo<number>(() => {
    if (items.length === 0) {
      return 0
    }
    let completed = 0
    for (const item of items) {
      if (item.status === 'created' || item.status === 'duplicate' || item.status === 'error') {
        completed += 1
      } else if (item.status === 'uploading') {
        completed += item.progress
      }
    }
    return completed / items.length
  }, [items])

  const isUploading = summary.uploading > 0
  const isComplete = summary.total > 0 && summary.queued === 0 && summary.uploading === 0

  return {
    items,
    summary,
    progress,
    isUploading,
    isComplete,
    createdUids,
    resolvedUids,
    addFiles,
    removeItem,
    start,
    retry,
    retryFailed,
    clear,
  }
}

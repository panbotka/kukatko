import { useCallback, useEffect, useRef, useState } from 'react'

import { pendingName } from '../lib/pendingCreate'
import { ApiError } from '../services/auth'
import { type BulkOperations, type BulkResult, bulkUpdatePhotos } from '../services/bulk'
import {
  type AlbumCount,
  createAlbum,
  createLabel,
  fetchAlbums,
  fetchLabels,
  type LabelCount,
} from '../services/organize'

/** Fetch lifecycle of the album/label option lists. */
export type OrganizeLoadState =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; albums: AlbumCount[]; labels: LabelCount[] }

/**
 * Lifecycle of the post-upload assignment. It runs at most once per completed
 * batch: `idle` before and between batches, `assigning` while the call is in
 * flight, `done` with the per-photo result, or `error` with a retryable message
 * (the photos are already uploaded — only the album/label assignment failed).
 */
export type OrganizeAssignState =
  | { status: 'idle' }
  | { status: 'assigning' }
  | { status: 'done'; result: BulkResult }
  | { status: 'error'; message: string }

/** Public surface of {@link useUploadOrganize}. */
export interface UseUploadOrganizeResult {
  /** Fetch lifecycle of the album/label option lists. */
  load: OrganizeLoadState
  /** Chosen album values — real UIDs or `create:` markers for pending creations. */
  albums: string[]
  /** Chosen label values — real UIDs or `create:` markers for pending creations. */
  labels: string[]
  /** Replaces the chosen albums (the multi-select emits the whole next list). */
  setAlbums: (values: string[]) => void
  /** Replaces the chosen labels. */
  setLabels: (values: string[]) => void
  /** True when at least one album or label is chosen. */
  hasSelection: boolean
  /** Current assignment lifecycle. */
  assign: OrganizeAssignState
  /**
   * Assigns every `uids` photo to the chosen albums/labels in one
   * `POST /photos/bulk`, creating any pending albums/labels first. A no-op when
   * nothing is chosen or `uids` is empty. Safe to call repeatedly — it ignores a
   * call while one is already in flight.
   */
  runAssign: (uids: string[]) => void
  /** Retries the last assignment (same photos) after a failure. */
  retryAssign: () => void
  /** Clears any `done`/`error` result so the next batch can assign afresh. */
  resetAssign: () => void
}

/**
 * Owns the album/label selection and the post-upload assignment for the upload
 * page. It fetches the album and label catalogs once, holds the batch-wide
 * selection (with inline-create entries kept as `create:` markers, exactly like
 * the bulk-edit modal), and — given the UIDs a finished upload resolved to —
 * creates any pending albums/labels and adds every photo to the selection in a
 * single bulk call.
 *
 * The listing failing never blocks uploading: `load` goes to `error`, the
 * selectors stay empty, and `hasSelection` is false, so the page uploads exactly
 * as it does today. Assignment is deferred creation → one `POST /photos/bulk`, so
 * a failure leaves the freshly created albums/labels intact under their real UIDs
 * and a retry re-sends only the batch.
 */
export function useUploadOrganize(): UseUploadOrganizeResult {
  const [load, setLoad] = useState<OrganizeLoadState>({ status: 'loading' })
  const [albums, setAlbums] = useState<string[]>([])
  const [labels, setLabels] = useState<string[]>([])
  const [assign, setAssign] = useState<OrganizeAssignState>({ status: 'idle' })

  // The pump reads the latest selection synchronously from an async run.
  const albumsRef = useRef(albums)
  albumsRef.current = albums
  const labelsRef = useRef(labels)
  labelsRef.current = labels
  // The photos the last assignment targeted, so a retry re-sends the same set.
  const lastUidsRef = useRef<string[]>([])
  // Guards against a second assignment overlapping the first (StrictMode double
  // effects, a re-render firing the trigger twice before state settles).
  const inFlight = useRef(false)

  // Load the album and label catalogs once, on mount.
  useEffect(() => {
    const controller = new AbortController()
    Promise.all([fetchAlbums(controller.signal), fetchLabels(controller.signal)])
      .then(([albumList, labelList]) => {
        setLoad({ status: 'ready', albums: albumList, labels: labelList })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setLoad({ status: 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [])

  /**
   * Creates one pending album/label, records it in the option list so its chip
   * reads as the real entry, and swaps its fresh UID into the selection so a
   * later retry never recreates it. Returns the fresh UID.
   */
  const createPending = useCallback(
    async (kind: 'album' | 'label', value: string, name: string): Promise<string> => {
      if (kind === 'album') {
        const album = await createAlbum({ title: name, description: '', private: false })
        setLoad((prev) =>
          prev.status === 'ready'
            ? { ...prev, albums: [...prev.albums, { ...album, photo_count: 0 }] }
            : prev,
        )
        setAlbums((prev) => prev.map((current) => (current === value ? album.uid : current)))
        return album.uid
      }
      const label = await createLabel({ name, priority: 0 })
      setLoad((prev) =>
        prev.status === 'ready'
          ? { ...prev, labels: [...prev.labels, { ...label, photo_count: 0 }] }
          : prev,
      )
      setLabels((prev) => prev.map((current) => (current === value ? label.uid : current)))
      return label.uid
    },
    [],
  )

  /** Resolves a selection to real UIDs, creating pending entries as it goes. */
  const resolve = useCallback(
    async (kind: 'album' | 'label', values: string[]): Promise<string[]> => {
      const uids: string[] = []
      for (const value of values) {
        const name = pendingName(value)
        uids.push(name === null ? value : await createPending(kind, value, name))
      }
      return uids
    },
    [createPending],
  )

  const doAssign = useCallback(
    async (uids: string[]): Promise<void> => {
      if (inFlight.current) {
        return
      }
      if (uids.length === 0 || (albumsRef.current.length === 0 && labelsRef.current.length === 0)) {
        return
      }
      inFlight.current = true
      setAssign({ status: 'assigning' })
      try {
        // Create pending albums first (their failure is the more likely one and
        // stops before the batch), then labels, then submit the whole batch.
        const albumUids = await resolve('album', albumsRef.current)
        const labelUids = await resolve('label', labelsRef.current)
        const operations: BulkOperations = {}
        if (albumUids.length > 0) {
          operations.add_to_albums = albumUids
        }
        if (labelUids.length > 0) {
          operations.add_labels = labelUids
        }
        const result = await bulkUpdatePhotos(uids, operations)
        setAssign({ status: 'done', result })
      } catch (err: unknown) {
        // The backend names an actionable batch problem (a too-large batch); a
        // create failure (a duplicate name, permission) surfaces its message too.
        const message = err instanceof ApiError && err.message !== '' ? err.message : ''
        setAssign({ status: 'error', message })
      } finally {
        inFlight.current = false
      }
    },
    [resolve],
  )

  const runAssign = useCallback(
    (uids: string[]): void => {
      lastUidsRef.current = uids
      void doAssign(uids)
    },
    [doAssign],
  )

  const retryAssign = useCallback((): void => {
    void doAssign(lastUidsRef.current)
  }, [doAssign])

  const resetAssign = useCallback((): void => {
    setAssign({ status: 'idle' })
  }, [])

  return {
    load,
    albums,
    labels,
    setAlbums,
    setLabels,
    hasSelection: albums.length > 0 || labels.length > 0,
    assign,
    runAssign,
    retryAssign,
    resetAssign,
  }
}

import { useCallback, useEffect, useRef, useState } from 'react'

import {
  canSharePhotoFiles,
  isShareAbort,
  type ShareManifestFile,
  splitShareBatches,
} from '../lib/photoShare'
import { ApiError } from '../services/auth'
import { fetchShareFile, fetchShareManifest } from '../services/share'

/**
 * What the share control is doing right now.
 *
 * `waiting` is the state that shapes the whole feature: a share sheet may only be
 * opened from a fresh user gesture (iOS throws otherwise), so a selection larger
 * than one batch cannot be chained — the sequence pauses and asks for the next tap,
 * naming which batch is next and how many there are.
 */
export type ShareStatus =
  | { kind: 'idle' }
  /** Asking the backend what the selection is as files. */
  | { kind: 'manifest' }
  /** Downloading one batch's originals; `done` of `total` are in memory. */
  | { kind: 'fetching'; batch: number; batches: number; done: number; total: number }
  /** The share sheet is open and the answer is the phone's to give. */
  | { kind: 'sharing'; batch: number; batches: number }
  /** Batch `batch` of `batches` is prepared and waiting for its own tap. */
  | { kind: 'waiting'; batch: number; batches: number }

/**
 * Why a share did not (fully) happen. Structured rather than a message, so the hook
 * stays free of translation and the component says it in the reader's language.
 */
export type ShareError =
  /** The backend would not describe the selection. */
  | { kind: 'manifest' }
  /** The selection is over the per-share cap (413). */
  | { kind: 'tooMany' }
  /** `count` files could not be fetched; `name` is the first of them. */
  | { kind: 'fetch'; name: string; count: number }
  /** The share sheet itself refused (not a cancellation). */
  | { kind: 'sheet' }

/** What {@link usePhotoShare} hands its component. */
export interface UsePhotoShareResult {
  /** Whether this browser can hand files to a share sheet at all. */
  supported: boolean
  /** The current step of the sequence. */
  status: ShareStatus
  /** The last thing that went wrong, or null. Survives into the next batch. */
  error: ShareError | null
  /** Whether the hook is working and the control must not be tapped again. */
  busy: boolean
  /** The tap: starts a share, or hands over the next prepared batch. */
  share: () => void
}

/** A share in progress: the batches it was split into and which one is next. */
interface Sequence {
  batches: ShareManifestFile[][]
  index: number
}

/**
 * Drives sharing photos into the phone's own library: asks the backend what the
 * selection is as files, splits it into batches a phone can hold, fetches one batch
 * at a time and hands each to `navigator.share()`, whence iOS offers "Save Images"
 * into Apple Photos and Android offers Google Photos.
 *
 * The hook owns everything except the rendering, so a component only reflects
 * {@link ShareStatus} and {@link ShareError} — and the whole sequence can be tested
 * without a share sheet anywhere near it.
 *
 * Four properties are deliberate and worth keeping:
 *
 * - **One tap per batch.** `navigator.share()` requires a fresh user gesture, so
 *   after each handoff the sequence pauses in `waiting` instead of chaining, which
 *   on iOS would throw.
 * - **One batch in memory.** Files are fetched per batch and dropped once it has
 *   been handed over, so a four-hundred-photo selection never holds four hundred
 *   files at once.
 * - **A cancelled sheet is not an error.** An `AbortError` ends the sequence
 *   quietly; a photo that would not download names itself and the remaining batches
 *   stay shareable.
 * - **Nothing is truncated.** Every photo of the selection is in some batch, and
 *   the sequence only ends when the last one has been offered.
 *
 * @param photoUids The selection to share, read when a sequence starts. It may
 *   change afterwards (more tiles picked, the grid reloaded) without disturbing a
 *   sequence already under way — its batches are fixed at their photos.
 */
export function usePhotoShare(photoUids: readonly string[]): UsePhotoShareResult {
  // Probed once per mount: the answer is a property of the browser, and a probe per
  // render would build a throwaway File every time.
  const [supported] = useState(canSharePhotoFiles)
  const [status, setStatus] = useState<ShareStatus>({ kind: 'idle' })
  const [error, setError] = useState<ShareError | null>(null)
  const sequence = useRef<Sequence | null>(null)
  // Guards every state write and every fetch against an unmount mid-sequence: the
  // batch bar disappears the moment the selection is cleared, which can happen
  // while a batch is still downloading.
  const live = useRef(true)
  const abort = useRef<AbortController | null>(null)
  // The selection as of this render, so `share` can start on it without taking a new
  // identity every time a tile is picked.
  const uids = useRef(photoUids)
  uids.current = photoUids

  useEffect(() => {
    // Re-armed on mount, not just cleared on unmount: React remounts an effect in
    // development, and a one-way flag would leave the hook permanently mute.
    live.current = true
    return () => {
      live.current = false
      abort.current?.abort()
    }
  }, [])

  const idle = status.kind === 'idle'
  const selectionKey = photoUids.join('\n')
  const lastSelection = useRef(selectionKey)
  useEffect(() => {
    if (lastSelection.current === selectionKey) {
      return
    }
    lastSelection.current = selectionKey
    // A new selection is a new question, so a message about the previous one is no
    // longer about anything on screen. A sequence in flight — or waiting for its
    // next tap — is left alone: those batches were asked for and stay shareable.
    if (idle && sequence.current === null) {
      setError(null)
    }
  }, [selectionKey, idle])

  /**
   * Fetches the current batch's files, reporting progress as they arrive. A file
   * that will not download is skipped and named — the rest of the batch is still
   * worth sharing — and the names of the failures come back with the files.
   */
  const collectBatch = useCallback(
    async (seq: Sequence): Promise<{ files: File[]; failed: string[] }> => {
      const batch = seq.batches[seq.index]
      const files: File[] = []
      const failed: string[] = []
      const position = { batch: seq.index + 1, batches: seq.batches.length }
      setStatus({ kind: 'fetching', ...position, done: 0, total: batch.length })
      for (const [index, entry] of batch.entries()) {
        try {
          files.push(await fetchShareFile(entry, abort.current?.signal))
        } catch (err) {
          if (isShareAbort(err)) {
            throw err
          }
          failed.push(entry.name)
        }
        if (!live.current) {
          throw new DOMException('the share control was unmounted', 'AbortError')
        }
        setStatus({ kind: 'fetching', ...position, done: index + 1, total: batch.length })
      }
      return { files, failed }
    },
    [],
  )

  /**
   * Moves past the batch just dealt with: pauses for the next tap, or ends the
   * sequence when that was the last one. Any error already reported stays visible —
   * it is about a photo, not about this step.
   */
  const advance = useCallback((seq: Sequence) => {
    const next = seq.index + 1
    if (next >= seq.batches.length) {
      sequence.current = null
      setStatus({ kind: 'idle' })
      return
    }
    sequence.current = { batches: seq.batches, index: next }
    setStatus({ kind: 'waiting', batch: next + 1, batches: seq.batches.length })
  }, [])

  /** Hands a fetched batch to the share sheet, translating its outcome. */
  const handOver = useCallback(
    async (seq: Sequence, files: File[]) => {
      setStatus({ kind: 'sharing', batch: seq.index + 1, batches: seq.batches.length })
      try {
        await navigator.share({ files })
      } catch (err) {
        if (!live.current) {
          return
        }
        if (isShareAbort(err)) {
          // The reader closed the sheet. That is a decision: stop, say nothing.
          sequence.current = null
          setStatus({ kind: 'idle' })
          return
        }
        // The sheet refused for another reason. Stay on this batch so the next tap
        // retries it rather than skipping the photos in it.
        setError({ kind: 'sheet' })
        setStatus({ kind: 'waiting', batch: seq.index + 1, batches: seq.batches.length })
        return
      }
      if (live.current) {
        advance(seq)
      }
    },
    [advance],
  )

  /** Fetches one batch and hands it over. */
  const runBatch = useCallback(
    async (seq: Sequence) => {
      const { files, failed } = await collectBatch(seq)
      if (failed.length > 0) {
        setError({ kind: 'fetch', name: failed[0], count: failed.length })
      }
      if (files.length === 0) {
        // Nothing downloaded, so there is nothing to hand over — but the batches
        // after this one are still worth offering.
        advance(seq)
        return
      }
      await handOver(seq, files)
    },
    [advance, collectBatch, handOver],
  )

  /** Asks the backend what the selection is and splits it into batches. */
  const startSequence = useCallback(async (): Promise<Sequence | null> => {
    setStatus({ kind: 'manifest' })
    let files: ShareManifestFile[]
    try {
      files = await fetchShareManifest([...uids.current], abort.current?.signal)
    } catch (err) {
      if (!live.current || isShareAbort(err)) {
        return null
      }
      const tooMany = err instanceof ApiError && err.status === 413
      setError(tooMany ? { kind: 'tooMany' } : { kind: 'manifest' })
      setStatus({ kind: 'idle' })
      return null
    }
    const batches = splitShareBatches(files)
    if (batches.length === 0) {
      setStatus({ kind: 'idle' })
      return null
    }
    return { batches, index: 0 }
  }, [])

  const share = useCallback(() => {
    // Nothing is fetched where the sheet cannot take files: the control is not
    // rendered there, and a share that would end in a TypeError is not worth the
    // bytes even if something else calls this.
    if (!supported) {
      return
    }
    // A tap while a batch is downloading or a sheet is open is not a second share.
    if (status.kind !== 'idle' && status.kind !== 'waiting') {
      return
    }
    setError(null)
    abort.current = new AbortController()
    void (async () => {
      try {
        const seq = sequence.current ?? (await startSequence())
        if (seq === null || !live.current) {
          return
        }
        sequence.current = seq
        await runBatch(seq)
      } catch (err) {
        if (!live.current || isShareAbort(err)) {
          return
        }
        setError({ kind: 'sheet' })
        setStatus({ kind: 'idle' })
      }
    })()
  }, [runBatch, startSequence, status.kind, supported])

  const busy = status.kind === 'manifest' || status.kind === 'fetching' || status.kind === 'sharing'
  return { supported, status, error, busy, share }
}

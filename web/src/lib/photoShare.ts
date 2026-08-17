/**
 * The rules of handing photos to the phone's own share sheet — the arithmetic and
 * the browser check, with no fetching and no React in sight, so they can be
 * reasoned about (and tested) without a share sheet anywhere near.
 *
 * The flow itself lives in `hooks/usePhotoShare`, the fetching in
 * `services/share`. What is here is why a share is split at all: a phone hands the
 * files to Fotky/Photos through memory, so the page must hold a whole batch of
 * originals at once, and a selection of four hundred holiday photos is several
 * hundred megabytes. Everything below is that limit and its consequences.
 */

/** One file of a share, as the backend's share manifest describes it. */
export interface ShareManifestFile {
  /** The photo whose bytes to fetch. */
  uid: string
  /** The name to hand the file over under; unique within one manifest. */
  name: string
  /** The type to label it with (`image/jpeg`, `video/mp4`, …). */
  mime: string
  /** How many bytes to budget for it — for a preview, the original's size. */
  size: number
  /** Fetch the largest cached JPEG preview instead of the original (a RAW). */
  preview: boolean
}

/**
 * How many files one share-sheet handoff may carry. Twenty is what a phone
 * reliably accepts: iOS starts refusing (and Android's picker starts truncating)
 * well before a hundred, and twenty pictures is also about as much as anyone
 * inspects in the sheet before tapping "Save".
 */
export const SHARE_MAX_FILES = 20

/**
 * How many bytes one handoff may carry, roughly. The whole batch is in the page's
 * memory at once — that is what a `File` is — and a mobile browser is killed, not
 * slowed, when it asks for too much. 300 MB leaves room for the twenty-file case
 * to be big pictures rather than small ones.
 */
export const SHARE_MAX_BYTES = 300 * 1024 * 1024

/**
 * The cached JPEG sizes a RAW photo is shared as, largest first. The largest that
 * answers wins: `fit_3840` is what a phone library deserves, and the smaller ones
 * are the fallback for a photo whose big previews were never generated (or whose
 * generation fails on a RAW the converter cannot open).
 */
export const SHARE_PREVIEW_SIZES = ['fit_3840', 'fit_2560', 'fit_1920', 'fit_1280'] as const

/**
 * Splits a selection into the batches one share each. A batch ends when adding the
 * next file would put it over either limit — never mid-file, and never by dropping
 * one: a single file bigger than the byte budget gets a batch of its own, because
 * the alternative is silently not sharing the photo the user picked.
 *
 * An empty selection yields no batches, so a caller can treat "nothing to share"
 * as the absence of work rather than a special case.
 */
export function splitShareBatches(files: readonly ShareManifestFile[]): ShareManifestFile[][] {
  const batches: ShareManifestFile[][] = []
  let current: ShareManifestFile[] = []
  let bytes = 0
  for (const file of files) {
    const wouldOverflow = current.length >= SHARE_MAX_FILES || bytes + file.size > SHARE_MAX_BYTES
    if (wouldOverflow && current.length > 0) {
      batches.push(current)
      current = []
      bytes = 0
    }
    current.push(file)
    bytes += file.size
  }
  if (current.length > 0) {
    batches.push(current)
  }
  return batches
}

/**
 * Whether this browser can hand *files* to a share sheet at all.
 *
 * It is asked with a real probe file rather than by looking for `navigator.share`,
 * because the two capabilities are separate: desktop Chrome on Linux has
 * `share()` but refuses files, and `canShare({files})` is the only honest answer.
 * Where it says no — desktop Firefox, Linux, an insecure origin — the button is
 * never rendered and the ZIP download stays the answer. Rendering a control that
 * throws on tap is worse than not offering it.
 */
export function canSharePhotoFiles(): boolean {
  // Read through a shape that admits both may be missing: the DOM typings declare
  // `share`/`canShare` as always present, which is exactly the assumption this
  // function exists to check.
  const shareable: {
    share?: unknown
    canShare?: (data: { files: File[] }) => boolean
  } = navigator
  if (typeof shareable.share !== 'function' || typeof shareable.canShare !== 'function') {
    return false
  }
  try {
    const probe = new File([new Uint8Array([0xff, 0xd8])], 'kukatko.jpg', { type: 'image/jpeg' })
    return shareable.canShare({ files: [probe] })
  } catch {
    // A browser that throws on the probe cannot share files either.
    return false
  }
}

/**
 * Whether an error is the user closing the share sheet without choosing anything.
 * The Web Share API reports that as an `AbortError`, and it is a decision, not a
 * failure: the sequence stops and nothing is said about it.
 *
 * The name is duck-typed rather than checked against `DOMException`, because which
 * exception class a browser rejects with is not something to bet the quiet path on
 * (Safari's is not the same object as Chrome's), while the `AbortError` name is
 * what the specification actually promises.
 */
export function isShareAbort(error: unknown): boolean {
  return (
    typeof error === 'object' && error !== null && 'name' in error && error.name === 'AbortError'
  )
}

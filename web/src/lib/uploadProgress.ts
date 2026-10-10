/**
 * How complete an upload batch is, as one number a progress bar can show.
 *
 * Sending a file's last byte is not the end of it: the server still hashes it,
 * cuts its thumbnails and answers with a verdict (created, duplicate, refused).
 * A bar that counted bytes alone stood at 100 % while the count beside it still
 * said "4 z 6 fotek", and a full bar reads as "done" — people closed the page.
 *
 * So each file's share of the bar is split in two: sending fills
 * {@link SEND_SHARE} of it, the server's verdict the rest. Nothing is at 100 %
 * until every file has a verdict, which is exactly when the page shows its done
 * screen. Pure on purpose, so the weighting is unit-tested without React.
 */

/**
 * The part of a file's share filled by sending its bytes; the remaining tenth
 * waits for the server's verdict. Ninety per cent keeps the bar honest about
 * the slow part a person can see (the transfer) while leaving a visible step
 * for the processing that follows it.
 */
export const SEND_SHARE = 0.9

/** The part of a queued file the weighting reads (structural, like `lib/uploadEta`). */
interface ProgressUpload {
  file: { size: number }
  status: string
  progress: number
  interrupted?: boolean
}

/** What one file's share of the bar is weighted by. */
export type ProgressWeight = 'bytes' | 'files'

/** Whether the file has its server verdict. */
function hasVerdict(item: ProgressUpload): boolean {
  return (
    item.status === 'created' ||
    item.status === 'duplicate' ||
    (item.status === 'error' && item.interrupted !== true)
  )
}

/**
 * One file's completion in `[0, 1]`: a verdict makes it whole, a running upload
 * fills {@link SEND_SHARE} by its sent fraction, and a waiting file — or one
 * whose request was interrupted, because it goes again from its first byte —
 * stands at zero.
 */
export function fileCompletion(item: ProgressUpload): number {
  if (hasVerdict(item)) {
    return 1
  }
  if (item.status === 'uploading') {
    return SEND_SHARE * Math.min(1, Math.max(0, item.progress))
  }
  return 0
}

/**
 * The batch's completion in `[0, 1]`, each file weighted by its size
 * (`bytes`: a video is not one small step like a photo) or equally (`files`).
 * It is 1 only once every file has a verdict; zero for an empty batch.
 */
export function batchCompletion(
  items: readonly ProgressUpload[],
  weight: ProgressWeight = 'bytes',
): number {
  let done = 0
  let total = 0
  for (const item of items) {
    const share = weight === 'bytes' ? item.file.size : 1
    total += share
    done += share * fileCompletion(item)
  }
  if (total <= 0) {
    // A batch of empty files has no bytes to weigh: count files instead.
    return items.length > 0 && weight === 'bytes' ? batchCompletion(items, 'files') : 0
  }
  return Math.min(1, done / total)
}

/**
 * True while every byte has gone and the batch only waits for the server: no
 * file is waiting or due to go again, and every running one has sent all of it.
 * This is when a remaining-time estimate has nothing left to estimate, so the
 * page says "Zpracovávám…" instead.
 */
export function awaitingVerdicts(items: readonly ProgressUpload[]): boolean {
  let running = false
  for (const item of items) {
    if (item.status === 'queued' || (item.status === 'error' && item.interrupted === true)) {
      return false
    }
    if (item.status === 'uploading') {
      if (item.progress < 1) {
        return false
      }
      running = true
    }
  }
  return running
}

/**
 * A completion fraction as the whole percentage to print. Rounded *down*, so a
 * batch one verdict short of done reads 99 %, never a rounded-up 100 %.
 */
export function progressPercent(fraction: number): number {
  return Math.min(100, Math.max(0, Math.floor(fraction * 100)))
}

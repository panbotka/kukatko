import { type TFunction } from 'i18next'

/**
 * The part of a queued file these helpers read. Structural rather than the
 * queue's own item type, so the queue hook can use them without an import cycle.
 */
interface FailedUpload {
  status?: string
  error?: string
  errorCode?: string
}

/**
 * The per-file refusal codes the backend sends (`ingest.FileResult.code`) whose
 * reason is the file itself: its bytes are not a photo or a video
 * (`not_media`), its type is one Kukátko does not take (`unsupported_type`),
 * or it is an image whose data is cut short or corrupt (`damaged`).
 * Sending the same file again would be refused the same way, so no retry is
 * offered for them.
 */
const PERMANENT_CODES: ReadonlySet<string> = new Set(['not_media', 'unsupported_type', 'damaged'])

/** True when `code` names a refusal a retry cannot change. */
export function isPermanentUploadError(code: string | undefined): boolean {
  return code !== undefined && PERMANENT_CODES.has(code)
}

/** True when `item` failed for a reason a retry might fix. */
export function canRetryUpload(item: FailedUpload): boolean {
  return item.status === 'error' && !isPermanentUploadError(item.errorCode)
}

/**
 * The failure line shown under a file: the translated sentence for a refusal
 * the backend names with a code, the server's own message otherwise (nothing
 * better is known about it), `undefined` when there is nothing to say.
 */
export function uploadErrorMessage(item: FailedUpload, t: TFunction): string | undefined {
  if (item.errorCode === 'not_media') {
    return t('upload.error.not_media')
  }
  if (item.errorCode === 'unsupported_type') {
    return t('upload.error.unsupported_type')
  }
  if (item.errorCode === 'damaged') {
    return t('upload.error.damaged')
  }
  return item.error !== undefined && item.error !== '' ? item.error : undefined
}

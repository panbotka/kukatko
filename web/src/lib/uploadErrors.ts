import { type TFunction } from 'i18next'

import { formatBytes } from './format'

/**
 * The part of a queued file these helpers read. Structural rather than the
 * queue's own item type, so the queue hook can use them without an import cycle.
 */
interface FailedUpload {
  status?: string
  error?: string
  errorCode?: string
  errorLimitBytes?: number
  interrupted?: boolean
}

/**
 * The per-file refusal codes the backend sends (`ingest.FileResult.code`) whose
 * reason is the file itself: its bytes are not a photo or a video
 * (`not_media`), its type is one Kukátko does not take (`unsupported_type`),
 * or it is an image whose data is cut short or corrupt (`damaged`) — and the
 * project limits: the file is over the per-file cap (`file_too_large`), or the
 * project's storage is full (`quota_exceeded`).
 * Sending the same file again would be refused the same way, so no retry is
 * offered for them.
 */
const PERMANENT_CODES: ReadonlySet<string> = new Set([
  'not_media',
  'unsupported_type',
  'damaged',
  'file_too_large',
  'quota_exceeded',
])

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
 * better is known about it), `undefined` when there is nothing to say. A file
 * over the size cap names the cap when the server sent it; the full storage
 * never names a number.
 */
export function uploadErrorMessage(
  item: FailedUpload,
  t: TFunction,
  locale?: string,
): string | undefined {
  if (item.errorCode === 'file_too_large') {
    return item.errorLimitBytes !== undefined
      ? t('upload.error.file_too_large', { limit: formatBytes(item.errorLimitBytes, locale) })
      : t('upload.error.file_too_large_unknown')
  }
  if (item.errorCode === 'quota_exceeded') {
    return t('upload.error.quota_exceeded')
  }
  if (item.errorCode === 'not_media') {
    return t('upload.error.not_media')
  }
  if (item.errorCode === 'unsupported_type') {
    return t('upload.error.unsupported_type')
  }
  if (item.errorCode === 'damaged') {
    return t('upload.error.damaged')
  }
  if (item.interrupted === true) {
    return t('upload.error.interrupted')
  }
  return item.error !== undefined && item.error !== '' ? item.error : undefined
}

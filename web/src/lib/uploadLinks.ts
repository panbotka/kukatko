import { type TFunction } from 'i18next'

import { type UploadSummary } from '../hooks/useUploadQueue'
import { type PhotoUploadLinkRef } from '../services/photos'

/** The curators' management page of upload links. */
export const UPLOAD_LINKS_PATH = '/upload-links'

/** The query parameter that opens the create form with one album preselected. */
export const ALBUM_PARAM = 'album'

/**
 * The device-level key remembering the "from whom" name an anonymous uploader
 * typed. Deliberately not account-scoped and not cleared at sign-out: it belongs
 * to the phone, not to an account (see "Browser storage" in docs/FRONTEND.md).
 */
export const UPLOADER_NAME_KEY = 'kukatko.uploadLink.uploaderName'

/** The create-form address with `albumUid` already chosen (the album page's action). */
export function newLinkForAlbum(albumUid: string): string {
  return `${UPLOAD_LINKS_PATH}?${ALBUM_PARAM}=${encodeURIComponent(albumUid)}`
}

/**
 * The one-line outcome of a finished batch — "12 nahráno, 1 duplicita, 1 chyba"
 * — naming only the parts that happened (the uploaded count always).
 */
export function uploadLinkSummary(summary: UploadSummary, t: TFunction): string {
  const parts = [t('uploadLink.summary.created', { count: summary.created })]
  if (summary.duplicate > 0) {
    parts.push(t('uploadLink.summary.duplicate', { count: summary.duplicate }))
  }
  if (summary.error > 0) {
    parts.push(t('uploadLink.summary.error', { count: summary.error }))
  }
  return parts.join(', ')
}

/**
 * Who sent a photo through an upload link, as one name: the name the guest typed
 * on the public page, otherwise the account the upload is attributed to (signed
 * in, or claimed at registration), otherwise `undefined` — nobody said. The typed
 * name wins over the account because it is what the sender chose to sign with;
 * the account is shown anyway as the photo's uploader in the technical details.
 */
export function uploadLinkSender(link: PhotoUploadLinkRef): string | undefined {
  const typed = link.uploader_name?.trim() ?? ''
  if (typed !== '') {
    return typed
  }
  const account = link.account?.name.trim() ?? ''
  return account !== '' ? account : undefined
}

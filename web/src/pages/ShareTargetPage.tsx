import { useEffect } from 'react'
import Alert from 'react-bootstrap/Alert'
import { useTranslation } from 'react-i18next'
import { Link, Navigate, useSearchParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { LIBRARY_PATH } from '../lib/libraryView'
import { discardSharedFiles } from '../pwa/shareTarget'
import { SHARE_PARAM } from '../pwa/shareContract'

/** Where a share that may be uploaded is forwarded to, carrying its id. */
const UPLOAD_PATH = '/upload'

/**
 * Where the phone's share sheet lands (`/share-target`, the manifest's
 * `share_target.action`). It is a junction, not a destination: the service
 * worker has already stashed the shared files by the time this renders, and all
 * that is left is to decide who may take them.
 *
 * Three ways through:
 *
 *  - **An editor with a staged share** is forwarded to the upload page, which
 *    collects the files and queues them. `replace` keeps the junction out of
 *    history, so Back from the upload page leaves the app rather than bouncing
 *    through here again — and the share id is single-use anyway.
 *  - **A viewer** is told, plainly, that their account may not upload. Their
 *    photos are discarded there and then instead of waiting out the cache TTL.
 *  - **No share id at all** means the files did not come through: either the
 *    worker could not read the POST, or none was installed to intercept it and
 *    the server answered the POST with the app shell. Both end here with the
 *    same honest sentence and a way to the picker.
 *
 * The route sits inside `RequireAuth` but outside the editor gate, so a visitor
 * who is not signed in goes to login first and comes back to this exact URL —
 * the files wait for them in the cache, which is what lets a share survive the
 * login round trip.
 */
export function ShareTargetPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('share.title'))
  const { canWrite } = useAuth()
  const [searchParams] = useSearchParams()
  const shareId = searchParams.get(SHARE_PARAM) ?? ''

  // A share nobody may upload is thrown away rather than left to expire.
  useEffect(() => {
    if (!canWrite && shareId !== '') {
      void discardSharedFiles(shareId)
    }
  }, [canWrite, shareId])

  if (canWrite && shareId !== '') {
    return <Navigate to={`${UPLOAD_PATH}?${SHARE_PARAM}=${encodeURIComponent(shareId)}`} replace />
  }

  return (
    <div className="py-4" data-testid="share-target-page">
      <h1 className="kk-page-title mb-3">{t('share.title')}</h1>
      {canWrite ? (
        <>
          <Alert variant="warning">{t('share.missing.message')}</Alert>
          <Link to={UPLOAD_PATH} className="btn btn-primary">
            {t('share.missing.action')}
          </Link>
        </>
      ) : (
        <>
          <Alert variant="warning">{t('share.forbidden.message')}</Alert>
          <Link to={LIBRARY_PATH} className="btn btn-primary">
            {t('share.forbidden.action')}
          </Link>
        </>
      )}
    </div>
  )
}

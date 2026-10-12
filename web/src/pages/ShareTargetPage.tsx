import Alert from 'react-bootstrap/Alert'
import { useTranslation } from 'react-i18next'
import { Navigate, useSearchParams } from 'react-router-dom'

import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useProjectPath } from '../project/ProjectContext'
import { ProjectLink } from '../project/ProjectLink'
import { SHARE_PARAM } from '../pwa/shareContract'

/** Where a staged share is forwarded to, carrying its id. */
const UPLOAD_PATH = '/upload'

/**
 * Where the phone's share sheet lands (`/share-target`, the manifest's
 * `share_target.action`). It is a junction, not a destination: the service
 * worker has already stashed the shared files by the time this renders, and all
 * that is left is to hand them on.
 *
 * Two ways through:
 *
 *  - **A staged share** is forwarded to the upload page, which collects the files
 *    and queues them — for every signed-in role, since anybody signed in may add
 *    photos. `replace` keeps the junction out of history, so Back from the upload
 *    page leaves the app rather than bouncing through here again — and the share
 *    id is single-use anyway.
 *  - **No share id at all** means the files did not come through: either the
 *    worker could not read the POST, or none was installed to intercept it and
 *    the server answered the POST with the app shell. Both end here with the
 *    same honest sentence and a way to the picker.
 *
 * The route sits inside `RequireAuth`, so a visitor who is not signed in goes to
 * login first and comes back to this exact URL — the files wait for them in the
 * cache, which is what lets a share survive the login round trip.
 */
export function ShareTargetPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('share.title'))
  const [searchParams] = useSearchParams()
  const projectPath = useProjectPath()
  const shareId = searchParams.get(SHARE_PARAM) ?? ''

  if (shareId !== '') {
    return (
      <Navigate
        to={projectPath(`${UPLOAD_PATH}?${SHARE_PARAM}=${encodeURIComponent(shareId)}`)}
        replace
      />
    )
  }

  return (
    <div className="py-4" data-testid="share-target-page">
      <h1 className="kk-page-title mb-3">{t('share.title')}</h1>
      <Alert variant="warning">{t('share.missing.message')}</Alert>
      <ProjectLink to={UPLOAD_PATH} className="btn btn-primary">
        {t('share.missing.action')}
      </ProjectLink>
    </div>
  )
}

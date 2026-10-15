import { useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Container from 'react-bootstrap/Container'
import ListGroup from 'react-bootstrap/ListGroup'
import { useTranslation } from 'react-i18next'
import { Link, Navigate, useSearchParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { Icon } from '../components/Icon'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { PROJECTS_PATH, isEnterable, projectPathFor } from '../project/paths'
import { SHARE_PARAM } from '../pwa/shareContract'

/** The upload page, the page of a project a staged share is forwarded to. */
const UPLOAD_PATH = '/upload'

/** The upload page of the project `slug`, picking up the staged share `shareId`. */
function uploadPathFor(slug: string, shareId: string): string {
  return projectPathFor(slug, `${UPLOAD_PATH}?${SHARE_PARAM}=${encodeURIComponent(shareId)}`)
}

/**
 * Where the phone's share sheet lands (`/share-target`, the manifest's
 * `share_target.action`). It is instance-level — the share sheet knows nothing
 * of projects — and it is a junction, not a destination: the service worker has
 * already stashed the shared files by the time this renders, and all that is
 * left is to decide **which library** they go into. That decision is the
 * account's, never a guess: the upload page starts uploading the moment it has
 * the files, so whatever this page forwards to is where the photos end up.
 *
 * With a staged share (`?share=<id>`), by the account's enterable projects
 * (approved memberships of active projects):
 *
 *  - **One** — there is nothing to choose, so it forwards straight to that
 *    project's upload page, which names the project above the batch.
 *  - **Several** — it asks: one entry per project, the one last worked in only
 *    marked, never preselected. Nothing is read out of the cache, let alone
 *    uploaded, until the account picks.
 *  - **None** (no membership yet, or one still waiting for approval) — the files
 *    are left where they are, and the page says so: they wait on this device
 *    for a day, and "check again" re-reads the session, so an approval that
 *    lands meanwhile carries the share on from this same page.
 *
 * Forwarding uses `replace`, so Back from the upload page leaves the app rather
 * than walking into a spent share (the id is single-use anyway).
 *
 * **No share id at all** means the files did not come through: either the
 * worker could not read the POST, or none was installed to intercept it and the
 * server answered the POST with the app shell. That ends here with an honest
 * sentence and a way to the picker — the one project's upload page, or the
 * chooser when there is no single one.
 *
 * The route sits inside `RequireAuth`, so a visitor who is not signed in goes to
 * login first and comes back to this exact URL — the files wait for them in the
 * cache, which is what lets a share survive the login round trip.
 */
export function ShareTargetPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('share.title'))
  const [searchParams] = useSearchParams()
  const { projects, lastProjectSlug } = useAuth()
  const shareId = searchParams.get(SHARE_PARAM) ?? ''
  const enterable = projects.filter(isEnterable)

  if (shareId !== '' && enterable.length === 1) {
    return <Navigate to={uploadPathFor(enterable[0].slug, shareId)} replace />
  }

  let body
  if (shareId === '') {
    const only = enterable.length === 1 ? enterable[0].slug : null
    body = (
      <>
        <Alert variant="warning">{t('share.missing.message')}</Alert>
        <Link to={projectPathFor(only, UPLOAD_PATH)} className="btn btn-primary kukatko-tap-target">
          {t(only === null ? 'share.missing.choose' : 'share.missing.action')}
        </Link>
      </>
    )
  } else if (enterable.length === 0) {
    body = <ShareWaiting />
  } else {
    body = (
      <>
        <p>{t('share.choose.intro')}</p>
        <ListGroup data-testid="share-project-choices">
          {enterable.map((membership) => (
            <ListGroup.Item
              key={membership.uid}
              action
              as={Link}
              to={uploadPathFor(membership.slug, shareId)}
              replace
              className="d-flex align-items-center gap-2 py-3 kukatko-tap-target"
            >
              <Icon name="images" />
              <span className="me-auto">{membership.name}</span>
              {membership.slug === lastProjectSlug && (
                <Badge bg="secondary">{t('share.choose.last')}</Badge>
              )}
            </ListGroup.Item>
          ))}
        </ListGroup>
      </>
    )
  }

  return (
    <Container as="main" className="pt-3 pt-md-4 kukatko-main" data-testid="share-target-page">
      <h1 className="kk-page-title mb-3">
        {shareId !== '' && enterable.length > 1 ? t('share.choose.title') : t('share.title')}
      </h1>
      {body}
    </Container>
  )
}

/**
 * A share staged for an account with no project to put it in. The files stay
 * in the cache untouched — collecting them is the upload page's job, and only a
 * chosen project's upload page may do it — and "check again" re-reads the
 * session, after which {@link ShareTargetPage} re-renders and carries the share
 * on if a membership was approved meanwhile.
 */
function ShareWaiting() {
  const { t } = useTranslation()
  const { refresh } = useAuth()
  const [checking, setChecking] = useState(false)

  async function checkAgain() {
    setChecking(true)
    try {
      await refresh()
    } catch {
      // A failed re-read changes nothing: the files are still waiting, and the
      // button is there to be pressed again.
    } finally {
      setChecking(false)
    }
  }

  return (
    <>
      <Alert variant="info" data-testid="share-waiting">
        <p className="mb-2">{t('share.waiting.message')}</p>
        <p className="mb-0">{t('share.waiting.kept')}</p>
      </Alert>
      <div className="d-flex flex-wrap gap-2">
        <Button
          variant="primary"
          className="kukatko-tap-target"
          disabled={checking}
          onClick={() => {
            void checkAgain()
          }}
        >
          {t('share.waiting.check')}
        </Button>
        <Link to={PROJECTS_PATH} className="btn btn-outline-secondary kukatko-tap-target">
          {t('share.waiting.projects')}
        </Link>
      </div>
    </>
  )
}

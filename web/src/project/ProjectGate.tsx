import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Outlet, useParams } from 'react-router-dom'

import { Layout } from '../components/Layout'
import { NotFoundPage } from '../pages/NotFoundPage'
import { PendingApprovalPage } from '../pages/PendingApprovalPage'

import { useProject } from './ProjectContext'

/**
 * The element of `/p/:slug`: the project's pages when the account may enter
 * the project the address names, and otherwise one of two refusals inside the
 * shell — whose chrome is neutral there, so no other project's name, navigation
 * or banner claims the page (see `useNeutralChrome`):
 *
 * - a membership of that project still waiting for its admin's approval gets
 *   {@link PendingApprovalPage} — the account already knows the project (it
 *   registered into it, the chooser lists it), so saying "waiting" reveals
 *   nothing and spares it a puzzling "page not found";
 * - anything else — no membership, a deactivated project, no such project — is
 *   the very same not-found page an unknown address gets: a project the reader
 *   may not enter must look exactly like one that does not exist.
 *
 * While the maintainer's project it is no member of is being looked up the
 * gate shows a spinner instead of guessing either way.
 *
 * The outlet is keyed by the project, so **a project switch remounts every page
 * below it**. Without the key, `/p/a/albums` → `/p/b/albums` matches the same
 * route element at the same place, React keeps the page, and its fetch effects
 * — keyed on the page's own inputs, never on the project — leave project a's
 * albums on screen under b's name, with every click sending a's uids to b's
 * API. Remounting throws away all of a page's state at once (its data, its
 * selection, its upload queue) and runs the effects' cleanups, which abort the
 * requests still in flight for a, so a late answer for a has nowhere to land.
 * Page code therefore never has to put the project in its dependencies.
 */
export function ProjectGate() {
  const { t } = useTranslation()
  const { status } = useProject()
  const { slug } = useParams()
  if (status === 'loading') {
    return (
      <div
        className="d-flex justify-content-center align-items-center"
        style={{ minHeight: '60vh' }}
      >
        <Spinner animation="border" role="status">
          <span className="visually-hidden">{t('auth.loading')}</span>
        </Spinner>
      </div>
    )
  }
  if (status === 'pending') {
    return (
      <Layout>
        <PendingApprovalPage />
      </Layout>
    )
  }
  if (status === 'not-member') {
    return (
      <Layout>
        <NotFoundPage />
      </Layout>
    )
  }
  return <Outlet key={slug} />
}

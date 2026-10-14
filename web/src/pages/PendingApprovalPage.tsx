import { useContext } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useLocation } from 'react-router-dom'

import { EmptyState } from '../components/EmptyState'
import { Icon } from '../components/Icon'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { ProjectContext } from '../project/ProjectContext'
import { PROJECTS_PATH, projectSlugInPath } from '../project/paths'

/**
 * What `/p/<slug>` shows an account whose membership of that (active) project
 * still waits for the project admin's approval — the SPA's side of the API's
 * `403 pending_approval`. It names the project (the account knows it: it
 * registered into it, and the chooser lists it as waiting) and points to the
 * chooser, where the account's other projects are. Only this state gets it:
 * every other refusal is the ordinary not-found page.
 */
export function PendingApprovalPage() {
  const { t } = useTranslation()
  const { pathname } = useLocation()
  const slug = projectSlugInPath(pathname)
  const name = useContext(ProjectContext)?.projects.find((m) => m.slug === slug)?.name ?? slug ?? ''
  useDocumentTitle(t('pendingApproval.title'))

  return (
    <div data-testid="pending-approval-page">
      <EmptyState
        titleAs="h1"
        icon={<Icon name="hourglass-split" />}
        title={t('pendingApproval.title')}
        hint={t('pendingApproval.message', { project: name })}
        action={
          <Link to={PROJECTS_PATH} className="btn btn-primary">
            {t('pendingApproval.projects')}
          </Link>
        }
      />
    </div>
  )
}

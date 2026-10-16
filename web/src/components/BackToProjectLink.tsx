import { useTranslation } from 'react-i18next'

import { useBackToProject } from '../project/backToProject'

import { BackLink } from './BackLink'

/**
 * The way back into a project from a page outside one (`/account`,
 * `/account/activity`, `/system…`, `/maintenance`), at the top of the page's
 * content: the left arrow and „Zpět do projektu <name>" leading to the library
 * of the project the account came from — or „Vybrat projekt", the chooser, when
 * there is no single project to name (see `project/backToProject.ts`).
 *
 * The neutral shell draws no library navigation, so without this the only way
 * back was the plain app title at the top left, which did not look like a link.
 * It is a {@link BackLink} — a router link, the look of every other "back" in
 * the app — so the browser's Back keeps working as it did. Renders nothing
 * without a signed-in account (or a provider) to lead anywhere.
 */
export function BackToProjectLink() {
  const { t } = useTranslation()
  const back = useBackToProject()
  if (back === null) {
    return null
  }
  const label =
    back.project === null
      ? t('backToProject.chooser')
      : t('backToProject.toProject', { name: back.project.name })
  return (
    <div className="kk-instance-back mb-3" data-testid="back-to-project">
      <BackLink to={back.to} label={label} />
    </div>
  )
}

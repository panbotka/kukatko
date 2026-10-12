import { useTranslation } from 'react-i18next'

import { EmptyState } from '../components/EmptyState'
import { Icon } from '../components/Icon'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { LIBRARY_PATH } from '../lib/libraryView'
import { ProjectLink } from '../project/ProjectLink'

/**
 * Fallback page shown for unknown client-side routes. Drawn with the shared
 * {@link EmptyState}, like every other "nothing here" screen in the app, so a
 * mistyped URL reads as part of Kukátko rather than as a bare error page.
 */
export function NotFoundPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('notFound.title'))

  return (
    <div data-testid="not-found-page">
      <EmptyState
        titleAs="h1"
        icon={<Icon name="compass" />}
        title={t('notFound.title')}
        hint={t('notFound.message')}
        action={
          <ProjectLink to={LIBRARY_PATH} className="btn btn-primary">
            {t('notFound.back')}
          </ProjectLink>
        }
      />
    </div>
  )
}

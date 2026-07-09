import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

/** Fallback page shown for unknown client-side routes. */
export function NotFoundPage() {
  const { t } = useTranslation()

  return (
    <div className="text-center py-5">
      <h1 className="kk-page-title mb-3">{t('notFound.title')}</h1>
      <p className="text-secondary mb-4">{t('notFound.message')}</p>
      <Link to="/" className="btn btn-primary">
        {t('notFound.back')}
      </Link>
    </div>
  )
}

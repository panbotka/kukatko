import { useEffect, useRef, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../auth/AuthContext'
import { Icon } from '../components/Icon'
import { useDocumentTitle } from '../hooks/useDocumentTitle'

/**
 * Shown **in place of** a guarded route when the backend could not be reached,
 * so the session is simply unknown (see `AuthStatus`).
 *
 * It replaces a redirect to `/login`, which was the worst answer available: the
 * reader was signed in the whole time, the form could not reach a server to
 * check anything, and the failure it printed accused them of mistyping a
 * password they had not forgotten. This says what actually happened, and offers
 * the one thing that can change it.
 *
 * Rendering rather than redirecting keeps the URL on the route that was asked
 * for — the same reasoning as {@link ForbiddenPage} — so retrying, or reloading
 * once the signal is back, lands on the page the reader wanted.
 */
export function OfflinePage() {
  const { t } = useTranslation()
  useDocumentTitle(t('offline.title'))
  const { refresh } = useAuth()
  const [retrying, setRetrying] = useState(false)
  // A successful retry unmounts this page mid-await (the guard swaps in the
  // route), so the flag is only cleared while we are still on screen.
  const mounted = useRef(true)

  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])

  async function retry() {
    setRetrying(true)
    // refresh() never rejects: a still-unreachable backend just re-files the
    // same status, and we land back here with the button ready again.
    await refresh()
    if (mounted.current) {
      setRetrying(false)
    }
  }

  return (
    <div className="text-center py-5" data-testid="offline-page">
      <Icon name="wifi-off" className="fs-1 text-secondary mb-3 d-block" />
      <h1 className="kk-page-title mb-3">{t('offline.title')}</h1>
      <p className="text-secondary mb-2">{t('offline.message')}</p>
      <p className="text-secondary mb-4">{t('offline.notSignedOut')}</p>
      <Button
        variant="primary"
        disabled={retrying}
        onClick={() => {
          void retry()
        }}
      >
        {retrying && (
          <Spinner animation="border" size="sm" role="status" aria-hidden="true" className="me-2" />
        )}
        {retrying ? t('offline.retrying') : t('offline.retry')}
      </Button>
    </div>
  )
}

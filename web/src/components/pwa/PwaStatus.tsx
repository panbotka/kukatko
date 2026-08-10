import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { usePwaStatus } from '../../pwa/usePwaStatus'
import { Icon } from '../Icon'

/**
 * The installed-app's two ambient messages, in one fixed stack above the bottom
 * edge: "you are offline" and "a new version is ready".
 *
 * It is mounted at the app root rather than inside the layout shell, so it also
 * reaches the routes that render without the shell — the immersive photo
 * viewer, the slideshow and the review game — which are exactly the screens a
 * reader is most likely to be on when the network drops.
 *
 * Nothing renders while the app is online and up to date, so the common case
 * costs one `null`. Registering the service worker happens here too, by way of
 * {@link usePwaStatus}.
 */
export function PwaStatus() {
  const { t } = useTranslation()
  const { offline, updateReady, applyUpdate, dismissUpdate } = usePwaStatus()

  if (!offline && !updateReady) {
    return null
  }

  return (
    <div className="kk-pwa-status">
      {offline && (
        <Alert variant="warning" className="kk-pwa-status__item" role="status" aria-live="polite">
          <Icon name="exclamation-triangle" className="me-2" />
          {t('pwa.offline')}
        </Alert>
      )}
      {updateReady && (
        <Alert variant="info" className="kk-pwa-status__item" role="status" aria-live="polite">
          <Icon name="arrow-clockwise" className="me-2" />
          <span className="me-auto">{t('pwa.update.message')}</span>
          <Button variant="primary" size="sm" onClick={applyUpdate}>
            {t('pwa.update.action')}
          </Button>
          <Button variant="link" size="sm" className="text-body-secondary" onClick={dismissUpdate}>
            {t('pwa.update.dismiss')}
          </Button>
        </Alert>
      )}
    </div>
  )
}

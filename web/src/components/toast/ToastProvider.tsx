import { useCallback, useMemo, useRef, useState } from 'react'
import Toast from 'react-bootstrap/Toast'
import ToastContainer from 'react-bootstrap/ToastContainer'
import { useTranslation } from 'react-i18next'

import { Icon, type IconName } from '../Icon'

import { ToastContext, type ToastApi, type ToastOptions, type ToastVariant } from './ToastContext'

/** A live toast in the stack. */
interface ActiveToast extends ToastOptions {
  id: number
  variant: ToastVariant
}

/** How long a toast stays up before it auto-dismisses. */
const TOAST_DELAY_MS = 5000

/** The leading glyph per tone; icons stay decorative beside the message text. */
const ICONS: Record<ToastVariant, IconName> = {
  success: 'check-lg',
  danger: 'exclamation-triangle',
  info: 'info-circle',
}

/**
 * Hosts the app-wide toast stack. Any descendant can raise a transient
 * success/failure message via `useToast`; the toasts render in a fixed,
 * top-centred container above the rest of the chrome (including the floating
 * batch bar and the immersive viewer) and auto-dismiss, with a manual close for
 * anyone who wants them gone sooner. There is exactly one provider, mounted at
 * the app root.
 */
export function ToastProvider({ children }: { children: React.ReactNode }) {
  const { t } = useTranslation()
  const [toasts, setToasts] = useState<ActiveToast[]>([])
  // A monotonic id keeps React keys stable without a wall-clock/random source.
  const nextId = useRef(0)

  const dismiss = useCallback((id: number) => {
    setToasts((prev) => prev.filter((toast) => toast.id !== id))
  }, [])

  const show = useCallback((options: ToastOptions) => {
    const id = nextId.current
    nextId.current += 1
    setToasts((prev) => [
      ...prev,
      { id, message: options.message, variant: options.variant ?? 'info' },
    ])
  }, [])

  const api = useMemo<ToastApi>(() => ({ show }), [show])

  return (
    <ToastContext.Provider value={api}>
      {children}
      <ToastContainer className="p-3 kk-toast-stack" position="top-center">
        {toasts.map((toast) => (
          <Toast
            key={toast.id}
            bg={toast.variant}
            autohide
            delay={TOAST_DELAY_MS}
            onClose={() => {
              dismiss(toast.id)
            }}
          >
            <Toast.Body className="d-flex align-items-center gap-2 text-white">
              <Icon name={ICONS[toast.variant]} />
              <span className="me-auto">{toast.message}</span>
              <button
                type="button"
                className="btn-close btn-close-white ms-2"
                aria-label={t('toast.close')}
                onClick={() => {
                  dismiss(toast.id)
                }}
              />
            </Toast.Body>
          </Toast>
        ))}
      </ToastContainer>
    </ToastContext.Provider>
  )
}

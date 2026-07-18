import { createContext, useContext } from 'react'

/** The visual tone of a toast, mapped to a Bootstrap contextual background. */
export type ToastVariant = 'success' | 'danger' | 'info'

/** What a caller passes to raise a toast. */
export interface ToastOptions {
  /** The already-translated message shown to the reader. */
  message: string
  /** The tone; defaults to `info`. */
  variant?: ToastVariant
}

/** The one action a component needs: raise a transient message. */
export interface ToastApi {
  /** Shows a toast that auto-dismisses after a few seconds. */
  show: (options: ToastOptions) => void
}

/**
 * Default no-op API, so a component using {@link useToast} outside a
 * `ToastProvider` (e.g. a focused unit test) renders without a provider rather
 * than throwing — the messages are simply dropped.
 */
export const NOOP_TOAST_API: ToastApi = { show: () => undefined }

export const ToastContext = createContext<ToastApi>(NOOP_TOAST_API)

/**
 * Accesses the app's toast API. Returns a no-op outside a `ToastProvider`, so a
 * component can call `show` unconditionally without guarding for a provider.
 */
export function useToast(): ToastApi {
  return useContext(ToastContext)
}

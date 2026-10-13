import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { useToast } from '../components/toast/ToastContext'

/** The part of a queued file the hook reads (structural, like `lib/uploadErrors`). */
interface LimitedUpload {
  status: string
  errorCode?: string
}

/** The toast message key of each project-limit refusal code. */
const TOAST_KEYS = {
  file_too_large: 'upload.toast.fileTooLarge',
  quota_exceeded: 'upload.toast.quotaExceeded',
} as const

/** True when `code` is one of the limit refusals this hook announces. */
function isLimitCode(code: string | undefined): code is keyof typeof TOAST_KEYS {
  return code !== undefined && Object.hasOwn(TOAST_KEYS, code)
}

/**
 * Raises one translated toast when the queue first meets a project limit: a
 * file over the per-file cap (`file_too_large`), or a project whose storage
 * just ran out (`quota_exceeded`). Each file still carries its own reason in the
 * list; the toast is what reaches someone who is not looking at the list.
 *
 * One toast per kind while the kind keeps occurring, not one per file: a
 * fifty-file batch into a full project would otherwise stack fifty identical
 * messages. Once no file in the queue carries a kind any more (the queue was
 * cleared, or the files removed), the next refusal of that kind toasts again.
 */
export function useUploadLimitToasts(items: readonly LimitedUpload[]): void {
  const { t } = useTranslation()
  const toast = useToast()
  const announced = useRef(new Set<string>())

  useEffect(() => {
    const present = new Set<string>()
    for (const item of items) {
      if (item.status === 'error' && isLimitCode(item.errorCode)) {
        present.add(item.errorCode)
      }
    }
    for (const code of announced.current) {
      if (!present.has(code)) {
        announced.current.delete(code)
      }
    }
    for (const code of present) {
      if (!announced.current.has(code) && isLimitCode(code)) {
        announced.current.add(code)
        toast.show({ message: t(TOAST_KEYS[code]), variant: 'danger' })
      }
    }
  }, [items, t, toast])
}

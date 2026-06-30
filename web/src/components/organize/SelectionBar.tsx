import { type ReactNode } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

/** Props for {@link SelectionBar}. */
export interface SelectionBarProps {
  /** Number of selected items, shown in the label. */
  count: number
  /** Leaves selection mode. */
  onCancel: () => void
  /** Action buttons (e.g. add-to-album, remove), rendered after the count. */
  children: ReactNode
}

/**
 * A sticky toolbar shown while a grid is in selection mode: the selected count,
 * the caller's action buttons, and a cancel control to leave selection mode.
 */
export function SelectionBar({ count, onCancel, children }: SelectionBarProps) {
  const { t } = useTranslation()
  return (
    <div
      className="d-flex align-items-center gap-2 flex-wrap bg-body-tertiary border rounded p-2 mb-3 kukatko-sticky-toolbar"
      role="toolbar"
      aria-label={t('selection.label')}
    >
      <span className="fw-semibold me-auto" aria-live="polite">
        {t('selection.count', { count })}
      </span>
      {children}
      <Button variant="outline-secondary" size="sm" onClick={onCancel}>
        {t('selection.cancel')}
      </Button>
    </div>
  )
}

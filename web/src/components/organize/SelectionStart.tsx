import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { type UseBulkEditResult } from '../../hooks/useBulkEdit'

/** Props for {@link SelectionStart}. */
export interface SelectionStartProps {
  /** The bulk-edit state from `useBulkEdit`, owned by the page. */
  bulk: UseBulkEditResult
  /**
   * Overrides the enter action for a page that must leave another mode first.
   * Defaults to `selection.enable`.
   */
  onEnter?: () => void
}

/**
 * The button that puts a photo list into selection mode, the counterpart of
 * `BulkEditControl` in the `SelectionBar` it opens. It is absent for a viewer —
 * who may not bulk edit, so has nothing to select photos for — and while
 * selection mode is already on, since the selection bar owns the page from then
 * until it is cancelled.
 */
export function SelectionStart({ bulk, onEnter }: SelectionStartProps) {
  const { t } = useTranslation()
  if (!bulk.canBulkEdit || bulk.selection.active) {
    return null
  }
  return (
    <Button variant="outline-secondary" size="sm" onClick={onEnter ?? bulk.selection.enable}>
      {t('selection.enter')}
    </Button>
  )
}

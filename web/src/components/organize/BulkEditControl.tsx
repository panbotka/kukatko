import { useTranslation } from 'react-i18next'

import { type UseBulkEditResult } from '../../hooks/useBulkEdit'
import { ReasonedButton } from '../ReasonedButton'

import { BulkEditModal, type BulkEditPrefill } from './BulkEditModal'

/** Props for {@link BulkEditControl}. */
export interface BulkEditControlProps {
  /** The bulk-edit state from `useBulkEdit`, owned by the page. */
  bulk: UseBulkEditResult
  /** Bootstrap button variant of the trigger. Defaults to `primary`. */
  variant?: string
  /** Form values pre-selected when the dialog opens (see `BulkEditPrefill`). */
  prefill?: BulkEditPrefill
}

/**
 * The bulk-edit affordance of a photo list: a trigger button plus the
 * {@link BulkEditModal} it opens, driven entirely by a `useBulkEdit` result. Drop
 * it into a `SelectionBar` and the page gains bulk editing without owning any
 * dialog state.
 *
 * The whole control — trigger and dialog both — is absent for a viewer, who may
 * not write; it goes off while nothing is selected, since an empty batch has
 * nothing to apply to, and then says exactly that. The dialog always submits
 * exactly the selected UIDs, never the filtered result set behind them.
 */
export function BulkEditControl({ bulk, variant = 'primary', prefill }: BulkEditControlProps) {
  const { t } = useTranslation()
  if (!bulk.canBulkEdit) {
    return null
  }
  return (
    <>
      <ReasonedButton
        variant={variant}
        size="sm"
        disabledReason={bulk.selection.count === 0 ? t('selection.editDisabled') : undefined}
        onClick={bulk.open}
      >
        {t('selection.edit')}
      </ReasonedButton>
      <BulkEditModal
        show={bulk.editing}
        photoUids={bulk.photoUids}
        onHide={bulk.close}
        onDone={bulk.finish}
        prefill={prefill}
      />
    </>
  )
}

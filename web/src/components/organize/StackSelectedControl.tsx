import { useState } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { type UseBulkEditResult } from '../../hooks/useBulkEdit'
import { stackPhotos } from '../../services/photos'

/** Props for {@link StackSelectedControl}. */
export interface StackSelectedControlProps {
  /** The bulk-edit state from `useBulkEdit`, owned by the page. */
  bulk: UseBulkEditResult
  /** Bootstrap button variant of the trigger. Defaults to `outline-secondary`. */
  variant?: string
}

/**
 * The "stack selected" affordance of a photo list: it groups the selected photos
 * into one stack (manual stacking, for the shots automatic detection misses) and
 * then clears the selection and reloads the grid. It is absent for a viewer, who
 * may not write, and disabled until at least two photos are selected, since a
 * stack needs two members. On failure the selection is left intact so the reader
 * can retry.
 */
export function StackSelectedControl({
  bulk,
  variant = 'outline-secondary',
}: StackSelectedControlProps) {
  const { t } = useTranslation()
  const [busy, setBusy] = useState(false)
  if (!bulk.canBulkEdit) {
    return null
  }
  const onClick = () => {
    setBusy(true)
    void stackPhotos(bulk.photoUids)
      .then(() => {
        bulk.finish()
      })
      .catch(() => {
        // Leave the selection intact so the reader can retry.
      })
      .finally(() => {
        setBusy(false)
      })
  }
  return (
    <Button
      variant={variant}
      size="sm"
      disabled={busy || bulk.selection.count < 2}
      onClick={onClick}
    >
      {t('selection.stack')}
    </Button>
  )
}

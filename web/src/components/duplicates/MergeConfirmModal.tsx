import type { TFunction } from 'i18next'
import Button from 'react-bootstrap/Button'
import Modal from 'react-bootstrap/Modal'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { type MergeResult } from '../../services/duplicates'

interface MergeConfirmModalProps {
  /** The dry-run preview to confirm, or null to keep the modal closed. */
  preview: MergeResult | null
  /** Whether the confirmed merge is in flight (disables the buttons). */
  busy: boolean
  /** Perform the merge the user confirmed. */
  onConfirm: () => void
  /** Close the modal without merging. */
  onCancel: () => void
}

/**
 * Builds the "+3 albums, +2 tags, +1 person" fragment from a preview, omitting
 * any part with a zero count. Returns null when nothing new would move onto the
 * keeper (e.g. it already carries everything the copies have).
 */
function mergeMoves(preview: MergeResult, t: TFunction): string | null {
  const parts: string[] = []
  if (preview.albums_added > 0) {
    parts.push(t('duplicates.merge.albums', { count: preview.albums_added }))
  }
  if (preview.labels_added > 0) {
    parts.push(t('duplicates.merge.tags', { count: preview.labels_added }))
  }
  if (preview.people_added > 0) {
    parts.push(t('duplicates.merge.people', { count: preview.people_added }))
  }
  if (preview.metadata_filled.length > 0) {
    parts.push(t('duplicates.merge.metadata', { count: preview.metadata_filled.length }))
  }
  return parts.length > 0 ? parts.join(', ') : null
}

/** The body of the confirmation modal: what will move plus what will be archived. */
function MergePreviewBody({ preview }: { preview: MergeResult }) {
  const { t } = useTranslation()
  const moves = mergeMoves(preview, t)
  return (
    <>
      <p className="mb-2">{t('duplicates.merge.body')}</p>
      <p className="mb-0">
        <strong>{moves ?? t('duplicates.merge.nothingMoves')}</strong>
        {' · '}
        {t('duplicates.merge.archived', { count: preview.archived })}
      </p>
    </>
  )
}

/**
 * A confirmation dialog shown before a duplicate group is resolved. It previews
 * what the merge will move onto the keeper and how many copies will be archived,
 * then asks the user to confirm. It is rendered whenever a preview is present.
 */
export function MergeConfirmModal({ preview, busy, onConfirm, onCancel }: MergeConfirmModalProps) {
  const { t } = useTranslation()
  return (
    <Modal show={preview !== null} onHide={onCancel} centered>
      <Modal.Header closeButton>
        <Modal.Title>{t('duplicates.merge.title')}</Modal.Title>
      </Modal.Header>
      <Modal.Body>{preview !== null && <MergePreviewBody preview={preview} />}</Modal.Body>
      <Modal.Footer>
        <Button variant="outline-secondary" onClick={onCancel} disabled={busy}>
          {t('duplicates.merge.cancel')}
        </Button>
        <Button variant="primary" onClick={onConfirm} disabled={busy}>
          {busy ? <Spinner animation="border" size="sm" /> : t('duplicates.merge.confirm')}
        </Button>
      </Modal.Footer>
    </Modal>
  )
}

import { type ReactNode, useRef } from 'react'
import Button from 'react-bootstrap/Button'
import Modal from 'react-bootstrap/Modal'
import { useTranslation } from 'react-i18next'

/** Props for {@link ConfirmModal}. */
export interface ConfirmModalProps {
  /** Whether the dialog is visible. */
  show: boolean
  /** The short question, e.g. "Delete album?". Named in the app's voice, never "OK". */
  title: ReactNode
  /** The consequence spelled out — what will happen, and to what. */
  children: ReactNode
  /**
   * The confirm button's label. Always the action itself ("Delete album", "Start
   * import"), never a bare "OK" — it reads the same in the dialog as on the control
   * that opened it, so the action keeps one name through the whole flow.
   */
  confirmLabel: string
  /** The cancel button's label; defaults to the shared "Cancel". */
  cancelLabel?: string
  /**
   * Destructive confirms (`danger`, the default) paint the confirm button red;
   * a merely-consequential one (`primary`) paints it in the app's accent.
   */
  variant?: 'danger' | 'primary'
  /** Disables both buttons while the confirmed action is in flight. */
  busy?: boolean
  /** Runs the action. The caller closes the dialog (clears its pending state). */
  onConfirm: () => void
  /** Dismisses the dialog without acting — also fired by Escape, the ✕ and the backdrop. */
  onCancel: () => void
}

/**
 * The app's one confirmation dialog, modelled on the Trash page's styled Modal so
 * a single pattern replaces every native `window.confirm`. It states what will
 * happen in the app's voice and localised copy, and its confirm button carries the
 * action rather than a generic label.
 *
 * Focus lands on Cancel when the dialog opens, so a stray Enter cancels rather than
 * firing a destructive action; Escape, the ✕ and the backdrop all cancel; and
 * react-bootstrap restores focus to the control that opened it on close.
 */
export function ConfirmModal({
  show,
  title,
  children,
  confirmLabel,
  cancelLabel,
  variant = 'danger',
  busy = false,
  onConfirm,
  onCancel,
}: ConfirmModalProps) {
  const { t } = useTranslation()
  const cancelRef = useRef<HTMLButtonElement>(null)

  return (
    <Modal
      show={show}
      onHide={onCancel}
      centered
      backdrop={busy ? 'static' : true}
      onEntered={() => {
        // Move focus to the safe action so a stray Enter cancels, never confirms.
        cancelRef.current?.focus()
      }}
    >
      <Modal.Header closeButton={!busy}>
        <Modal.Title>{title}</Modal.Title>
      </Modal.Header>
      <Modal.Body>{children}</Modal.Body>
      <Modal.Footer>
        <Button ref={cancelRef} variant="secondary" onClick={onCancel} disabled={busy}>
          {cancelLabel ?? t('confirmModal.cancel')}
        </Button>
        <Button variant={variant} onClick={onConfirm} disabled={busy}>
          {confirmLabel}
        </Button>
      </Modal.Footer>
    </Modal>
  )
}

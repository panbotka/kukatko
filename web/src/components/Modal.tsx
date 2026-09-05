import BsModal, { type ModalProps } from 'react-bootstrap/Modal'
import type { ModalHeaderProps } from 'react-bootstrap/ModalHeader'
import { useTranslation } from 'react-i18next'

/**
 * The modal header, identical to react-bootstrap's except that its ✕ takes an
 * accessible name from the catalogue. The library's own default is the literal
 * string `Close`, which a Czech screen-reader user would hear in English on every
 * dialog; a caller wanting something more specific than "Zavřít" still passes
 * `closeLabel` itself.
 */
function ModalHeader({ closeLabel, ...props }: ModalHeaderProps) {
  const { t } = useTranslation()

  return <BsModal.Header closeLabel={closeLabel ?? t('dialog.close')} {...props} />
}

/**
 * The app's modal: react-bootstrap's, with the header swapped for the one above,
 * so every dialog — those written today and those written next — names its close
 * button in the interface language instead of repeating the prop at each call
 * site. Nothing else changes: `Modal.Title`, `Modal.Body` and `Modal.Footer` are
 * the library's own components, and the markup is byte for byte the same.
 *
 * Import this instead of `react-bootstrap/Modal`; ESLint enforces it.
 */
function Modal(props: ModalProps) {
  return <BsModal {...props} />
}

// The dialog itself is passed straight through; only `Header` differs. Attaching
// the parts here rather than mutating react-bootstrap's own export keeps a direct
// `react-bootstrap/Modal` import (there is none left) behaving as the library
// documents it. React 19 hands `ref` down as an ordinary prop, so a caller that
// needs one is unaffected.
Modal.Header = ModalHeader
Modal.Title = BsModal.Title
Modal.Body = BsModal.Body
Modal.Footer = BsModal.Footer
Modal.Dialog = BsModal.Dialog

export default Modal

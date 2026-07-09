import { useState } from 'react'
import Button from 'react-bootstrap/Button'
import Modal from 'react-bootstrap/Modal'
import Table from 'react-bootstrap/Table'
import { useTranslation } from 'react-i18next'

import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts'
import { HELP_SHORTCUT_KEY, SHORTCUT_GROUPS } from '../lib/shortcuts'

/** A keyboard-cap glyph for the trigger button. */
function KeyboardIcon() {
  return (
    <svg
      width="20"
      height="20"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      className="d-block"
    >
      <rect x="2" y="6" width="20" height="12" rx="2" />
      <path d="M6 10h.01M10 10h.01M14 10h.01M18 10h.01M6 14h.01M18 14h.01M9 14h6" />
    </svg>
  )
}

/**
 * The discoverable keyboard-shortcuts help: a small keyboard-icon button in the
 * navbar and a modal listing every shortcut grouped by context (Grid / Detail).
 * The overlay also opens with `?` (Shift+/) from anywhere and is dismissed with
 * Escape (react-bootstrap Modal) or its close button. Self-contained so the
 * layout only needs to render it; the `?` handler is suppressed while typing or a
 * form modal is open (see {@link useKeyboardShortcuts}).
 */
export function KeyboardShortcutsHelp() {
  const { t } = useTranslation()
  const [show, setShow] = useState(false)

  useKeyboardShortcuts({
    [HELP_SHORTCUT_KEY]: () => {
      setShow(true)
    },
  })

  const close = () => {
    setShow(false)
  }

  return (
    <>
      <Button
        variant="outline-secondary"
        size="sm"
        className="d-inline-flex align-items-center kukatko-tap-target justify-content-center"
        aria-label={t('shortcuts.open')}
        title={t('shortcuts.open')}
        onClick={() => {
          setShow(true)
        }}
      >
        <KeyboardIcon />
      </Button>

      <Modal show={show} onHide={close} centered scrollable aria-labelledby="shortcuts-title">
        <Modal.Header closeButton closeLabel={t('shortcuts.close')}>
          <Modal.Title id="shortcuts-title" className="h5">
            {t('shortcuts.title')}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {SHORTCUT_GROUPS.map((group) => (
            <section key={group.titleKey} className="mb-3">
              <h3 className="kk-section-title text-secondary">{t(group.titleKey)}</h3>
              <Table size="sm" borderless className="mb-0 align-middle">
                <tbody>
                  {group.entries.map((entry) => (
                    <tr key={entry.descriptionKey}>
                      <td className="text-nowrap pe-3">
                        {entry.keys.map((key) => (
                          <kbd key={key} className="me-1">
                            {key}
                          </kbd>
                        ))}
                      </td>
                      <td className="w-100">{t(entry.descriptionKey)}</td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            </section>
          ))}
          <p className="text-secondary small mb-0">{t('shortcuts.helpHint')}</p>
        </Modal.Body>
      </Modal>
    </>
  )
}

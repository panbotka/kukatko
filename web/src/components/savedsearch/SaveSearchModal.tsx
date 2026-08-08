import { type SyntheticEvent, useEffect, useState } from 'react'
import Form from 'react-bootstrap/Form'
import Modal from 'react-bootstrap/Modal'
import { useTranslation } from 'react-i18next'

import {
  createSavedSearch,
  type SavedSearch,
  type SavedSearchParams,
  updateSavedSearch,
} from '../../services/savedSearches'
import { ReasonedButton } from '../ReasonedButton'

/** Props for {@link SaveSearchModal}. */
export interface SaveSearchModalProps {
  /**
   * The saved search being renamed; omit (or pass `null`) to create a new one.
   * In create mode {@link SaveSearchModalProps.params} supplies the view state to
   * store; in rename mode only the name changes and the stored params are kept.
   */
  search?: SavedSearch | null
  /** The current view-state object to store when creating a new saved search. */
  params?: SavedSearchParams
  /** Whether the modal is visible. */
  show: boolean
  /** Dismisses the modal without saving. */
  onHide: () => void
  /** Called with the created/renamed saved search after a successful save. */
  onSaved: (search: SavedSearch) => void
}

/**
 * A small modal form for naming a saved search. In create mode it captures a name
 * and persists it alongside the current view params; in rename mode it rewrites
 * just the name, preserving the stored params. Validation and save errors are
 * surfaced inline.
 */
export function SaveSearchModal({ search, params, show, onHide, onSaved }: SaveSearchModalProps) {
  const { t } = useTranslation()
  const renaming = search != null
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(false)

  // Reset the form whenever the modal opens so a reused modal never shows stale
  // input from a previous save.
  useEffect(() => {
    if (show) {
      setName(search?.name ?? '')
      setError(false)
    }
  }, [show, search])

  async function save(event: SyntheticEvent) {
    event.preventDefault()
    const trimmed = name.trim()
    if (trimmed === '') {
      setError(true)
      return
    }
    setBusy(true)
    setError(false)
    try {
      const saved = renaming
        ? await updateSavedSearch(search.uid, { name: trimmed })
        : await createSavedSearch(trimmed, params ?? {})
      onSaved(saved)
    } catch {
      setError(true)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal show={show} onHide={onHide} centered fullscreen="sm-down">
      <Form
        onSubmit={(event) => {
          void save(event)
        }}
      >
        <Modal.Header closeButton>
          <Modal.Title>
            {renaming ? t('savedSearches.edit.titleEdit') : t('savedSearches.edit.titleNew')}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {error && <p className="text-danger small">{t('savedSearches.edit.error')}</p>}
          <Form.Group controlId="saved-search-name">
            <Form.Label>{t('savedSearches.edit.name')}</Form.Label>
            <Form.Control
              type="text"
              value={name}
              autoFocus
              disabled={busy}
              onChange={(event) => {
                setName(event.target.value)
              }}
            />
          </Form.Group>
        </Modal.Body>
        <Modal.Footer>
          {/* Both footer buttons go off together while the save is in flight, and
              both say so: two greyed buttons and no words is the state a reader
              takes for a broken dialog. */}
          <ReasonedButton
            variant="secondary"
            onClick={onHide}
            disabledReason={busy ? t('savedSearches.edit.busy') : undefined}
          >
            {t('savedSearches.edit.cancel')}
          </ReasonedButton>
          <ReasonedButton
            type="submit"
            variant="primary"
            disabledReason={busy ? t('savedSearches.edit.busy') : undefined}
          >
            {t('savedSearches.edit.save')}
          </ReasonedButton>
        </Modal.Footer>
      </Form>
    </Modal>
  )
}

import { useEffect, useState } from 'react'
import { Alert, Button, Modal, Spinner } from 'react-bootstrap'
import { useTranslation } from 'react-i18next'

import { type DirectoryUser, listDirectory } from '../../services/directory'
import { PersonAvatar } from '../PersonAvatar'

/** Fetch lifecycle of the directory the picker chooses from. */
type DirectoryState =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; users: DirectoryUser[] }

/** Props for {@link PersonPicker}. */
export interface PersonPickerProps {
  show: boolean
  busy: boolean
  /** The people already on the task or already chosen, left out of the list. */
  already: readonly string[]
  /** Called with the chosen person — the uid to send, the name to show. */
  onPick: (user: DirectoryUser) => void
  onClose: () => void
}

/**
 * Picks somebody to ask. Shared by the task page (asking one more person) and
 * the create dialog (handing a task over as it is opened), so the two never
 * drift into two lists of the same dozen names. The directory is small — a family archive has a dozen
 * accounts, not a company's thousands — so it is a list to read rather than a
 * field to search, and it is fetched when the dialog opens rather than with the
 * page that hosts it.
 */
export function PersonPicker({ show, busy, already, onPick, onClose }: PersonPickerProps) {
  const { t } = useTranslation()
  const [state, setState] = useState<DirectoryState>({ status: 'loading' })

  useEffect(() => {
    if (!show) {
      return
    }
    const controller = new AbortController()
    setState({ status: 'loading' })
    listDirectory(controller.signal)
      .then((users) => {
        setState({ status: 'ready', users })
      })
      .catch((err: unknown) => {
        if (!(err instanceof DOMException && err.name === 'AbortError')) {
          setState({ status: 'error' })
        }
      })
    return () => {
      controller.abort()
    }
  }, [show])

  const choices =
    state.status === 'ready' ? state.users.filter((u) => !already.includes(u.uid)) : []

  return (
    <Modal show={show} onHide={onClose} centered aria-labelledby="person-picker-title">
      <Modal.Header closeButton>
        <Modal.Title as="h2" className="h5" id="person-picker-title">
          {t('taskDetail.people.pick')}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {state.status === 'loading' && (
          <div className="d-flex justify-content-center py-3">
            <Spinner animation="border" role="status" size="sm">
              <span className="visually-hidden">{t('taskDetail.people.loading')}</span>
            </Spinner>
          </div>
        )}
        {state.status === 'error' && (
          <Alert variant="danger" className="py-2 mb-0">
            {t('taskDetail.people.loadFailed')}
          </Alert>
        )}
        {state.status === 'ready' && choices.length === 0 && (
          <p className="text-body-secondary mb-0">{t('taskDetail.people.everybody')}</p>
        )}
        {/* A flex column, not `d-grid` — see the note in NewTaskModal: a grid
            column takes the width of its widest item, so one long name would
            push the rows out of the dialog. */}
        {choices.length > 0 && (
          <div className="d-flex flex-column gap-1">
            {choices.map((user) => (
              <Button
                key={user.uid}
                variant="outline-secondary"
                className="d-flex align-items-center gap-2 text-start"
                disabled={busy}
                onClick={() => {
                  onPick(user)
                }}
              >
                <PersonAvatar name={user.name} userUid={user.uid} />
                <span className="kk-min-w-0 text-truncate">{user.name}</span>
              </Button>
            ))}
          </div>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" onClick={onClose} disabled={busy}>
          {t('confirmModal.cancel')}
        </Button>
      </Modal.Footer>
    </Modal>
  )
}

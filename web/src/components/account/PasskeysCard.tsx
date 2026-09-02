import { type SyntheticEvent, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Form from 'react-bootstrap/Form'
import InputGroup from 'react-bootstrap/InputGroup'
import ListGroup from 'react-bootstrap/ListGroup'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { useCapabilities } from '../../capabilities/CapabilitiesContext'
import { useReloadKey } from '../../hooks/useReloadKey'
import { formatDateTimeMinutes } from '../../lib/format'
import {
  deletePasskey,
  fetchPasskeys,
  isPasskeySupported,
  type Passkey,
  PasskeyError,
  type PasskeyErrorReason,
  PASSKEY_NAME_MAX_LENGTH,
  registerPasskey,
} from '../../services/passkeys'
import { ConfirmModal } from '../ConfirmModal'
import { EmptyState } from '../EmptyState'
import { ErrorState } from '../ErrorState'
import { Icon } from '../Icon'

/**
 * Fetch lifecycle of the passkey list. `unavailable` is not an error: it is the
 * answer "this instance has no relying party configured", which the card renders
 * as one explanatory sentence rather than as a broken load. The account page
 * hides the card entirely in that case, so this only catches an instance whose
 * configuration changed under a page that was already open.
 */
type ListState =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'unavailable' }
  | { status: 'ready'; passkeys: Passkey[] }

/** The i18n key of the message a failed registration shows. */
type AddErrorKey =
  | 'account.passkeys.addError.unsupported'
  | 'account.passkeys.addError.unavailable'
  | 'account.passkeys.addError.cancelled'
  | 'account.passkeys.addError.duplicate'
  | 'account.passkeys.addError.refused'
  | 'account.passkeys.addError.offline'
  | 'account.passkeys.addError.generic'

/**
 * The message each way adding a passkey can fail.
 *
 * Two of the reasons cannot arise here and fall back to the generic sentence: a
 * `pendingApproval` account cannot be signed in to add anything, and the
 * registration ceremony is not on the sign-in rate limiter's budget.
 */
const ADD_ERROR_KEYS = {
  unsupported: 'account.passkeys.addError.unsupported',
  unavailable: 'account.passkeys.addError.unavailable',
  cancelled: 'account.passkeys.addError.cancelled',
  duplicate: 'account.passkeys.addError.duplicate',
  refused: 'account.passkeys.addError.refused',
  offline: 'account.passkeys.addError.offline',
  pendingApproval: 'account.passkeys.addError.generic',
  rateLimited: 'account.passkeys.addError.generic',
  generic: 'account.passkeys.addError.generic',
} satisfies Record<PasskeyErrorReason, AddErrorKey>

/** Maps a failed registration to the i18n key of the message to show. */
function addErrorKeyFor(error: unknown): AddErrorKey {
  if (error instanceof PasskeyError) {
    return ADD_ERROR_KEYS[error.reason]
  }
  return 'account.passkeys.addError.generic'
}

/**
 * One passkey in the list: what it was called, when it was added and when it was
 * last used, plus the way to remove it.
 *
 * An unnamed key gets a stand-in label rather than an empty line — the backend
 * accepts an empty name, and a row with no words in it cannot be told from the
 * one below it, nor confirmed away in a dialogue that names what it removes.
 */
function PasskeyRow({
  passkey,
  onRemove,
}: {
  passkey: Passkey
  onRemove: (passkey: Passkey) => void
}) {
  const { t, i18n } = useTranslation()
  const name = passkeyLabel(passkey, t('account.passkeys.unnamed'))

  return (
    <ListGroup.Item className="d-flex align-items-start justify-content-between gap-2">
      <div className="kk-min-w-0">
        <div className="fw-semibold text-break">{name}</div>
        <div className="text-secondary small">
          {t('account.passkeys.createdAt', {
            date: formatDateTimeMinutes(passkey.created_at, i18n.language),
          })}
          {' · '}
          {passkey.last_used_at === undefined
            ? t('account.passkeys.neverUsed')
            : t('account.passkeys.lastUsed', {
                date: formatDateTimeMinutes(passkey.last_used_at, i18n.language),
              })}
        </div>
      </div>
      {/* The glyph keeps its word above `sm`; below it the row is a device name
          plus one Czech-worded button, which does not fit a phone. */}
      <Button
        variant="outline-danger"
        size="sm"
        className="d-inline-flex align-items-center gap-2 flex-shrink-0 kukatko-tap-target-touch"
        aria-label={t('account.passkeys.removeNamed', { name })}
        title={t('account.passkeys.remove')}
        onClick={() => {
          onRemove(passkey)
        }}
      >
        <Icon name="trash" />
        <span className="d-none d-sm-inline">{t('account.passkeys.remove')}</span>
      </Button>
    </ListGroup.Item>
  )
}

/** The name to print for a passkey, falling back for one saved without a name. */
function passkeyLabel(passkey: Passkey, fallback: string): string {
  return passkey.name.trim() === '' ? fallback : passkey.name
}

/**
 * The "Passkeys" section of the account page: the WebAuthn credentials this
 * account signs in with instead of typing its password, listed, added and
 * removed here.
 *
 * It sits beside the password and the API tokens because it is the same kind of
 * thing — a credential of this one user — but it is the only one of the three
 * whose secret half never reaches the server at all: the key stays in the phone
 * or the laptop, and what travels is a signature over a challenge.
 *
 * The section renders only where passkeys can actually work. An instance with no
 * relying party configured (`capabilities.passkeys`) gets no card at all rather
 * than a form that answers every press with "not available", and a browser
 * without WebAuthn gets the list plus one sentence saying why it cannot add
 * one — the keys it already has are still worth showing, and still worth being
 * able to remove.
 *
 * Removing the last passkey is deliberately not refused: the password never
 * stopped working, and refusing would strand somebody whose only authenticator
 * was lost.
 */
export function PasskeysCard() {
  const { t } = useTranslation()
  const capabilities = useCapabilities()
  const [state, setState] = useState<ListState>({ status: 'loading' })
  const [reloadKey, reload] = useReloadKey()
  const [name, setName] = useState('')
  const [adding, setAdding] = useState(false)
  const [addError, setAddError] = useState<AddErrorKey | null>(null)
  const [added, setAdded] = useState<string | null>(null)
  const [pendingRemove, setPendingRemove] = useState<Passkey | null>(null)
  const [removeError, setRemoveError] = useState(false)
  const offered = capabilities.known && capabilities.passkeys
  const supported = isPasskeySupported()

  useEffect(() => {
    if (!offered) {
      return undefined
    }
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchPasskeys(controller.signal)
      .then((passkeys) => {
        setState({ status: 'ready', passkeys })
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) {
          return
        }
        setState({
          status:
            error instanceof PasskeyError && error.reason === 'unavailable'
              ? 'unavailable'
              : 'error',
        })
      })
    return () => {
      controller.abort()
    }
  }, [offered, reloadKey])

  if (!offered) {
    return null
  }

  async function handleAdd(event: SyntheticEvent) {
    event.preventDefault()
    setAdding(true)
    setAddError(null)
    setAdded(null)
    try {
      const passkey = await registerPasskey(name.trim())
      setAdded(passkeyLabel(passkey, t('account.passkeys.unnamed')))
      setName('')
      // The new key simply shows up among the others, as any other client's would.
      reload()
    } catch (error: unknown) {
      setAddError(addErrorKeyFor(error))
    } finally {
      setAdding(false)
    }
  }

  async function remove(passkey: Passkey) {
    setRemoveError(false)
    setAdded(null)
    // Optimistically drop the row, remembering the prior list to restore on error.
    let previous: Passkey[] = []
    setState((prev) => {
      if (prev.status !== 'ready') {
        return prev
      }
      previous = prev.passkeys
      return { status: 'ready', passkeys: prev.passkeys.filter((item) => item.id !== passkey.id) }
    })
    try {
      await deletePasskey(passkey.id)
    } catch {
      setRemoveError(true)
      setState({ status: 'ready', passkeys: previous })
    }
  }

  return (
    <Card text="light" className="mb-4">
      <Card.Body>
        <Card.Title as="h2" className="kk-section-title mb-3">
          {t('account.passkeys.title')}
        </Card.Title>
        <p className="text-secondary">{t('account.passkeys.intro')}</p>

        {added !== null && (
          <Alert variant="success" role="alert">
            {t('account.passkeys.added', { name: added })}
          </Alert>
        )}

        {removeError && (
          <Alert variant="danger" role="alert">
            {t('account.passkeys.removeError')}
          </Alert>
        )}

        {state.status === 'loading' && (
          <div className="d-flex justify-content-center py-4">
            <Spinner animation="border" role="status">
              <span className="visually-hidden">{t('account.passkeys.loading')}</span>
            </Spinner>
          </div>
        )}

        {state.status === 'error' && (
          <ErrorState title={t('account.passkeys.loadError')} onRetry={reload} size="sm" />
        )}

        {state.status === 'unavailable' && (
          <Alert variant="secondary" role="alert" className="mb-0">
            <Icon name="lock-fill" className="me-2" />
            {t('account.passkeys.unavailable')}
          </Alert>
        )}

        {state.status === 'ready' && state.passkeys.length === 0 && (
          <EmptyState
            size="sm"
            icon={<Icon name="shield-lock" />}
            title={t('account.passkeys.empty.title')}
            hint={t('account.passkeys.empty.hint')}
          />
        )}

        {state.status === 'ready' && state.passkeys.length > 0 && (
          <ListGroup>
            {state.passkeys.map((passkey) => (
              <PasskeyRow key={passkey.id} passkey={passkey} onRemove={setPendingRemove} />
            ))}
          </ListGroup>
        )}

        {/* A browser without WebAuthn keeps the list — the keys are still the
            account's, and still worth removing — but gets no form it could not
            complete. */}
        {state.status !== 'unavailable' && !supported && (
          <Alert variant="secondary" role="status" className="mt-3 mb-0">
            <Icon name="info-circle" className="me-2" />
            {t('account.passkeys.unsupported')}
          </Alert>
        )}

        {state.status !== 'unavailable' && supported && (
          <Form
            className="mt-3"
            onSubmit={(event) => {
              void handleAdd(event)
            }}
          >
            {addError !== null && (
              <Alert variant="danger" role="alert">
                {t(addError)}
              </Alert>
            )}
            <Form.Group controlId="passkey-name">
              <Form.Label>{t('account.passkeys.nameLabel')}</Form.Label>
              <InputGroup>
                <Form.Control
                  value={name}
                  maxLength={PASSKEY_NAME_MAX_LENGTH}
                  placeholder={t('account.passkeys.namePlaceholder')}
                  disabled={adding}
                  onChange={(event) => {
                    setName(event.target.value)
                  }}
                />
                <Button
                  type="submit"
                  variant="primary"
                  disabled={adding}
                  className="d-inline-flex align-items-center gap-2"
                >
                  {adding ? (
                    <Spinner animation="border" size="sm" role="status" aria-hidden="true" />
                  ) : (
                    <Icon name="plus-lg" />
                  )}
                  {t('account.passkeys.add')}
                </Button>
              </InputGroup>
              <Form.Text className="text-secondary">{t('account.passkeys.nameHint')}</Form.Text>
            </Form.Group>
          </Form>
        )}
      </Card.Body>

      <ConfirmModal
        show={pendingRemove !== null}
        title={t('account.passkeys.confirmTitle')}
        confirmLabel={t('account.passkeys.confirmAction')}
        onCancel={() => {
          setPendingRemove(null)
        }}
        onConfirm={() => {
          const passkey = pendingRemove
          setPendingRemove(null)
          if (passkey !== null) {
            void remove(passkey)
          }
        }}
      >
        {pendingRemove !== null &&
          t('account.passkeys.confirmBody', {
            name: passkeyLabel(pendingRemove, t('account.passkeys.unnamed')),
          })}
      </ConfirmModal>
    </Card>
  )
}

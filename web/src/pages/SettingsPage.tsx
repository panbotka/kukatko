import { type SyntheticEvent, useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Col from 'react-bootstrap/Col'
import Form from 'react-bootstrap/Form'
import InputGroup from 'react-bootstrap/InputGroup'
import Row from 'react-bootstrap/Row'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { ConfirmModal } from '../components/ConfirmModal'
import { ErrorState } from '../components/ErrorState'
import { Icon } from '../components/Icon'
import { Markdown } from '../components/Markdown'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useLeaveGuard } from '../hooks/useLeaveGuard'
import { formatDateTime } from '../lib/format'
import { ApiError } from '../services/auth'
import {
  fetchInstanceSettings,
  type InstanceSettings,
  updateInstanceSettings,
} from '../services/settings'

/** The three values the form edits, i.e. everything `PUT /settings` replaces. */
interface Draft {
  registrationEnabled: boolean
  registrationSecret: string
  welcomeMarkdown: string
}

/** Fetch lifecycle of the settings record behind the form. */
type LoadState =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; saved: InstanceSettings }

/** What the last save attempt did, so the page can report it. */
type SaveState =
  | { status: 'idle' }
  | { status: 'saving' }
  | { status: 'saved' }
  | { status: 'failed'; message: string }

/** Reduces a fetched record to the editable draft the form holds. */
function toDraft(settings: InstanceSettings): Draft {
  return {
    registrationEnabled: settings.registration_enabled,
    registrationSecret: settings.registration_secret,
    welcomeMarkdown: settings.welcome_markdown,
  }
}

/**
 * Whether the draft differs from what is stored. The secret is compared
 * trimmed, because the backend stores it trimmed: typing a trailing space and
 * deleting it again must not leave the page claiming unsaved work forever.
 */
function isDirty(draft: Draft, saved: InstanceSettings): boolean {
  return (
    draft.registrationEnabled !== saved.registration_enabled ||
    draft.registrationSecret.trim() !== saved.registration_secret ||
    draft.welcomeMarkdown !== saved.welcome_markdown
  )
}

/**
 * The administrator's screen for the three instance-wide settings
 * (`GET`/`PUT /api/v1/settings`): whether self-service registration is open,
 * the shared secret it asks a newcomer for, and the Markdown greeting shown to
 * a person the first time they sign in.
 *
 * It is its own route rather than another section of {@link UsersPage} — those
 * are accounts, these are the instance — and admin-only, because the record
 * carries the registration secret in readable form.
 *
 * Three decisions worth stating:
 *
 * - **The secret is shown as text.** An administrator's whole job with it is to
 *   read it back and tell people what it is, so a write-only password field
 *   would make the setting useless; the eye button masks it for the moment
 *   somebody is looking over the shoulder.
 * - **Enabling registration with a blank secret is refused here**, in the same
 *   words the API refuses it (`settings.ErrSecretRequired`) — an open door with
 *   no lock is never what was meant, and saying so at the switch beats saying it
 *   after a round trip.
 * - **Nothing is written until Save.** The draft lives in this component, a
 *   leave with unsaved changes is held back by {@link useLeaveGuard}, and the
 *   welcome preview renders through the shared {@link Markdown} component — the
 *   one the welcome itself will render with — so the two cannot drift apart.
 */
export function SettingsPage() {
  const { t, i18n } = useTranslation()
  useDocumentTitle(t('settings.title'))

  const [load, setLoad] = useState<LoadState>({ status: 'loading' })
  const [draft, setDraft] = useState<Draft>({
    registrationEnabled: false,
    registrationSecret: '',
    welcomeMarkdown: '',
  })
  const [save, setSave] = useState<SaveState>({ status: 'idle' })
  const [secretRevealed, setSecretRevealed] = useState(true)
  // Set when registration is asked to open while the secret is blank — the one
  // combination both this form and the backend refuse.
  const [secretRequired, setSecretRequired] = useState(false)

  const reload = useCallback(async (signal?: AbortSignal) => {
    setLoad({ status: 'loading' })
    const settings = await fetchInstanceSettings(signal)
    if (signal?.aborted === true) {
      return
    }
    setLoad({ status: 'ready', saved: settings })
    setDraft(toDraft(settings))
    setSave({ status: 'idle' })
    setSecretRequired(false)
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    reload(controller.signal).catch((error: unknown) => {
      if (error instanceof DOMException && error.name === 'AbortError') {
        return
      }
      setLoad({ status: 'error' })
    })
    return () => {
      controller.abort()
    }
  }, [reload])

  const dirty = load.status === 'ready' && isDirty(draft, load.saved)
  const guard = useLeaveGuard(dirty)

  function toggleRegistration(enabled: boolean) {
    if (enabled && draft.registrationSecret.trim() === '') {
      // Refused in the browser, in the API's own words: the flag and the secret
      // are saved together, so the combination never briefly exists.
      setSecretRequired(true)
      return
    }
    setSecretRequired(false)
    setDraft((prev) => ({ ...prev, registrationEnabled: enabled }))
  }

  function changeSecret(value: string) {
    setSecretRequired(false)
    setDraft((prev) => ({ ...prev, registrationSecret: value }))
  }

  async function handleSubmit(event: SyntheticEvent) {
    event.preventDefault()
    if (load.status !== 'ready' || save.status === 'saving') {
      return
    }
    if (draft.registrationEnabled && draft.registrationSecret.trim() === '') {
      // Reachable by emptying the field after the switch was already on.
      setSecretRequired(true)
      return
    }
    setSave({ status: 'saving' })
    try {
      const saved = await updateInstanceSettings({
        registration_enabled: draft.registrationEnabled,
        registration_secret: draft.registrationSecret,
        welcome_markdown: draft.welcomeMarkdown,
      })
      setLoad({ status: 'ready', saved })
      setDraft(toDraft(saved))
      setSave({ status: 'saved' })
    } catch (error: unknown) {
      // The server's own wording, not a generic apology: a rejection it made on
      // grounds this form does not know about is exactly what needs repeating.
      const message = error instanceof ApiError ? error.message : t('settings.saveFailed')
      setSave({ status: 'failed', message })
    }
  }

  if (load.status === 'loading') {
    return (
      <div className="d-flex justify-content-center py-5">
        <Spinner animation="border" role="status">
          <span className="visually-hidden">{t('settings.loading')}</span>
        </Spinner>
      </div>
    )
  }

  if (load.status === 'error') {
    return (
      <ErrorState
        title={t('settings.loadFailed')}
        onRetry={() => {
          void reload()
        }}
      />
    )
  }

  const savedRecord = load.saved

  return (
    <>
      <h1 className="kk-page-title mb-3">{t('settings.title')}</h1>
      <p className="text-secondary">{t('settings.intro')}</p>

      <Form noValidate onSubmit={(event) => void handleSubmit(event)}>
        <Card className="mb-4">
          <Card.Body>
            <h2 className="kk-section-title mb-3">{t('settings.registration.title')}</h2>
            <Form.Check
              type="switch"
              id="settings-registration-enabled"
              className="mb-2"
              label={t('settings.registration.enabledLabel')}
              checked={draft.registrationEnabled}
              onChange={(event) => {
                toggleRegistration(event.currentTarget.checked)
              }}
            />
            <Form.Text className="d-block mb-3">{t('settings.registration.enabledHelp')}</Form.Text>

            <Form.Group controlId="settings-registration-secret">
              <Form.Label>{t('settings.registration.secretLabel')}</Form.Label>
              <InputGroup>
                <Form.Control
                  type={secretRevealed ? 'text' : 'password'}
                  value={draft.registrationSecret}
                  autoComplete="off"
                  isInvalid={secretRequired}
                  onChange={(event) => {
                    changeSecret(event.currentTarget.value)
                  }}
                />
                <Button
                  variant="outline-secondary"
                  onClick={() => {
                    setSecretRevealed((prev) => !prev)
                  }}
                >
                  <Icon name={secretRevealed ? 'eye-slash' : 'eye'} className="me-1" />
                  {secretRevealed
                    ? t('settings.registration.hide')
                    : t('settings.registration.reveal')}
                </Button>
              </InputGroup>
              <Form.Text className="d-block">{t('settings.registration.secretHelp')}</Form.Text>
            </Form.Group>

            {secretRequired && (
              <Alert variant="danger" className="mt-3 mb-0">
                {t('settings.registration.secretRequired')}
              </Alert>
            )}
          </Card.Body>
        </Card>

        <Card className="mb-4">
          <Card.Body>
            <h2 className="kk-section-title mb-1">{t('settings.welcome.title')}</h2>
            <p className="text-secondary">{t('settings.welcome.help')}</p>
            {/* Side by side from `md` up, stacked (editor first) on a phone. */}
            <Row className="g-3">
              <Col md={6}>
                <Form.Group controlId="settings-welcome-markdown">
                  <Form.Label>{t('settings.welcome.editorLabel')}</Form.Label>
                  <Form.Control
                    as="textarea"
                    rows={14}
                    className="font-monospace"
                    value={draft.welcomeMarkdown}
                    onChange={(event) => {
                      // Read before the updater runs: React nulls `currentTarget`
                      // once the event handler has returned.
                      const value = event.currentTarget.value
                      setDraft((prev) => ({ ...prev, welcomeMarkdown: value }))
                    }}
                  />
                </Form.Group>
              </Col>
              <Col md={6}>
                <Form.Label as="div" id="settings-welcome-preview-label">
                  {t('settings.welcome.previewLabel')}
                </Form.Label>
                <div
                  className="border rounded p-3 h-100"
                  role="region"
                  aria-labelledby="settings-welcome-preview-label"
                >
                  {draft.welcomeMarkdown.trim() === '' ? (
                    <p className="text-secondary mb-0">{t('settings.welcome.previewEmpty')}</p>
                  ) : (
                    <Markdown>{draft.welcomeMarkdown}</Markdown>
                  )}
                </div>
              </Col>
            </Row>
          </Card.Body>
        </Card>

        {save.status === 'saved' && <Alert variant="success">{t('settings.saved')}</Alert>}
        {save.status === 'failed' && <Alert variant="danger">{save.message}</Alert>}

        <div className="d-flex flex-wrap align-items-center gap-2">
          <Button type="submit" variant="primary" disabled={!dirty || save.status === 'saving'}>
            {save.status === 'saving' && (
              <Spinner as="span" animation="border" size="sm" className="me-2" />
            )}
            {t('settings.save')}
          </Button>
          <Button
            type="button"
            variant="outline-secondary"
            disabled={!dirty || save.status === 'saving'}
            onClick={() => {
              setDraft(toDraft(savedRecord))
              setSecretRequired(false)
              setSave({ status: 'idle' })
            }}
          >
            {t('settings.discard')}
          </Button>
          {dirty && <span className="text-warning">{t('settings.unsavedHint')}</span>}
          <span className="text-secondary ms-auto">
            {t('settings.lastSaved', {
              when: formatDateTime(savedRecord.updated_at, i18n.language),
            })}
          </span>
        </div>
      </Form>

      <ConfirmModal
        show={guard.asking}
        title={t('settings.leave.title')}
        confirmLabel={t('settings.leave.confirm')}
        cancelLabel={t('settings.leave.cancel')}
        onConfirm={guard.confirm}
        onCancel={guard.cancel}
      >
        {t('settings.leave.body')}
      </ConfirmModal>
    </>
  )
}

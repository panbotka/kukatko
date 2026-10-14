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

import { useAuth } from '../auth/AuthContext'
import { AnnouncementComposer } from '../components/AnnouncementComposer'
import { ConfirmModal } from '../components/ConfirmModal'
import { ErrorState } from '../components/ErrorState'
import { Icon } from '../components/Icon'
import { Markdown } from '../components/Markdown'
import { NarrowPage } from '../components/NarrowPage'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { announceFooterChanged } from '../hooks/useProjectFooter'
import { useLeaveGuard } from '../hooks/useLeaveGuard'
import { formatBytes, formatCount, formatDateTime } from '../lib/format'
import { generateRegistrationSecret } from '../lib/registrationSecret'
import { projectRegisterPath } from '../project/paths'
import { getCurrentProjectSlug } from '../services/api'
import { ApiError } from '../services/auth'
import {
  fetchProjectSettings,
  fetchProjectUsage,
  MAX_FOOTER_LENGTH,
  type ProjectSettings,
  type ProjectUsage,
  updateProjectSettings,
} from '../services/settings'

/** The values the form edits, i.e. everything `PUT /settings` replaces. */
interface Draft {
  registrationEnabled: boolean
  registrationSecret: string
  welcomeMarkdown: string
  footerText: string
}

/** Fetch lifecycle of the settings record behind the form. */
type LoadState =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; saved: ProjectSettings }

/** What the last save attempt did, so the page can report it. */
type SaveState =
  | { status: 'idle' }
  | { status: 'saving' }
  | { status: 'saved' }
  | { status: 'failed'; message: string }

/** Fetch lifecycle of the read-only usage block. */
type UsageState =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; usage: ProjectUsage }

/** Props of {@link UsageBar}. */
interface UsageBarProps {
  /** The summed size of the project's originals, in bytes. */
  used: number
  /** The project's quota (`max_total_bytes`), in bytes. */
  limit: number
}

/**
 * How full the project's storage is: a bar of the originals against the quota,
 * the two sizes and the percentage spelled out beside it, and a sentence once
 * the quota is reached — from then on every upload is refused. Colours come
 * from the palette (`.kk-usage-bar` in `styles/app.css`): the accent while
 * there is room, danger when full.
 */
function UsageBar({ used, limit }: UsageBarProps) {
  const { t, i18n } = useTranslation()
  const full = used >= limit
  const percent = limit > 0 ? Math.min(100, Math.round((used / limit) * 100)) : 100
  return (
    <div
      className={`kk-usage-bar mb-3${full ? ' kk-usage-bar--full' : ''}`}
      data-testid="settings-usage-bar"
    >
      {/* Bootstrap's own markup rather than react-bootstrap's ProgressBar, which
          puts aria-label on the track and leaves the progressbar unnamed. */}
      <div className="progress">
        <div
          className="progress-bar"
          role="progressbar"
          aria-label={t('settings.usage.barLabel')}
          aria-valuenow={percent}
          aria-valuemin={0}
          aria-valuemax={100}
          style={{ width: `${String(percent)}%` }}
        />
      </div>
      <p className="small mt-1 mb-0">
        {t('settings.usage.barText', {
          used: formatBytes(used, i18n.language),
          limit: formatBytes(limit, i18n.language),
          percent,
        })}
      </p>
      {full && (
        <p className="small fw-semibold mt-1 mb-0" role="status">
          {t('settings.usage.full')}
        </p>
      )}
    </div>
  )
}

/**
 * The read-only "usage and limits" block: what the project occupies — its
 * photos and the size of their originals — next to the two limits the system
 * administrator set on it, each "unlimited" when there is none, and — when a
 * quota is set — a {@link UsageBar} of how much of it is used. The project's
 * admin cannot change a limit (that is the system administrator's projects
 * page), and the block says so. It loads on its own, so a failure here never
 * keeps the settings form from working.
 */
function UsageCard() {
  const { t, i18n } = useTranslation()
  const [state, setState] = useState<UsageState>({ status: 'loading' })

  useEffect(() => {
    const controller = new AbortController()
    fetchProjectUsage(controller.signal)
      .then((usage) => {
        setState({ status: 'ready', usage })
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          setState({ status: 'error' })
        }
      })
    return () => {
      controller.abort()
    }
  }, [])

  const limit = (bytes: number | null) =>
    bytes === null ? t('settings.usage.unlimited') : formatBytes(bytes, i18n.language)

  return (
    <Card className="mb-4" data-testid="settings-usage">
      <Card.Body>
        <h2 className="kk-section-title mb-3">{t('settings.usage.title')}</h2>
        {state.status === 'loading' && (
          <Spinner animation="border" size="sm" role="status">
            <span className="visually-hidden">{t('settings.loading')}</span>
          </Spinner>
        )}
        {state.status === 'error' && (
          <p className="text-secondary mb-0">{t('settings.usage.loadFailed')}</p>
        )}
        {state.status === 'ready' && (
          <>
            {state.usage.max_total_bytes !== null && (
              <UsageBar used={state.usage.originals_bytes} limit={state.usage.max_total_bytes} />
            )}
            <dl className="row mb-2">
              <dt className="col-sm-6">{t('settings.usage.photos')}</dt>
              <dd className="col-sm-6">{formatCount(state.usage.photos, i18n.language)}</dd>
              <dt className="col-sm-6">{t('settings.usage.originals')}</dt>
              <dd className="col-sm-6">
                {formatBytes(state.usage.originals_bytes, i18n.language)}
              </dd>
              <dt className="col-sm-6">{t('settings.usage.maxFile')}</dt>
              <dd className="col-sm-6">{limit(state.usage.max_file_bytes)}</dd>
              <dt className="col-sm-6">{t('settings.usage.maxTotal')}</dt>
              <dd className="col-sm-6">{limit(state.usage.max_total_bytes)}</dd>
            </dl>
            <Form.Text className="d-block">{t('settings.usage.note')}</Form.Text>
          </>
        )}
      </Card.Body>
    </Card>
  )
}

/** Reduces a fetched record to the editable draft the form holds. */
function toDraft(settings: ProjectSettings): Draft {
  return {
    registrationEnabled: settings.registration_enabled,
    registrationSecret: settings.registration_secret,
    welcomeMarkdown: settings.welcome_markdown,
    footerText: settings.footer_text,
  }
}

/**
 * Whether the draft differs from what is stored. The secret and the footer line
 * are compared trimmed, because the backend stores them trimmed: typing a
 * trailing space and deleting it again must not leave the page claiming unsaved
 * work forever.
 */
function isDirty(draft: Draft, saved: ProjectSettings): boolean {
  return (
    draft.registrationEnabled !== saved.registration_enabled ||
    draft.registrationSecret.trim() !== saved.registration_secret ||
    draft.welcomeMarkdown !== saved.welcome_markdown ||
    draft.footerText.trim() !== saved.footer_text
  )
}

/**
 * The project admin's screen for the project's settings
 * (`GET`/`PUT /api/v1/p/<slug>/settings`): whether self-service registration
 * into the project is open, the shared secret its registration page
 * (`/p/<slug>/register`, linked here to pass on) asks a newcomer for, the
 * Markdown greeting shown to a member the first time they sign in, and the
 * plain-text footer line every page of the project ends with (saved, it
 * replaces the footer on screen at once). Below them a
 * read-only "usage and limits" block, and — for the system administrator, who
 * alone publishes it — the project's announcement banner.
 *
 * It is its own route rather than another section of {@link MembersPage} — those
 * are memberships, these are the project — and admin-only, because the record
 * carries the registration secret in readable form.
 *
 * Three decisions worth stating:
 *
 * - **The secret is shown as text.** An administrator's whole job with it is to
 *   read it back and tell people what it is, so a write-only password field
 *   would make the setting useless; the eye button masks it for the moment
 *   somebody is looking over the shoulder, and "generate new" drafts a fresh
 *   random word that, like everything else here, takes effect on Save.
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
  const { isMaintainer } = useAuth()
  const slug = getCurrentProjectSlug()
  const registerUrl = slug === null ? null : `${window.location.origin}${projectRegisterPath(slug)}`

  const [load, setLoad] = useState<LoadState>({ status: 'loading' })
  const [draft, setDraft] = useState<Draft>({
    registrationEnabled: false,
    registrationSecret: '',
    welcomeMarkdown: '',
    footerText: '',
  })
  const [save, setSave] = useState<SaveState>({ status: 'idle' })
  const [secretRevealed, setSecretRevealed] = useState(true)
  // Set when registration is asked to open while the secret is blank — the one
  // combination both this form and the backend refuse.
  const [secretRequired, setSecretRequired] = useState(false)

  const reload = useCallback(async (signal?: AbortSignal) => {
    setLoad({ status: 'loading' })
    const settings = await fetchProjectSettings(signal)
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
      const saved = await updateProjectSettings({
        registration_enabled: draft.registrationEnabled,
        registration_secret: draft.registrationSecret,
        welcome_markdown: draft.welcomeMarkdown,
        footer_text: draft.footerText,
      })
      setLoad({ status: 'ready', saved })
      setDraft(toDraft(saved))
      setSave({ status: 'saved' })
      // The footer under this very form is the project's: it changes with it.
      if (slug !== null) {
        announceFooterChanged(slug, saved.footer_text)
      }
    } catch (error: unknown) {
      // The server's own wording, not a generic apology: a rejection it made on
      // grounds this form does not know about is exactly what needs repeating.
      const message = error instanceof ApiError ? error.message : t('settings.saveFailed')
      setSave({ status: 'failed', message })
    }
  }

  if (load.status === 'loading') {
    return (
      <NarrowPage>
        <div className="d-flex justify-content-center py-5">
          <Spinner animation="border" role="status">
            <span className="visually-hidden">{t('settings.loading')}</span>
          </Spinner>
        </div>
      </NarrowPage>
    )
  }

  if (load.status === 'error') {
    return (
      <NarrowPage>
        <ErrorState
          title={t('settings.loadFailed')}
          onRetry={() => {
            void reload()
          }}
        />
      </NarrowPage>
    )
  }

  const savedRecord = load.saved

  return (
    <NarrowPage>
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
                <Button
                  variant="outline-secondary"
                  title={t('settings.registration.regenerateHelp')}
                  onClick={() => {
                    changeSecret(generateRegistrationSecret())
                    setSecretRevealed(true)
                  }}
                >
                  <Icon name="arrow-repeat" className="me-1" />
                  {t('settings.registration.regenerate')}
                </Button>
              </InputGroup>
              <Form.Text className="d-block">{t('settings.registration.secretHelp')}</Form.Text>
            </Form.Group>

            {registerUrl !== null && (
              <div className="mt-3">
                <div className="form-label mb-1">{t('settings.registration.pageLabel')}</div>
                <a href={registerUrl} className="text-break" data-testid="settings-register-url">
                  {registerUrl}
                </a>
                <Form.Text className="d-block">{t('settings.registration.pageHelp')}</Form.Text>
              </div>
            )}

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

        <Card className="mb-4">
          <Card.Body>
            <h2 className="kk-section-title mb-1">{t('settings.footer.title')}</h2>
            <p className="text-secondary">{t('settings.footer.help')}</p>
            <Form.Group controlId="settings-footer-text">
              <Form.Label>{t('settings.footer.label')}</Form.Label>
              <Form.Control
                type="text"
                value={draft.footerText}
                maxLength={MAX_FOOTER_LENGTH}
                placeholder={t('settings.footer.placeholder')}
                aria-describedby="settings-footer-counter"
                onChange={(event) => {
                  // A text input is one line already: the browser strips a
                  // pasted line break, which the backend would refuse.
                  const value = event.currentTarget.value
                  setDraft((prev) => ({ ...prev, footerText: value }))
                }}
              />
              <Form.Text id="settings-footer-counter" className="d-block text-end">
                {t('settings.footer.counter', {
                  length: draft.footerText.length,
                  max: MAX_FOOTER_LENGTH,
                })}
              </Form.Text>
            </Form.Group>
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

      <div className="mt-4">
        <UsageCard />
        {isMaintainer && <AnnouncementComposer />}
      </div>

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
    </NarrowPage>
  )
}

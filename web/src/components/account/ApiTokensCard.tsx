import { type SyntheticEvent, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Form from 'react-bootstrap/Form'
import InputGroup from 'react-bootstrap/InputGroup'
import ListGroup from 'react-bootstrap/ListGroup'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useReloadKey } from '../../hooks/useReloadKey'
import { formatDateTimeMinutes } from '../../lib/format'
import {
  ApiError,
  API_TOKEN_NAME_MAX_LENGTH,
  type ApiToken,
  createApiToken,
  type CreatedApiToken,
  fetchApiTokens,
  revokeApiToken,
} from '../../services/auth'
import { ConfirmModal } from '../ConfirmModal'
import { EmptyState } from '../EmptyState'
import { ErrorState } from '../ErrorState'
import { Icon } from '../Icon'

/**
 * The help chapter this section points at, both from its empty state and from
 * the panel that discloses a fresh secret. It is one place so the anchor cannot
 * drift from `HelpPage`'s section id.
 */
export const API_TOKENS_HELP_HREF = '/help#help-api-tokens'

/**
 * Fetch lifecycle of the token list. `forbidden` is not an error: it is the
 * answer "your role may not have tokens", which the card renders as a read-only
 * explanation rather than as a broken load.
 */
type ListState =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'forbidden' }
  | { status: 'ready'; tokens: ApiToken[] }

/** The i18n key of the message a failed creation shows. */
type CreateErrorKey =
  | 'account.apiTokens.createError.name'
  | 'account.apiTokens.createError.rateLimited'
  | 'account.apiTokens.createError.generic'

/** Maps a failed creation to the i18n key of the message to show. */
function createErrorKeyFor(error: unknown): CreateErrorKey {
  if (error instanceof ApiError) {
    if (error.status === 400) {
      return 'account.apiTokens.createError.name'
    }
    if (error.status === 429) {
      return 'account.apiTokens.createError.rateLimited'
    }
  }
  return 'account.apiTokens.createError.generic'
}

/**
 * Reports whether a token has been revoked. A revoked token still comes back
 * from the API (the backend keeps the full history), but it can never
 * authenticate anything again, so it is not shown here.
 */
function isRevoked(token: ApiToken): boolean {
  return token.revoked_at !== undefined
}

/** Reports whether a token's expiry has already passed as of `now`. */
function isExpired(token: ApiToken, now: number): boolean {
  return token.expires_at !== undefined && new Date(token.expires_at).getTime() <= now
}

/**
 * The recognisable head of a plaintext token: `kkt_<id>_…`. The id is public (it
 * is the lookup key embedded in the credential, and the API returns it), so this
 * lets somebody who found a token in a script's config match it to a row here
 * without pasting the secret anywhere.
 */
function tokenPrefix(token: ApiToken): string {
  return `kkt_${token.id}_…`
}

/**
 * One token in the list: its name, the identifying prefix, when it was made and
 * when it was last seen, plus the way to revoke it.
 */
function TokenRow({ token, onRevoke }: { token: ApiToken; onRevoke: (token: ApiToken) => void }) {
  const { t, i18n } = useTranslation()
  const expired = isExpired(token, Date.now())

  return (
    <ListGroup.Item className="d-flex align-items-start justify-content-between gap-2">
      <div className="kk-min-w-0">
        <div className="d-flex align-items-center gap-2 flex-wrap">
          <span className="fw-semibold text-break">{token.name}</span>
          {expired && <Badge bg="secondary">{t('account.apiTokens.expired')}</Badge>}
        </div>
        <div className="text-secondary small text-break">
          <code>{tokenPrefix(token)}</code>
        </div>
        <div className="text-secondary small">
          {t('account.apiTokens.createdAt', {
            date: formatDateTimeMinutes(token.created_at, i18n.language),
          })}
          {' · '}
          {token.last_used_at === undefined
            ? t('account.apiTokens.neverUsed')
            : t('account.apiTokens.lastUsed', {
                date: formatDateTimeMinutes(token.last_used_at, i18n.language),
              })}
          {token.expires_at !== undefined && (
            <>
              {' · '}
              {t('account.apiTokens.expiresAt', {
                date: formatDateTimeMinutes(token.expires_at, i18n.language),
              })}
            </>
          )}
        </div>
      </div>
      {/* The glyph keeps its word above `sm`; below it the row is a long token
          name plus one Czech-worded button, which does not fit a phone. */}
      <Button
        variant="outline-danger"
        size="sm"
        className="d-inline-flex align-items-center gap-2 flex-shrink-0 kukatko-tap-target-touch"
        aria-label={t('account.apiTokens.revokeNamed', { name: token.name })}
        title={t('account.apiTokens.revoke')}
        onClick={() => {
          onRevoke(token)
        }}
      >
        <Icon name="trash" />
        <span className="d-none d-sm-inline">{t('account.apiTokens.revoke')}</span>
      </Button>
    </ListGroup.Item>
  )
}

/**
 * The panel that discloses a freshly minted secret — the one and only time it is
 * ever shown. It is a warning, not a success: the server keeps nothing but a
 * hash, so a secret that is not copied now is lost and the token has to be made
 * again. The value sits in a read-only input (selectable, and copyable by the
 * button beside it) rather than in prose, so it can be dragged out even where
 * the clipboard API is denied.
 */
function CreatedSecret({
  created,
  onDismiss,
}: {
  created: CreatedApiToken
  onDismiss: () => void
}) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)

  async function copy() {
    try {
      await navigator.clipboard.writeText(created.secret)
      setCopied(true)
    } catch {
      // The clipboard can be denied (an insecure context, a withheld
      // permission). The secret is in the field either way, so there is nothing
      // to report and nothing to undo.
    }
  }

  return (
    <Alert variant="warning" className="mt-3" role="alert">
      <Alert.Heading as="h3" className="h6">
        {t('account.apiTokens.created.title', { name: created.token.name })}
      </Alert.Heading>
      <p className="mb-2">{t('account.apiTokens.created.warning')}</p>
      <InputGroup>
        <Form.Control
          readOnly
          value={created.secret}
          aria-label={t('account.apiTokens.created.secretLabel')}
          onFocus={(event) => {
            event.target.select()
          }}
        />
        <Button
          variant="outline-dark"
          className="d-inline-flex align-items-center gap-2"
          onClick={() => {
            void copy()
          }}
        >
          <Icon name={copied ? 'check-lg' : 'clipboard'} />
          {copied ? t('account.apiTokens.created.copied') : t('account.apiTokens.created.copy')}
        </Button>
      </InputGroup>
      <div className="d-flex justify-content-end mt-2">
        <Button variant="outline-dark" size="sm" onClick={onDismiss}>
          {t('account.apiTokens.created.dismiss')}
        </Button>
      </div>
    </Alert>
  )
}

/**
 * The "API tokens" section of the account page: the signed-in user's own
 * long-lived bearer credentials for non-interactive clients — the `kukatko ctl`
 * CLI, a backup script, an agent — listed, minted and revoked from the UI
 * instead of from `curl`.
 *
 * The section mirrors what the backend actually guarantees. A token inherits its
 * owner's role, so nothing here grants more than the reader already has; the
 * secret is disclosed exactly once, by {@link CreatedSecret}; and a revoked
 * token disappears from the list at once (the API keeps it, this card does not
 * show what can no longer authenticate). Expired tokens stay, badged, because
 * "it stopped working" is a question this list should answer.
 *
 * Every token endpoint is behind plain authentication today, so every role may
 * mint one. Should that ever change, a 403 on the listing or on the creation
 * switches the whole section to a read-only explanation rather than leaving a
 * form that answers every submission with an error.
 */
export function ApiTokensCard() {
  const { t } = useTranslation()
  const [state, setState] = useState<ListState>({ status: 'loading' })
  const [reloadKey, reload] = useReloadKey()
  const [name, setName] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<CreateErrorKey | null>(null)
  const [created, setCreated] = useState<CreatedApiToken | null>(null)
  const [pendingRevoke, setPendingRevoke] = useState<ApiToken | null>(null)
  const [revokeError, setRevokeError] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchApiTokens(controller.signal)
      .then((tokens) => {
        setState({ status: 'ready', tokens: tokens.filter((token) => !isRevoked(token)) })
      })
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === 'AbortError') {
          return
        }
        setState({
          status: error instanceof ApiError && error.status === 403 ? 'forbidden' : 'error',
        })
      })
    return () => {
      controller.abort()
    }
  }, [reloadKey])

  async function handleCreate(event: SyntheticEvent) {
    event.preventDefault()
    if (name.trim() === '') {
      setCreateError('account.apiTokens.createError.name')
      return
    }
    setCreating(true)
    setCreateError(null)
    try {
      const result = await createApiToken(name.trim())
      setCreated(result)
      setName('')
      // The secret panel lives outside the list, so refreshing is free: the new
      // token simply shows up among the others, as any other client's would.
      reload()
    } catch (error: unknown) {
      if (error instanceof ApiError && error.status === 403) {
        setState({ status: 'forbidden' })
      } else {
        setCreateError(createErrorKeyFor(error))
      }
    } finally {
      setCreating(false)
    }
  }

  async function revoke(token: ApiToken) {
    setRevokeError(false)
    // Optimistically drop the row, remembering the prior list to restore on error.
    let previous: ApiToken[] = []
    setState((prev) => {
      if (prev.status !== 'ready') {
        return prev
      }
      previous = prev.tokens
      return { status: 'ready', tokens: prev.tokens.filter((item) => item.id !== token.id) }
    })
    try {
      await revokeApiToken(token.id)
    } catch {
      setRevokeError(true)
      setState({ status: 'ready', tokens: previous })
    }
  }

  return (
    <Card text="light" className="mb-4">
      <Card.Body>
        <Card.Title as="h2" className="kk-section-title mb-3">
          {t('account.apiTokens.title')}
        </Card.Title>
        <p className="text-secondary">{t('account.apiTokens.intro')}</p>

        {revokeError && (
          <Alert variant="danger" role="alert">
            {t('account.apiTokens.revokeError')}
          </Alert>
        )}

        {state.status === 'loading' && (
          <div className="d-flex justify-content-center py-4">
            <Spinner animation="border" role="status">
              <span className="visually-hidden">{t('account.apiTokens.loading')}</span>
            </Spinner>
          </div>
        )}

        {state.status === 'error' && (
          <ErrorState title={t('account.apiTokens.loadError')} onRetry={reload} size="sm" />
        )}

        {/* The read-only answer to a backend that will not mint tokens for this
            role: an explanation of what is missing, never a form that cannot
            work. */}
        {state.status === 'forbidden' && (
          <Alert variant="secondary" role="alert" className="mb-0">
            <Icon name="lock-fill" className="me-2" />
            {t('account.apiTokens.forbidden')}
          </Alert>
        )}

        {state.status === 'ready' && state.tokens.length === 0 && (
          <EmptyState
            size="sm"
            icon={<Icon name="key" />}
            title={t('account.apiTokens.empty.title')}
            hint={t('account.apiTokens.empty.hint')}
            action={<Link to={API_TOKENS_HELP_HREF}>{t('account.apiTokens.empty.link')}</Link>}
          />
        )}

        {state.status === 'ready' && state.tokens.length > 0 && (
          <ListGroup>
            {state.tokens.map((token) => (
              <TokenRow key={token.id} token={token} onRevoke={setPendingRevoke} />
            ))}
          </ListGroup>
        )}

        {created !== null && (
          <CreatedSecret
            created={created}
            onDismiss={() => {
              setCreated(null)
            }}
          />
        )}

        {state.status !== 'forbidden' && (
          <Form
            className="mt-3"
            onSubmit={(event) => {
              void handleCreate(event)
            }}
          >
            {createError !== null && (
              <Alert variant="danger" role="alert">
                {t(createError)}
              </Alert>
            )}
            <Form.Group controlId="api-token-name">
              <Form.Label>{t('account.apiTokens.nameLabel')}</Form.Label>
              <InputGroup>
                <Form.Control
                  value={name}
                  maxLength={API_TOKEN_NAME_MAX_LENGTH}
                  placeholder={t('account.apiTokens.namePlaceholder')}
                  disabled={creating}
                  onChange={(event) => {
                    setName(event.target.value)
                  }}
                />
                <Button
                  type="submit"
                  variant="primary"
                  disabled={creating}
                  className="d-inline-flex align-items-center gap-2"
                >
                  {creating ? (
                    <Spinner animation="border" size="sm" role="status" aria-hidden="true" />
                  ) : (
                    <Icon name="plus-lg" />
                  )}
                  {t('account.apiTokens.create')}
                </Button>
              </InputGroup>
              <Form.Text className="text-secondary">{t('account.apiTokens.nameHint')}</Form.Text>
            </Form.Group>
          </Form>
        )}
      </Card.Body>

      <ConfirmModal
        show={pendingRevoke !== null}
        title={t('account.apiTokens.confirmTitle')}
        confirmLabel={t('account.apiTokens.confirmAction')}
        onCancel={() => {
          setPendingRevoke(null)
        }}
        onConfirm={() => {
          const token = pendingRevoke
          setPendingRevoke(null)
          if (token !== null) {
            void revoke(token)
          }
        }}
      >
        {pendingRevoke !== null && t('account.apiTokens.confirmBody', { name: pendingRevoke.name })}
      </ConfirmModal>
    </Card>
  )
}

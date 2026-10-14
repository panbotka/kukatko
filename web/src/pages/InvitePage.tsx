import { useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { Trans, useTranslation } from 'react-i18next'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { AuthFrame } from '../components/AuthFrame'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Icon } from '../components/Icon'
import { useToast } from '../components/toast/ToastContext'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import {
  acceptInvite,
  fetchPublicInvite,
  InviteApiError,
  type PublicInvite,
} from '../services/invites'

/** Fetch lifecycle of the invite the address names. */
type LoadState =
  | { status: 'loading' }
  | { status: 'notFound' }
  | { status: 'error' }
  | { status: 'ready'; invite: PublicInvite }

/** Whether `error` is the one 404 every unusable invite code gets. */
function isNotFound(error: unknown): boolean {
  return error instanceof InviteApiError && error.status === 404
}

/**
 * `/invite/:code` — the page behind an invitation link a project admin sent.
 * Public and instance-level (outside any project's layout), because whoever
 * follows the link may not have an account yet.
 *
 * It asks `GET /invite/<code>` what the invite grants and says it: „Pozvánka do
 * projektu **<name>** jako <role>". Signed in, „Přijmout pozvánku" accepts it,
 * re-reads the session (so the new membership is known) and lands in
 * `/p/<slug>/` with a toast. Signed out, it offers „Zaregistrovat se"
 * (`/register?invite=<code>`: no registration word, the account is approved at
 * once) and „Přihlásit se" (`/login?returnTo=/invite/<code>`, back here
 * afterwards).
 *
 * An unusable code — unknown, expired, revoked, used up — gets one and the same
 * page for every reason, exactly as the backend answers: "this invitation is no
 * longer valid, ask whoever sent it for a new one". It never says which reason
 * it was, so it is no oracle of codes; it only says what to do next.
 */
export function InvitePage() {
  const { t, i18n } = useTranslation()
  const { code = '' } = useParams()
  const { refresh } = useAuth()
  const toast = useToast()
  const navigate = useNavigate()
  const [load, setLoad] = useState<LoadState>({ status: 'loading' })
  const [accepting, setAccepting] = useState(false)
  useDocumentTitle(t('invitePage.title'))

  const reload = useCallback(
    async (signal?: AbortSignal) => {
      try {
        const invite = await fetchPublicInvite(code, signal)
        if (signal?.aborted !== true) {
          setLoad({ status: 'ready', invite })
        }
      } catch (error: unknown) {
        if (signal?.aborted === true) {
          return
        }
        setLoad({ status: isNotFound(error) ? 'notFound' : 'error' })
      }
    },
    [code],
  )

  useEffect(() => {
    const controller = new AbortController()
    void reload(controller.signal)
    return () => {
      controller.abort()
    }
  }, [reload])

  async function accept(invite: PublicInvite) {
    if (accepting) {
      return
    }
    setAccepting(true)
    try {
      const accepted = await acceptInvite(code)
      // The session lists the account's projects; until it is re-read the new
      // membership is unknown and the project's address would answer not-found.
      await refresh()
      toast.show({
        message: t(accepted.changed ? 'invitePage.accepted' : 'invitePage.alreadyMember', {
          project: invite.project_name,
        }),
        variant: 'success',
      })
      void navigate(`/p/${encodeURIComponent(accepted.slug)}/`)
    } catch (error: unknown) {
      setAccepting(false)
      if (isNotFound(error)) {
        setLoad({ status: 'notFound' })
      } else if (error instanceof InviteApiError && error.status === 401) {
        // The session ended meanwhile: the page offers signing in instead.
        void reload()
      } else {
        toast.show({ message: t('invitePage.acceptFailed'), variant: 'danger' })
      }
    }
  }

  if (load.status === 'notFound') {
    // One page for every reason, exactly as the backend answers: it never says
    // which one, only what to do about it.
    return (
      <AuthFrame title={t('invitePage.title')}>
        <div data-testid="invite-dead">
          <EmptyState
            icon={<Icon name="envelope-x" />}
            title={t('invitePage.deadTitle')}
            hint={t('invitePage.deadHint')}
          />
        </div>
      </AuthFrame>
    )
  }

  const encoded = encodeURIComponent(code)

  return (
    <AuthFrame title={t('invitePage.title')}>
      {load.status === 'loading' && (
        <div className="text-center py-3">
          <Spinner animation="border" role="status">
            <span className="visually-hidden">{t('invitePage.loading')}</span>
          </Spinner>
        </div>
      )}
      {load.status === 'error' && (
        <ErrorState
          title={t('invitePage.loadFailed')}
          onRetry={() => {
            setLoad({ status: 'loading' })
            void reload()
          }}
        />
      )}
      {load.status === 'ready' && (
        <div data-testid="invite-page">
          <p className="fs-5 text-center" data-testid="invite-lead">
            <Trans
              i18nKey="invitePage.lead"
              values={{
                project: load.invite.project_name,
                role: t(`roles.${load.invite.role}`).toLocaleLowerCase(i18n.language),
              }}
              components={{ strong: <strong /> }}
            />
          </p>
          {load.invite.signed_in ? (
            <div className="d-grid mt-4">
              <Button
                variant="primary"
                disabled={accepting}
                onClick={() => {
                  void accept(load.invite)
                }}
              >
                {accepting ? (
                  <Spinner animation="border" size="sm" className="me-2" aria-hidden="true" />
                ) : (
                  <Icon name="person-check" className="me-1" />
                )}
                {t('invitePage.accept')}
              </Button>
            </div>
          ) : (
            <>
              <p className="text-secondary">{t('invitePage.signedOutHint')}</p>
              {load.invite.registration_role !== load.invite.role && (
                <Alert variant="info" className="small">
                  {t('invitePage.registrationRole', {
                    role: t(`roles.${load.invite.registration_role}`).toLocaleLowerCase(
                      i18n.language,
                    ),
                  })}
                </Alert>
              )}
              <div className="d-grid gap-2 mt-4">
                <Link to={`/register?invite=${encoded}`} className="btn btn-primary">
                  <Icon name="person-plus" className="me-1" />
                  {t('invitePage.register')}
                </Link>
                <Link
                  to={`/login?returnTo=${encodeURIComponent(`/invite/${code}`)}`}
                  className="btn btn-outline-secondary"
                >
                  <Icon name="key" className="me-1" />
                  {t('invitePage.login')}
                </Link>
              </div>
            </>
          )}
        </div>
      )}
    </AuthFrame>
  )
}

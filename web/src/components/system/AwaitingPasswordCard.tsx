import { useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { formatDateTimeMinutes } from '../../lib/format'
import {
  type AwaitingMember,
  fetchAwaitingPassword,
  type FirstAdmin,
  resendPasswordLink,
} from '../../services/projects'
import { Icon } from '../Icon'

import { FirstAdminResult } from './FirstAdminResult'

/** Props of {@link AwaitingPasswordCard}. */
export interface AwaitingPasswordCardProps {
  projectUid: string
}

/**
 * The project page's „Čeká na nastavení hesla": the members whose accounts have
 * no password yet — a project set up for somebody who has not followed their
 * link — each with the link's expiry and „Poslat odkaz znovu", which issues a
 * fresh link (the previous one stops working) and mails it. What became of the
 * re-send is shown under the member ({@link FirstAdminResult}): „E-mail
 * odeslán", or the copyable link when no mail went out.
 *
 * The card renders nothing while there is nobody to list — the usual case — and
 * nothing when the list cannot be read: it is a convenience on the page, not
 * its subject.
 */
export function AwaitingPasswordCard({ projectUid }: AwaitingPasswordCardProps) {
  const { t, i18n } = useTranslation()
  const [members, setMembers] = useState<AwaitingMember[]>([])
  const [busy, setBusy] = useState<string | null>(null)
  const [failed, setFailed] = useState<string | null>(null)
  const [resent, setResent] = useState<FirstAdmin | null>(null)

  const load = useCallback(
    async (signal?: AbortSignal) => {
      try {
        const list = await fetchAwaitingPassword(projectUid, signal)
        if (signal?.aborted !== true) {
          setMembers(list)
        }
      } catch {
        // Nothing to show: see the doc comment.
      }
    },
    [projectUid],
  )

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => {
      controller.abort()
    }
  }, [load])

  async function resend(member: AwaitingMember) {
    setBusy(member.user_uid)
    setFailed(null)
    setResent(null)
    try {
      setResent(await resendPasswordLink(projectUid, member.user_uid))
      await load()
    } catch {
      setFailed(member.user_uid)
    } finally {
      setBusy(null)
    }
  }

  if (members.length === 0) {
    return null
  }

  return (
    <Card className="mb-4" data-testid="awaiting-password">
      <Card.Body>
        <h2 className="kk-section-title mb-2">{t('systemProject.awaiting.title')}</h2>
        <p className="text-secondary">{t('systemProject.awaiting.intro')}</p>
        <ul className="list-unstyled mb-0">
          {members.map((member) => (
            <li key={member.user_uid} className="mb-3">
              <div className="d-flex flex-wrap align-items-center justify-content-between gap-2">
                <div>
                  <span className="font-monospace">{member.username}</span>{' '}
                  <span className="text-secondary">{member.email}</span>
                  <div className="small text-secondary">
                    {member.link_expires_at === null
                      ? t('systemProject.awaiting.linkExpired')
                      : t('systemProject.awaiting.linkExpires', {
                          date: formatDateTimeMinutes(member.link_expires_at, i18n.language),
                        })}
                  </div>
                </div>
                <Button
                  variant="outline-primary"
                  size="sm"
                  disabled={busy !== null}
                  title={t('systemProject.awaiting.resendHelp')}
                  onClick={() => {
                    void resend(member)
                  }}
                >
                  {busy === member.user_uid ? (
                    <Spinner as="span" animation="border" size="sm" className="me-1" />
                  ) : (
                    <Icon name="send" className="me-1" />
                  )}
                  {t('systemProject.awaiting.resend')}
                </Button>
              </div>
              {failed === member.user_uid && (
                <Alert variant="danger" className="mt-2 mb-0" role="alert">
                  {t('systemProject.awaiting.resendFailed')}
                </Alert>
              )}
              {resent !== null && resent.user_uid === member.user_uid && (
                <div
                  className="mt-2"
                  role="group"
                  aria-label={t('systemProject.awaiting.resentTitle', {
                    username: member.username,
                  })}
                >
                  <FirstAdminResult admin={resent} idPrefix={`resent-${member.user_uid}`} />
                </div>
              )}
            </li>
          ))}
        </ul>
      </Card.Body>
    </Card>
  )
}

import { useState } from 'react'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import InputGroup from 'react-bootstrap/InputGroup'
import { useTranslation } from 'react-i18next'

import { formatDateTimeMinutes } from '../../lib/format'
import type { FirstAdmin } from '../../services/projects'
import { Icon } from '../Icon'

/** Props of {@link FirstAdminResult}. */
export interface FirstAdminResultProps {
  admin: FirstAdmin
  /** The id prefix of the link field, unique on the page. */
  idPrefix: string
}

/**
 * What became of a project's first admin, as the system administrator needs it
 * next: which account it is (new or existing), whether the e-mail went out and,
 * when it did not, the one-time set-password link in a read-only field,
 * copyable in one press — the administrator's copy is then the only way in, as
 * with a reset link ({@link ResetLinkModal}). The link's expiry is shown
 * whenever there is a link.
 *
 * Shown by the create dialog's result screen and by the project page's
 * "send the link again". The link lives in the caller's state only.
 */
export function FirstAdminResult({ admin, idPrefix }: FirstAdminResultProps) {
  const { t, i18n } = useTranslation()
  const [copied, setCopied] = useState(false)
  const link = admin.set_password_url ?? ''
  const existingWithPassword = link === ''

  async function copy() {
    try {
      await navigator.clipboard.writeText(link)
      setCopied(true)
    } catch {
      // A denied clipboard changes nothing: the link is in the field.
    }
  }

  let delivery: string
  if (admin.mail_sent) {
    delivery = existingWithPassword
      ? t('systemProjects.firstAdmin.mailSentExisting', { email: admin.email })
      : t('systemProjects.firstAdmin.mailSent', { email: admin.email })
  } else {
    delivery = existingWithPassword
      ? t('systemProjects.firstAdmin.mailNotSentExisting')
      : t('systemProjects.firstAdmin.mailNotSent')
  }

  return (
    <div data-testid="first-admin-result">
      <dl className="row mb-2">
        <dt className="col-4 fw-normal text-secondary">
          {t('systemProjects.firstAdmin.adminLabel')}
        </dt>
        <dd className="col-8">
          <span className="font-monospace">{admin.username}</span>{' '}
          <span className="text-secondary">
            (
            {admin.account === 'created'
              ? t('systemProjects.firstAdmin.accountCreated')
              : t('systemProjects.firstAdmin.accountExisting')}
            )
          </span>
        </dd>
      </dl>
      <p className={admin.mail_sent ? 'mb-2' : 'mb-2 fw-semibold'} role="status">
        {admin.mail_sent && <Icon name="envelope-check" className="me-2" />}
        {delivery}
      </p>
      {!admin.mail_sent && !existingWithPassword && (
        <InputGroup className="mb-2">
          <Form.Control
            id={`${idPrefix}-link`}
            readOnly
            value={link}
            aria-label={t('systemProjects.firstAdmin.linkLabel')}
            onFocus={(event) => {
              event.target.select()
            }}
          />
          <Button
            variant="outline-secondary"
            className="d-inline-flex align-items-center gap-2"
            onClick={() => {
              void copy()
            }}
          >
            <Icon name={copied ? 'check-lg' : 'clipboard'} />
            {copied ? t('systemProjects.firstAdmin.copied') : t('systemProjects.firstAdmin.copy')}
          </Button>
        </InputGroup>
      )}
      {admin.expires_at !== undefined && (
        <p className="text-secondary small mb-0">
          {t('systemProjects.firstAdmin.expires', {
            date: formatDateTimeMinutes(admin.expires_at, i18n.language),
          })}
        </p>
      )}
    </div>
  )
}

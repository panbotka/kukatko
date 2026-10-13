import { useState } from 'react'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import InputGroup from 'react-bootstrap/InputGroup'
import { useTranslation } from 'react-i18next'

import { Icon } from './Icon'

/** Props of {@link CopyLinkButton}. */
interface CopyLinkButtonProps {
  /** The absolute address to copy. */
  url: string
  size?: 'sm'
}

/**
 * A button that copies a link's address and confirms with "Copied" — the
 * compact form of {@link LinkAddress} for a list row, where a whole read-only
 * field per row would crowd the table.
 */
export function CopyLinkButton({ url, size }: CopyLinkButtonProps) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)

  async function copy() {
    try {
      await navigator.clipboard.writeText(url)
      setCopied(true)
    } catch {
      // Clipboard refused: the address is still on screen, ready to select by hand.
    }
  }

  return (
    <Button
      variant="outline-secondary"
      size={size}
      onClick={() => {
        void copy()
      }}
    >
      <Icon name={copied ? 'check-lg' : 'clipboard'} className="me-1" />
      {copied ? t('linkAddress.copied') : t('linkAddress.copy')}
    </Button>
  )
}

/** Props of {@link LinkAddress}. */
interface LinkAddressProps {
  /** The absolute address of the public page. */
  url: string
  /** The read-only field's accessible name ("Upload link", "Invitation link"). */
  label: string
  /** The link's title, offered to the share sheet; empty for none. */
  shareTitle?: string
  /** `sm` for compact cards. */
  size?: 'sm'
}

/**
 * A link's address in a read-only field, a copy button that confirms with
 * "Copied" and — where the device offers it — the system share sheet. Every
 * link Kukátko hands out for sending on (upload links, invitations) is shown
 * through this one control.
 */
export function LinkAddress({ url, label, shareTitle = '', size }: LinkAddressProps) {
  const { t } = useTranslation()
  const canShare = typeof navigator.share === 'function'

  async function share() {
    try {
      await navigator.share({ title: shareTitle, url })
    } catch {
      // Dismissed, or the share sheet failed: the field and copy button remain.
    }
  }

  return (
    <InputGroup className="mb-2" size={size}>
      <Form.Control
        readOnly
        value={url}
        aria-label={label}
        onFocus={(e) => {
          e.target.select()
        }}
      />
      <CopyLinkButton url={url} />
      {canShare && (
        <Button
          variant="outline-secondary"
          onClick={() => {
            void share()
          }}
        >
          <Icon name="share" className="me-1" />
          {t('linkAddress.share')}
        </Button>
      )}
    </InputGroup>
  )
}

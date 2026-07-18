import { useState } from 'react'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { formatBytes } from '../../lib/format'
import { type StackMember } from '../../services/photos'
import { FadeInImage } from '../FadeInImage'
import { Icon } from '../Icon'

/** Props for {@link StackStrip}. */
export interface StackStripProps {
  /** Every file of the stack, the primary first (as the detail response lists them). */
  members: StackMember[]
  /** The uid of the photo currently being viewed, highlighted in the strip. */
  currentUid: string
  /** Whether the acting user may set a primary or unstack (editor/admin). */
  canWrite: boolean
  /** Makes the given member the stack's primary; resolves once the page reloaded. */
  onSetPrimary: (uid: string) => Promise<void>
  /** Removes the given member from the stack (it becomes standalone). */
  onUnstackMember: (uid: string) => Promise<void>
  /** Dissolves the whole stack. */
  onUnstackAll: () => Promise<void>
  /** Query string appended to a variant's detail link to preserve list context. */
  detailQuery?: string
}

/**
 * The variants strip on the photo detail page: it lists the several files of one
 * shot that were grouped into a stack (a RAW next to its JPEG, an exported edit),
 * each with its thumbnail, format, dimensions and size, and links to view any of
 * them. For an editor it also offers the two per-member actions — set as primary
 * and unstack (remove that member) — plus dissolving the whole stack. It renders
 * nothing for an unstacked photo (fewer than two members).
 */
export function StackStrip({
  members,
  currentUid,
  canWrite,
  onSetPrimary,
  onUnstackMember,
  onUnstackAll,
  detailQuery,
}: StackStripProps) {
  const { t, i18n } = useTranslation()
  // A single in-flight guard for the whole strip: while one action runs, every
  // button is disabled so a second click cannot race the reload.
  const [busy, setBusy] = useState(false)

  if (members.length < 2) {
    return null
  }

  const run = (action: () => Promise<void>) => {
    return () => {
      setBusy(true)
      void action().finally(() => {
        setBusy(false)
      })
    }
  }

  const variantLink = (uid: string) =>
    detailQuery !== undefined && detailQuery !== ''
      ? `/photos/${uid}?${detailQuery}`
      : `/photos/${uid}`

  return (
    <Card className="mb-3">
      <Card.Header className="d-flex align-items-center justify-content-between">
        <span className="d-inline-flex align-items-center gap-2">
          <Icon name="images" />
          {t('stack.title', { count: members.length })}
        </span>
        {canWrite && (
          <Button variant="outline-danger" size="sm" disabled={busy} onClick={run(onUnstackAll)}>
            {t('stack.unstackAll')}
          </Button>
        )}
      </Card.Header>
      <Card.Body className="d-flex flex-wrap gap-2">
        {members.map((member) => (
          <div
            key={member.uid}
            className={`kk-stack-variant border rounded p-2 d-flex gap-2${
              member.uid === currentUid ? ' border-primary' : ''
            }`}
            style={{ maxWidth: '18rem' }}
          >
            <Link
              to={variantLink(member.uid)}
              className="flex-shrink-0"
              aria-label={t('stack.viewVariant', { name: member.file_name })}
            >
              {member.thumb_url !== undefined && member.thumb_url !== '' ? (
                <FadeInImage
                  src={member.thumb_url}
                  alt={member.file_name}
                  width={64}
                  height={64}
                  className="rounded"
                  style={{ objectFit: 'cover', width: 64, height: 64 }}
                />
              ) : (
                <span
                  className="d-inline-flex align-items-center justify-content-center bg-body-secondary rounded"
                  style={{ width: 64, height: 64 }}
                  aria-hidden="true"
                >
                  <Icon name="images" />
                </span>
              )}
            </Link>
            <div className="d-flex flex-column small">
              <span className="text-truncate fw-semibold" title={member.file_name}>
                {member.file_name}
                {member.is_primary && (
                  <Badge bg="primary" className="ms-1">
                    {t('stack.primary')}
                  </Badge>
                )}
              </span>
              <span className="text-secondary">
                {member.file_width > 0 && member.file_height > 0
                  ? `${String(member.file_width)}×${String(member.file_height)} · `
                  : ''}
                {formatBytes(member.file_size, i18n.language)}
              </span>
              {canWrite && (
                <div className="d-flex gap-1 mt-1 flex-wrap">
                  {!member.is_primary && (
                    <Button
                      variant="outline-secondary"
                      size="sm"
                      disabled={busy}
                      onClick={run(() => onSetPrimary(member.uid))}
                    >
                      {t('stack.setPrimary')}
                    </Button>
                  )}
                  <Button
                    variant="outline-secondary"
                    size="sm"
                    disabled={busy}
                    onClick={run(() => onUnstackMember(member.uid))}
                  >
                    {t('stack.unstack')}
                  </Button>
                </div>
              )}
            </div>
          </div>
        ))}
      </Card.Body>
    </Card>
  )
}

import { useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Form from 'react-bootstrap/Form'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import {
  clearAnnouncement,
  fetchAnnouncement,
  setAnnouncement,
  type AnnouncementLevel,
} from '../services/announcement'

/** Transient outcome of a publish or clear, shown as a dismissible alert. */
type ActionNotice = { kind: 'success'; message: string } | { kind: 'error'; message: string }

/**
 * The compose control for the current project's announcement banner
 * (`/api/v1/p/<slug>/announcement`). It loads the project's current message on
 * mount (so an existing announcement can be edited in place), and lets the
 * system administrator publish a new/updated message at an info or warning
 * level, or clear it for every member of the project. The banner is per
 * project, which is why this lives on the project's settings page rather than
 * on the instance-level system page; publishing stays a maintainer's call
 * server-side, so the page renders it only for a maintainer. It is
 * self-contained — it owns its own form and notice state.
 */
export function AnnouncementComposer() {
  const { t } = useTranslation()
  const [message, setMessage] = useState('')
  const [level, setLevel] = useState<AnnouncementLevel>('info')
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState<ActionNotice | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    // Prefill from whatever is currently published; a failure just leaves the
    // form blank (the maintainer can still publish a fresh message).
    fetchAnnouncement(controller.signal)
      .then((current) => {
        setMessage(current.message)
        if (current.level !== undefined) {
          setLevel(current.level)
        }
      })
      .catch(() => undefined)
    return () => {
      controller.abort()
    }
  }, [])

  const handlePublish = useCallback(async () => {
    setBusy(true)
    setNotice(null)
    try {
      const saved = await setAnnouncement(message.trim(), level)
      setMessage(saved.message)
      if (saved.level !== undefined) {
        setLevel(saved.level)
      }
      setNotice({ kind: 'success', message: t('announcement.compose.published') })
    } catch {
      setNotice({ kind: 'error', message: t('announcement.compose.publishError') })
    } finally {
      setBusy(false)
    }
  }, [level, message, t])

  const handleClear = useCallback(async () => {
    setBusy(true)
    setNotice(null)
    try {
      await clearAnnouncement()
      setMessage('')
      setLevel('info')
      setNotice({ kind: 'success', message: t('announcement.compose.cleared') })
    } catch {
      setNotice({ kind: 'error', message: t('announcement.compose.clearError') })
    } finally {
      setBusy(false)
    }
  }, [t])

  return (
    <Card className="mb-4">
      <Card.Body>
        <h2 className="kk-section-title mb-1">{t('announcement.compose.title')}</h2>
        <p className="text-secondary small">{t('announcement.compose.intro')}</p>
        {notice && (
          <Alert
            variant={notice.kind === 'success' ? 'success' : 'danger'}
            dismissible
            onClose={() => {
              setNotice(null)
            }}
          >
            {notice.message}
          </Alert>
        )}
        <Form.Group className="mb-3" controlId="announcement-message">
          <Form.Label>{t('announcement.compose.messageLabel')}</Form.Label>
          <Form.Control
            as="textarea"
            rows={2}
            value={message}
            placeholder={t('announcement.compose.messagePlaceholder')}
            disabled={busy}
            onChange={(event) => {
              setMessage(event.target.value)
            }}
          />
        </Form.Group>
        <Form.Group className="mb-3" controlId="announcement-level">
          <Form.Label>{t('announcement.compose.levelLabel')}</Form.Label>
          <Form.Select
            value={level}
            disabled={busy}
            onChange={(event) => {
              setLevel(event.target.value as AnnouncementLevel)
            }}
          >
            <option value="info">{t('announcement.compose.level.info')}</option>
            <option value="warning">{t('announcement.compose.level.warning')}</option>
          </Form.Select>
        </Form.Group>
        <div className="d-flex gap-2 flex-wrap">
          <Button
            variant="primary"
            size="sm"
            disabled={busy || message.trim() === ''}
            onClick={() => {
              void handlePublish()
            }}
          >
            {busy && <Spinner animation="border" size="sm" role="status" className="me-2" />}
            {t('announcement.compose.publish')}
          </Button>
          <Button
            variant="outline-secondary"
            size="sm"
            disabled={busy}
            onClick={() => {
              void handleClear()
            }}
          >
            {t('announcement.compose.clear')}
          </Button>
        </div>
      </Card.Body>
    </Card>
  )
}

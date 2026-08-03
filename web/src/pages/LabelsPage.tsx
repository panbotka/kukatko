import { useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import ListGroup from 'react-bootstrap/ListGroup'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { ConfirmModal } from '../components/ConfirmModal'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Icon } from '../components/Icon'
import { LabelEditModal } from '../components/organize/LabelEditModal'
import { ListSkeleton } from '../components/Skeleton'
import { useReloadKey } from '../hooks/useReloadKey'
import {
  deleteLabel,
  fetchLabels,
  type Label,
  type LabelCount,
  updateLabel,
} from '../services/organize'

/** Fetch lifecycle of the labels list. */
type State = { status: 'loading' } | { status: 'error' } | { status: 'ready'; labels: LabelCount[] }

/**
 * The labels index: a list of labels with photo counts, each linking to its
 * scoped photo grid. Editors and admins can create, rename and delete labels,
 * and switch a label in or out of the review game; mutation controls are hidden
 * from viewers.
 *
 * The review switch lives here and deliberately nowhere else. Inside the game it
 * would be an answer to the wrong question — "not this photo" is already a
 * per-photo rejection — and a decision about a whole label belongs where the
 * labels are managed, taken calmly rather than mid-round.
 */
export function LabelsPage() {
  const { t } = useTranslation()
  const { canWrite } = useAuth()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [editing, setEditing] = useState<Label | null>(null)
  const [creating, setCreating] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<Label | null>(null)
  const [actionError, setActionError] = useState(false)
  const [savingUID, setSavingUID] = useState<string | null>(null)
  const [reloadKey, reload] = useReloadKey()

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchLabels(controller.signal)
      .then((labels) => {
        setState({ status: 'ready', labels })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setState({ status: 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [reloadKey])

  async function remove(label: Label) {
    setActionError(false)
    try {
      await deleteLabel(label.uid)
      setState((prev) =>
        prev.status === 'ready'
          ? { status: 'ready', labels: prev.labels.filter((l) => l.uid !== label.uid) }
          : prev,
      )
    } catch {
      setActionError(true)
    }
  }

  /**
   * Switches a label in or out of the review game. The row flips immediately and
   * is rolled back if the save fails, so the switch never shows a state the
   * server does not hold. Name and priority are sent back unchanged because the
   * endpoint takes the whole editable record.
   */
  async function setReviewEnabled(label: LabelCount, enabled: boolean) {
    setActionError(false)
    setSavingUID(label.uid)
    applyReviewEnabled(label.uid, enabled)
    try {
      await updateLabel(label.uid, {
        name: label.name,
        priority: label.priority,
        review_enabled: enabled,
      })
    } catch {
      applyReviewEnabled(label.uid, !enabled)
      setActionError(true)
    } finally {
      setSavingUID(null)
    }
  }

  function applyReviewEnabled(uid: string, enabled: boolean) {
    setState((prev) =>
      prev.status === 'ready'
        ? {
            status: 'ready',
            labels: prev.labels.map((l) => (l.uid === uid ? { ...l, review_enabled: enabled } : l)),
          }
        : prev,
    )
  }

  function upsert(saved: Label) {
    setState((prev) => {
      if (prev.status !== 'ready') {
        return prev
      }
      const existing = prev.labels.find((l) => l.uid === saved.uid)
      const labels = existing
        ? prev.labels.map((l) => (l.uid === saved.uid ? { ...l, ...saved } : l))
        : [...prev.labels, { ...saved, photo_count: 0 }]
      labels.sort((a, b) => b.priority - a.priority)
      return { status: 'ready', labels }
    })
  }

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-3 flex-wrap gap-2">
        <h1 className="kk-page-title mb-0">{t('labels.title')}</h1>
        {canWrite && (
          <Button
            variant="primary"
            onClick={() => {
              setCreating(true)
            }}
          >
            {t('labels.create')}
          </Button>
        )}
      </div>

      {actionError && <Alert variant="danger">{t('labels.actionError')}</Alert>}

      {state.status === 'loading' && <ListSkeleton label={t('labels.loading')} />}

      {state.status === 'error' && <ErrorState title={t('labels.error')} onRetry={reload} />}

      {state.status === 'ready' && state.labels.length === 0 && (
        <EmptyState title={t('labels.empty.title')} hint={t('labels.empty.hint')} />
      )}

      {state.status === 'ready' && state.labels.length > 0 && (
        <ListGroup className="gap-2">
          {state.labels.map((label) => (
            <ListGroup.Item
              key={label.uid}
              className="kk-tile-row d-flex align-items-center justify-content-between gap-2"
            >
              {/* The name truncates rather than pushing the row past the
                  viewport (a label name is user data and can be long or
                  unbroken); the count keeps its width so it never truncates
                  away. */}
              <Link
                to={`/labels/${label.uid}`}
                className="text-decoration-none d-flex align-items-center gap-2 flex-grow-1 kk-min-w-0"
              >
                <span className="text-truncate">{label.name}</span>
                <Badge bg="secondary" pill className="flex-shrink-0">
                  {label.photo_count}
                </Badge>
              </Link>
              {canWrite && (
                /* Both actions keep a glyph and drop their word below `sm`, so a
                   phone row never has to fit a name plus two Czech-worded
                   buttons across ~336px. The `aria-label` carries the same word
                   the button shows, so the accessible name is identical at every
                   width. */
                <div className="d-flex align-items-center gap-1 flex-shrink-0">
                  {/* The review switch is wordless at every width — a per-row
                      label would repeat the same sentence down the whole page —
                      so the game's own icon carries the meaning and the
                      accessible name names both the label and what the switch
                      does. The hover text sits on the wrapper because Form.Check
                      does not forward `title` to either the input or its label. */}
                  <span
                    title={label.review_enabled ? t('labels.review.on') : t('labels.review.off')}
                  >
                    <Form.Check
                      type="switch"
                      id={`label-review-${label.uid}`}
                      className="mb-0 me-1 d-inline-flex align-items-center gap-1"
                      checked={label.review_enabled}
                      disabled={savingUID === label.uid}
                      aria-label={t('labels.review.toggle', { name: label.name })}
                      label={<Icon name="ui-checks" />}
                      onChange={(event) => {
                        void setReviewEnabled(label, event.target.checked)
                      }}
                    />
                  </span>
                  <Button
                    variant="outline-secondary"
                    size="sm"
                    className="d-inline-flex align-items-center gap-2 kukatko-tap-target-touch"
                    aria-label={t('labels.rename')}
                    title={t('labels.rename')}
                    onClick={() => {
                      setEditing(label)
                    }}
                  >
                    <Icon name="pencil" />
                    <span className="d-none d-sm-inline">{t('labels.rename')}</span>
                  </Button>
                  <Button
                    variant="outline-danger"
                    size="sm"
                    className="d-inline-flex align-items-center gap-2 kukatko-tap-target-touch"
                    aria-label={t('labels.delete')}
                    title={t('labels.delete')}
                    onClick={() => {
                      setPendingDelete(label)
                    }}
                  >
                    <Icon name="trash" />
                    <span className="d-none d-sm-inline">{t('labels.delete')}</span>
                  </Button>
                </div>
              )}
            </ListGroup.Item>
          ))}
        </ListGroup>
      )}

      {canWrite && (
        <LabelEditModal
          show={creating}
          onHide={() => {
            setCreating(false)
          }}
          onSaved={(label) => {
            upsert(label)
            setCreating(false)
          }}
        />
      )}
      {canWrite && (
        <LabelEditModal
          label={editing}
          show={editing !== null}
          onHide={() => {
            setEditing(null)
          }}
          onSaved={(label) => {
            upsert(label)
            setEditing(null)
          }}
        />
      )}

      <ConfirmModal
        show={pendingDelete !== null}
        title={t('labels.confirmTitle')}
        confirmLabel={t('labels.deleteConfirm')}
        onCancel={() => {
          setPendingDelete(null)
        }}
        onConfirm={() => {
          const label = pendingDelete
          setPendingDelete(null)
          if (label) {
            void remove(label)
          }
        }}
      >
        {pendingDelete && t('labels.confirmDelete', { name: pendingDelete.name })}
      </ConfirmModal>
    </>
  )
}

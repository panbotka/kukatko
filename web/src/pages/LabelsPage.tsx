import { useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import ListGroup from 'react-bootstrap/ListGroup'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { LabelEditModal } from '../components/organize/LabelEditModal'
import { deleteLabel, fetchLabels, type Label, type LabelCount } from '../services/organize'

/** Fetch lifecycle of the labels list. */
type State = { status: 'loading' } | { status: 'error' } | { status: 'ready'; labels: LabelCount[] }

/**
 * The labels index: a list of labels with photo counts, each linking to its
 * scoped photo grid. Editors and admins can create, rename and delete labels;
 * mutation controls are hidden from viewers.
 */
export function LabelsPage() {
  const { t } = useTranslation()
  const { canWrite } = useAuth()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [editing, setEditing] = useState<Label | null>(null)
  const [creating, setCreating] = useState(false)
  const [actionError, setActionError] = useState(false)

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
  }, [])

  async function remove(label: Label) {
    if (!window.confirm(t('labels.confirmDelete', { name: label.name }))) {
      return
    }
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
        <h1 className="h3 mb-0">{t('labels.title')}</h1>
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

      {state.status === 'loading' && (
        <div className="d-flex justify-content-center py-5">
          <Spinner animation="border" role="status">
            <span className="visually-hidden">{t('labels.loading')}</span>
          </Spinner>
        </div>
      )}

      {state.status === 'error' && <Alert variant="danger">{t('labels.error')}</Alert>}

      {state.status === 'ready' && state.labels.length === 0 && (
        <div className="text-center text-secondary py-5">
          <p className="mb-1 fs-5">{t('labels.empty.title')}</p>
          <p className="mb-0 small">{t('labels.empty.hint')}</p>
        </div>
      )}

      {state.status === 'ready' && state.labels.length > 0 && (
        <ListGroup>
          {state.labels.map((label) => (
            <ListGroup.Item
              key={label.uid}
              className="d-flex align-items-center justify-content-between gap-2"
            >
              <Link to={`/labels/${label.uid}`} className="text-decoration-none flex-grow-1">
                {label.name}{' '}
                <Badge bg="secondary" pill>
                  {label.photo_count}
                </Badge>
              </Link>
              {canWrite && (
                <div className="d-flex gap-1">
                  <Button
                    variant="outline-secondary"
                    size="sm"
                    onClick={() => {
                      setEditing(label)
                    }}
                  >
                    {t('labels.rename')}
                  </Button>
                  <Button
                    variant="outline-danger"
                    size="sm"
                    onClick={() => {
                      void remove(label)
                    }}
                  >
                    {t('labels.delete')}
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
    </>
  )
}

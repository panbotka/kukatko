import { useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../auth/AuthContext'
import { ConfirmModal } from '../components/ConfirmModal'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { type LabelChipActions } from '../components/organize/LabelChip'
import { LabelCloud } from '../components/organize/LabelCloud'
import { LabelEditModal } from '../components/organize/LabelEditModal'
import { LabelFilterBar } from '../components/organize/LabelFilterBar'
import { ChipCloudSkeleton } from '../components/Skeleton'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useReloadKey } from '../hooks/useReloadKey'
import {
  type LabelsView,
  browseLabels,
  familyKey,
  labelBrowseOptions,
  LABELS_DEFAULTS,
  toggleFamilyOpen,
  withFamilyOpen,
} from '../lib/labelBrowse'
import { useUrlState } from '../lib/urlState'
import {
  deleteLabel,
  fetchLabels,
  type Label,
  type LabelCount,
  updateLabel,
} from '../services/organize'

/** Fetch lifecycle of the labels list. */
type State = { status: 'loading' } | { status: 'error' } | { status: 'ready'; labels: LabelCount[] }

/** No labels at all, so there is nothing for the cloud to fold or order. */
const NO_LABELS: LabelCount[] = []

/**
 * The labels index: a wrapping cloud of label chips with photo counts, each
 * linking to its scoped photo grid. Editors and admins can create, rename and
 * delete labels, and switch a label in or out of the review game; mutation
 * controls are hidden from viewers.
 *
 * The API returns every label in one alphabetical list, and drawn as full-width
 * rows a real library's hundred-plus of them ran five screens deep — with whole
 * families of numbered labels (`Dum11`, `Dum12`, …) occupying the entire start of
 * the alphabet and pushing the meaningful ones off the first screens. So the
 * labels are chips (see `LabelCloud`), the numbered families fold into one
 * expandable chip each, and the page carries a name search and a choice between
 * most-used-first and alphabetical (see `LabelFilterBar` over the pure
 * `lib/labelBrowse`). All of it lives in the URL, so Back steps through the views
 * and a link carries the exact one, and a search that matched nothing carries one
 * button back out of it instead of leaving the reader to guess what to delete.
 *
 * The review switch lives here and deliberately nowhere else. Inside the game it
 * would be an answer to the wrong question — "not this photo" is already a
 * per-photo rejection — and a decision about a whole label belongs where the
 * labels are managed, taken calmly rather than mid-round.
 */
export function LabelsPage() {
  const { t, i18n } = useTranslation()
  useDocumentTitle(t('labels.title'))
  const { canWrite } = useAuth()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [editing, setEditing] = useState<Label | null>(null)
  const [creating, setCreating] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<Label | null>(null)
  const [actionError, setActionError] = useState(false)
  const [savingUID, setSavingUID] = useState<string | null>(null)
  const [reloadKey, reload] = useReloadKey()
  const [view, setView] = useUrlState<LabelsView>(LABELS_DEFAULTS)

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
   * Switches a label in or out of the review game. The chip flips immediately and
   * is rolled back if the save fails, so it never shows a state the server does
   * not hold. Name and priority are sent back unchanged because the endpoint
   * takes the whole editable record.
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
      // A new label is simply appended: the cloud's own search, folding and
      // ordering decide where it lands, so nothing is sorted here.
      return {
        status: 'ready',
        labels: existing
          ? prev.labels.map((l) => (l.uid === saved.uid ? { ...l, ...saved } : l))
          : [...prev.labels, { ...saved, photo_count: 0 }],
      }
    })
    reveal(saved)
  }

  /**
   * Makes a just-saved label visible. A numbered name (`Dum99`) joins a family
   * that may be folded shut, and a label that vanishes the moment it is saved
   * reads as a failed save; expanding its family costs one URL key and keeps the
   * chip on screen.
   */
  function reveal(saved: Label) {
    const key = familyKey(saved.name)
    if (key !== null) {
      setView({ open: withFamilyOpen(view.open, key) })
    }
  }

  const labels = state.status === 'ready' ? state.labels : NO_LABELS
  const language = i18n.language
  const { entries } = useMemo(
    () => browseLabels(labels, labelBrowseOptions(view, language)),
    [labels, view, language],
  )

  const actions: LabelChipActions | undefined = canWrite
    ? {
        onRename: (label) => {
          setEditing(label)
        },
        onDelete: (label) => {
          setPendingDelete(label)
        },
        onToggleReview: (label, enabled) => {
          void setReviewEnabled(label, enabled)
        },
        savingUID,
      }
    : undefined

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

      {state.status === 'loading' && <ChipCloudSkeleton label={t('labels.loading')} />}

      {state.status === 'error' && <ErrorState title={t('labels.error')} onRetry={reload} />}

      {state.status === 'ready' && labels.length === 0 && (
        <EmptyState title={t('labels.empty.title')} hint={t('labels.empty.hint')} />
      )}

      {state.status === 'ready' && labels.length > 0 && (
        <>
          <LabelFilterBar view={view} onChange={setView} />

          {entries.length === 0 && (
            <EmptyState
              title={t('labels.noMatches.title')}
              hint={t('labels.noMatches.hint')}
              // The way back from a search that matched nothing is one button.
              // It clears the search and nothing else: the search is the only
              // control that can empty the cloud, so resetting the ordering too
              // would silently undo a choice that is not the reason the reader
              // is looking at this screen.
              action={
                view.q === '' ? undefined : (
                  <Button
                    variant="outline-secondary"
                    onClick={() => {
                      setView({ q: '' })
                    }}
                  >
                    {t('labels.noMatches.reset')}
                  </Button>
                )
              }
            />
          )}

          {entries.length > 0 && (
            <LabelCloud
              entries={entries}
              actions={actions}
              onToggleFamily={(key) => {
                setView({ open: toggleFamilyOpen(view.open, key) })
              }}
            />
          )}
        </>
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

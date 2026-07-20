import { useCallback, useEffect, useMemo, useState } from 'react'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'
import { useParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { BackLink } from '../components/BackLink'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { GridDensityControl } from '../components/library/GridDensityControl'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { type BatchExtraAction, BatchActionBar } from '../components/organize/BatchActionBar'
import { Candidates } from '../components/people/Candidates'
import { Outliers } from '../components/people/Outliers'
import { SubjectEditModal } from '../components/people/SubjectEditModal'
import { SubjectPhotoTile } from '../components/people/SubjectPhotoTile'
import { Skeleton } from '../components/Skeleton'
import { useBulkEdit } from '../hooks/useBulkEdit'
import { useGridDensity } from '../hooks/useGridDensity'
import { useReloadKey } from '../hooks/useReloadKey'
import { useSubjectPhotos } from '../hooks/useSubjectPhotos'
import { DETAIL_DEFAULTS, detailQueryString } from '../lib/detailView'
import { GRID_GAP_PX, gridTemplateColumns } from '../lib/gridDensity'
import { fetchSubject, type Subject, updateSubject } from '../services/people'

/**
 * Where the back link leads. The people index keeps no view state of its own in
 * the URL, so the bare route restores it exactly; should it ever grow filters,
 * this is the one place that has to carry them.
 */
const PEOPLE_PATH = '/people'

/** Fetch lifecycle of the subject record. */
type State = { status: 'loading' } | { status: 'error' } | { status: 'ready'; subject: Subject }

/**
 * A subject's page: header (name, type, edit, and the shared images-per-row
 * density control — a view preference open to everyone who can see the page),
 * the photo gallery (with a set-cover action for editors), and — for editors —
 * two review sections: the candidate search (untagged photos where this person
 * likely appears, to confirm/reject) and the outlier review (spot and detach
 * mis-assigned faces). The gallery pages through `GET /subjects/{uid}/photos`
 * with a load-more control.
 *
 * Editors can also select photos in the gallery; picking one raises the
 * library's own floating batch bar, so the full set of batch actions (add to
 * album, add/remove labels, favorite, archive, download, stack, the full editor)
 * is available here too, and the gallery refetches afterwards, since the edit
 * may have taken photos out of it. Every tile carries the library's corner
 * selection checkmark from the outset (no "enter selection mode" step).
 */
export function SubjectPage() {
  const { t } = useTranslation()
  const { canWrite } = useAuth()
  const { density } = useGridDensity()
  const { uid = '' } = useParams<{ uid: string }>()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [editing, setEditing] = useState(false)
  const [coverBusy, setCoverBusy] = useState(false)

  const [reloadKey, reload] = useReloadKey()
  const { photos, status, hasMore, loadingMore, loadMore, retry } = useSubjectPhotos(uid, {
    reloadKey,
  })

  // Each tile carries this subject's scope in the detail link (`person=<uid>`), so
  // opening a photo pages prev/next through `GET /photos?person=<uid>` — this
  // person's photos in the same order the gallery shows — instead of the whole
  // library. The gallery has no filters/sort of its own (the subject-photos
  // endpoint is always newest-first), so the scope is the sole non-default facet.
  const detailQuery = useMemo(() => detailQueryString({ ...DETAIL_DEFAULTS, person: uid }), [uid])

  // Hover-select: a writer's tiles carry the corner checkmark from the outset,
  // so the toolbar below keys off what is picked rather than an explicit mode.
  // `gridSelection` is the role gate — it is undefined for a viewer, who never
  // selects, exactly as the shared photo grids read it.
  const bulk = useBulkEdit({ onEdited: reload, hoverSelect: true })
  const selection = bulk.selection
  const selectable = bulk.gridSelection !== undefined
  const selecting = selection.count > 0

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchSubject(uid, controller.signal)
      .then((subject) => {
        setState({ status: 'ready', subject })
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
  }, [uid])

  // Walking from one person to the next reuses this page, so a selection made in
  // the previous gallery would survive into a gallery that never showed those
  // photos. Each subject is its own list: leave selection mode with the subject.
  const leaveSelection = selection.disable
  useEffect(() => {
    leaveSelection()
  }, [uid, leaveSelection])

  const setCover = useCallback(
    async (photoUid: string) => {
      if (state.status !== 'ready') {
        return
      }
      const subject = state.subject
      setCoverBusy(true)
      try {
        const updated = await updateSubject(subject.uid, {
          name: subject.name,
          type: subject.type,
          favorite: subject.favorite,
          private: subject.private,
          notes: subject.notes,
          cover_photo_uid: photoUid,
        })
        setState({ status: 'ready', subject: updated })
      } catch {
        // A failed cover change leaves the previous cover untouched; the tile
        // re-enables so the user can retry.
      } finally {
        setCoverBusy(false)
      }
    },
    [state],
  )

  // Select every tile the gallery has loaded so far — the load-more pages that
  // have actually arrived, never the whole person.
  const selectAllInView = useCallback(() => {
    selection.selectMany(photos.map((p) => p.uid))
  }, [photos, selection])

  // A tile hides its own set-cover button once the gallery turns into selection
  // targets, so the action moves onto the batch bar for the duration — it stays
  // reachable, and like the album's it waits for a selection of exactly one.
  const extraActions = useMemo<BatchExtraAction[]>(
    () => [
      {
        id: 'set-cover',
        icon: 'image',
        label: t('subject.cover.set'),
        disabled: coverBusy || selection.count !== 1,
        onClick: () => {
          // Guarded by `disabled` above: exactly one photo is picked here.
          const [photoUid] = [...selection.selected]
          void setCover(photoUid)
        },
      },
    ],
    [t, coverBusy, selection.count, selection.selected, setCover],
  )

  if (state.status === 'loading') {
    // Hold the page's shape while the record loads: a header placeholder over the
    // gallery skeleton, so the layout does not jump when the subject arrives.
    return (
      <>
        <div className="d-flex align-items-center gap-2 mb-3" aria-hidden="true">
          <Skeleton width="10rem" height="1.75rem" radius="var(--kk-radius-sm)" />
          <Skeleton width="4rem" height="1.375rem" radius="var(--kk-radius-pill)" />
        </div>
        <h2 className="kk-section-title">{t('subject.photos')}</h2>
        <GridSkeleton label={t('subject.loadingPhotos')} />
      </>
    )
  }

  if (state.status === 'error') {
    return (
      <ErrorState
        title={t('subject.error')}
        action={<BackLink to={PEOPLE_PATH} label={t('subject.back')} />}
      />
    )
  }

  const { subject } = state

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-3 flex-wrap gap-2">
        <div className="d-flex align-items-center gap-2">
          <BackLink to={PEOPLE_PATH} label={t('subject.back')} />
          <h1 className="kk-page-title mb-0">{subject.name}</h1>
          <Badge bg="secondary">{t(`subject.type.${subject.type}`)}</Badge>
        </div>
        {/* The subject's own actions stay put during a selection: the batch bar
            floats over the bottom edge and never contends with the header. */}
        <div className="d-flex align-items-center gap-2 flex-wrap">
          {/* A view preference, not a write action: shown to every viewer so
              anyone can re-column the gallery, exactly as the other galleries
              expose it through the FilterBar. */}
          <GridDensityControl />
          {canWrite && (
            <Button
              variant="outline-secondary"
              size="sm"
              onClick={() => {
                setEditing(true)
              }}
            >
              {t('subject.editButton')}
            </Button>
          )}
        </div>
      </div>

      <h2 className="kk-section-title">{t('subject.photos')}</h2>
      {status === 'loading' && <GridSkeleton label={t('subject.loadingPhotos')} />}
      {status === 'error' && <ErrorState title={t('library.error.load')} onRetry={retry} />}
      {status === 'ready' && photos.length === 0 && <EmptyState title={t('subject.noPhotos')} />}
      {photos.length > 0 && (
        <>
          <div
            data-density={String(density)}
            style={{
              display: 'grid',
              gridTemplateColumns: gridTemplateColumns(density),
              gap: `${GRID_GAP_PX}px`,
            }}
          >
            {photos.map((photo) => (
              <SubjectPhotoTile
                key={photo.uid}
                photo={photo}
                isCover={subject.cover_photo_uid === photo.uid}
                canSetCover={canWrite}
                busy={coverBusy}
                onSetCover={(photoUid) => {
                  void setCover(photoUid)
                }}
                selectable={selectable}
                selectFirst={selectable && selecting}
                selected={selection.selected.has(photo.uid)}
                anySelected={selecting}
                onToggleSelect={selection.toggle}
                detailQuery={detailQuery}
              />
            ))}
          </div>
          {hasMore && (
            <div className="text-center mt-3">
              <Button
                variant="outline-secondary"
                size="sm"
                disabled={loadingMore}
                onClick={loadMore}
              >
                {loadingMore ? t('subject.loadingMore') : t('subject.loadMore')}
              </Button>
            </div>
          )}
        </>
      )}

      {canWrite && (
        <section className="mt-4">
          <h2 className="kk-section-title">{t('candidates.title')}</h2>
          <p className="text-secondary small">{t('candidates.subtitle')}</p>
          {/* Confirming a candidate assigns a face to this subject, which can bring a
              new photo into the gallery above — reload it so counters/tiles catch up. */}
          <Candidates subjectUid={subject.uid} onAssigned={reload} />
        </section>
      )}

      {canWrite && (
        <section className="mt-4">
          <h2 className="kk-section-title">{t('outliers.title')}</h2>
          <p className="text-secondary small">{t('outliers.subtitle')}</p>
          <Outliers subjectUid={subject.uid} />
        </section>
      )}

      {bulk.canBulkEdit && selecting && (
        <>
          {/* Keeps the page's last section scrollable clear of the floating
              bar, so nothing hides behind it — the clearance tracks the bar's
              measured height (`--kk-batch-clearance`), so it holds even when the
              bar collapses on a phone. */}
          <div style={{ paddingBottom: 'var(--kk-batch-clearance)' }} aria-hidden="true" />
          <BatchActionBar bulk={bulk} onSelectAll={selectAllInView} extraActions={extraActions} />
        </>
      )}

      {canWrite && (
        <SubjectEditModal
          subject={subject}
          show={editing}
          onHide={() => {
            setEditing(false)
          }}
          onSaved={(updated) => {
            setState({ status: 'ready', subject: updated })
            setEditing(false)
          }}
        />
      )}
    </>
  )
}

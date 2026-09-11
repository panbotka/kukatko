import { useCallback, useMemo, useRef, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { useNavigate, useParams } from 'react-router-dom'

import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Icon } from '../components/Icon'
import { FaceCrop } from '../components/people/FaceCrop'
import { FaceOverlay } from '../components/people/FaceOverlay'
import { useAlbumFaces } from '../hooks/useAlbumFaces'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useFrozenHeight } from '../hooks/useFrozenHeight'
import { useImageFrame } from '../hooks/useImageFrame'
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts'
import { type FaceView } from '../services/people'
import { type Photo, thumbUrl } from '../services/photos'

import '../components/people/albumFaces.css'

/**
 * The preview size the stage requests. A `fit_*` (whole frame), never a square
 * `tile_*`: the face boxes are normalised against the full photo, so a
 * centre-cropped tile would put every rectangle somewhere else.
 */
const PREVIEW_SIZE = 'fit_1280'

/** 3:2 while nothing better is known, so the stage never collapses to no height. */
const FALLBACK_RATIO = 1.5

/** The edge of a row's face crop, in CSS pixels — the same size the faces panel uses. */
const ROW_FACE_SIZE = 44

/** Formats a 0..1 confidence as a whole-percent string, as the faces panel does. */
function confidencePct(confidence: number): string {
  return `${String(Math.round(confidence * 100))}%`
}

/** Props for {@link AlbumFacesStage}. */
interface AlbumFacesStageProps {
  photo: Photo
  faces: FaceView[]
  hovered: number | null
  onHover: (faceIndex: number | null) => void
}

/**
 * The photo under question with its numbered face boxes. Split out so the frame
 * measurement ({@link useImageFrame}) is keyed on the photo and starts over on
 * every advance — a box drawn against the previous photo's frame lands on the
 * wrong part of this one.
 */
function AlbumFacesStage({ photo, faces, hovered, onHover }: AlbumFacesStageProps) {
  const { t } = useTranslation()
  const stage = useImageFrame({
    source: photo.uid,
    width: photo.file_width,
    height: photo.file_height,
    orientation: photo.file_orientation ?? 0,
  })
  const ratio = stage.ratio ?? FALLBACK_RATIO

  return (
    <div className="kk-album-faces__stage">
      <div
        className="kk-album-faces__photo"
        style={{
          width: '100%',
          aspectRatio: String(ratio),
          maxWidth: `min(100%, calc(100cqh * ${String(ratio)}))`,
        }}
      >
        <img
          {...stage.imgProps}
          src={thumbUrl(photo.uid, PREVIEW_SIZE)}
          alt={t('albumFaces.photoAlt')}
          decoding="async"
          className="kk-album-faces__img"
        />
        <FaceOverlay
          faces={faces}
          selected={null}
          hovered={hovered}
          measured={stage.measured}
          onSelect={onHover}
          onHover={onHover}
        />
      </div>
    </div>
  )
}

/**
 * Working through one album's photos, naming the faces on them: the photos of
 * the album that still carry somebody unnamed, one at a time, each face offered
 * with the person the recogniser thinks it is, so the normal rhythm is yes, yes,
 * yes.
 *
 * It is what an album uploaded after an event needs and what the photo detail
 * cannot be: the detail page is where *one* photo is worked on in depth, and
 * reaching it for eighty photos in a row costs eighty round trips through a grid.
 * Here the queue, the suggestions and the next photo's faces are all in flight
 * ahead of the reader ({@link useAlbumFaces}).
 *
 * **Confirm and dismiss are not opposites.** Confirming writes the name through
 * the same `POST /photos/{uid}/faces/assign` every other naming surface uses.
 * Dismissing writes nothing at all — it drops the row for this run, and the face
 * is offered again next time. That is deliberately unlike the review game, where
 * "no" is a stored rejection: there the reader is answering a question about a
 * face, here they are moving past one.
 *
 * **The page owns the whole viewport**, outside the layout shell like the review
 * game, with an explicit close back to the album. Nothing scrolls but the row
 * list: the confirm controls have to be on screen at all times, because the whole
 * point is the rhythm.
 *
 * The keyboard does the lot: digits confirm the row carrying that number, `a`
 * confirms every offer on the photo, → or Space moves on, ← goes back, Esc
 * closes.
 */
export function AlbumFacesPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { uid = '' } = useParams<{ uid: string }>()
  const run = useAlbumFaces(uid)
  const [hovered, setHovered] = useState<number | null>(null)
  // The list of questions holds the height it had when this photo opened.
  // Confirming a face takes its row out, and a block that shrank with it would
  // pull the photograph above it — and every box drawn on it — out from under the
  // reader mid-rhythm, which is the one thing a yes-yes-yes surface must not do.
  // Keyed on the photo, so the only thing that resizes it is moving to another
  // one; the inner list is what gets measured, because the block itself is the
  // thing being sized and would only ever report back its own frozen number.
  const listRef = useRef<HTMLDivElement>(null)
  const rowsHeight = useFrozenHeight(listRef, run.photo?.uid ?? null)

  useDocumentTitle(t('documentTitle.albumFaces'))

  const albumPath = `/albums/${encodeURIComponent(uid)}`
  const close = useCallback(() => {
    void navigate(albumPath)
  }, [albumPath, navigate])

  const { rows, confirm, confirmAll, dismiss, next, back, busy, status } = run
  // Which row a digit names. Keyed by the number drawn on the box, so pressing 3
  // confirms the face labelled 3 — and a row past 9 simply has no key, which is
  // honest: it still has its button.
  const byNumber = useMemo(() => {
    const map = new Map<string, (typeof rows)[number]>()
    for (const row of rows) {
      if (row.number <= 9 && row.suggestion !== null) {
        map.set(String(row.number), row)
      }
    }
    return map
  }, [rows])

  const confirmNumber = useCallback(
    (key: string) => {
      const row = byNumber.get(key)
      if (row?.suggestion == null || busy) {
        return
      }
      confirm(row.face, row.suggestion)
    },
    [busy, byNumber, confirm],
  )

  useKeyboardShortcuts(
    {
      // One entry per digit rather than a wildcard: the shared dispatcher is a
      // token→handler map, and spelling them out keeps it that way.
      ...Object.fromEntries(
        ['1', '2', '3', '4', '5', '6', '7', '8', '9'].map((digit) => [
          digit,
          () => {
            confirmNumber(digit)
          },
        ]),
      ),
      a: () => {
        if (!busy) {
          confirmAll()
        }
      },
      ArrowRight: next,
      ' ': next,
      ArrowLeft: back,
      Escape: close,
    },
    { enabled: status === 'ready' || status === 'done' },
  )

  return (
    <div className="kk-album-faces">
      <div className="kk-album-faces__bar">
        <strong className="me-auto">
          {run.total > 0 && run.position > 0
            ? t('albumFaces.progress', { current: run.position, total: run.total })
            : t('albumFaces.title')}
        </strong>
        {run.busy && (
          <Spinner animation="border" size="sm" role="status">
            <span className="visually-hidden">{t('albumFaces.saving')}</span>
          </Spinner>
        )}
        <Button variant="outline-secondary" size="sm" onClick={close}>
          <Icon name="x-lg" className="me-1" />
          {t('albumFaces.close')}
          <kbd className="kk-album-faces__kbd">Esc</kbd>
        </Button>
      </div>

      {status === 'loading' && (
        <div className="kk-album-faces__stage">
          <Spinner animation="border" role="status">
            <span className="visually-hidden">{t('albumFaces.loading')}</span>
          </Spinner>
        </div>
      )}

      {status === 'error' && (
        <div className="kk-album-faces__stage">
          <ErrorState title={t('albumFaces.error')} />
        </div>
      )}

      {status === 'done' && (
        <div className="kk-album-faces__stage">
          <EmptyState
            icon={<Icon name="person-check" />}
            title={t('albumFaces.done.title')}
            hint={t('albumFaces.done.hint')}
            action={
              <Button variant="primary" onClick={close}>
                {t('albumFaces.done.back')}
              </Button>
            }
          />
        </div>
      )}

      {run.photo !== null && (
        <AlbumFacesStage
          photo={run.photo}
          faces={run.faces}
          hovered={hovered}
          onHover={setHovered}
        />
      )}

      {status === 'ready' && (
        <>
          {run.failed > 0 && (
            <Alert variant="warning" className="py-2 small mx-3 mb-2">
              {t('albumFaces.failed', { count: run.failed })}
            </Alert>
          )}

          <div
            className="kk-album-faces__rows"
            style={rowsHeight === null ? undefined : { height: `${String(rowsHeight)}px` }}
          >
            <div ref={listRef} className="list-group list-group-flush kk-album-faces__list">
              {rows.map((row) => (
                <div
                  key={row.face.face_index}
                  className="list-group-item kk-album-faces__row"
                  onMouseEnter={() => {
                    setHovered(row.face.face_index)
                  }}
                  onMouseLeave={() => {
                    setHovered(null)
                  }}
                  // A finger never hovers and neither does the keyboard, so the
                  // pairing is reported from focus as well — tabbing onto a row's
                  // confirm button lights the box it belongs to.
                  onFocus={() => {
                    setHovered(row.face.face_index)
                  }}
                  onBlur={() => {
                    setHovered(null)
                  }}
                >
                  <span className="badge text-bg-dark flex-shrink-0">{row.number}</span>
                  {run.photo !== null && (
                    <FaceCrop
                      photoUid={run.photo.uid}
                      bbox={row.face.bbox}
                      label=""
                      size={ROW_FACE_SIZE}
                      className="rounded-circle flex-shrink-0"
                    />
                  )}
                  {row.suggestion === null ? (
                    /* Nothing to offer, so nothing to press: the row says so and
                       stays, because a face missing from the list would read as a
                       photo that is finished when it is not. */
                    <span className="text-secondary small me-auto">
                      {t('albumFaces.noSuggestion')}
                    </span>
                  ) : (
                    <>
                      <span className="me-auto text-truncate">
                        {row.suggestion.subject_name}{' '}
                        <span className="opacity-75 small">
                          · {confidencePct(row.suggestion.confidence)}
                        </span>
                      </span>
                      {/* The label rides in `aria-label` rather than in hidden
                          text: the button also carries its digit keycap, and a
                          key read out after the person's name is noise. */}
                      <Button
                        variant="success"
                        size="sm"
                        disabled={busy}
                        aria-label={t('albumFaces.confirmRow', {
                          name: row.suggestion.subject_name,
                        })}
                        title={t('albumFaces.confirmRow', { name: row.suggestion.subject_name })}
                        onClick={() => {
                          if (row.suggestion !== null) {
                            confirm(row.face, row.suggestion)
                          }
                        }}
                      >
                        <Icon name="check-lg" />
                        {row.number <= 9 && <kbd className="kk-album-faces__kbd">{row.number}</kbd>}
                      </Button>
                      <Button
                        variant="outline-secondary"
                        size="sm"
                        disabled={busy}
                        aria-label={t('albumFaces.dismissRow', { number: row.number })}
                        title={t('albumFaces.dismissRow', { number: row.number })}
                        onClick={() => {
                          dismiss(row.face.face_index)
                        }}
                      >
                        <Icon name="x-lg" />
                      </Button>
                    </>
                  )}
                </div>
              ))}
            </div>
          </div>

          <div className="kk-album-faces__foot">
            <Button variant="outline-secondary" size="sm" disabled={!run.canGoBack} onClick={back}>
              <Icon name="chevron-left" className="me-1" />
              {t('albumFaces.previous')}
              <kbd className="kk-album-faces__kbd">←</kbd>
            </Button>
            {run.batch.length > 0 && (
              <Button variant="success" disabled={busy} onClick={confirmAll}>
                <Icon name="check-lg" className="me-1" />
                {t('albumFaces.confirmAll', { n: run.batch.length })}
                <kbd className="kk-album-faces__kbd">a</kbd>
              </Button>
            )}
            <Button variant="outline-secondary" size="sm" onClick={next}>
              {t('albumFaces.skip')}
              <Icon name="chevron-right" className="ms-1" />
              <kbd className="kk-album-faces__kbd">→</kbd>
            </Button>
          </div>
        </>
      )}
    </div>
  )
}
